package task

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/ffmpeg"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/smb"
	"Fnos.VC_Service/pkg/winapi"

	_ "github.com/mattn/go-sqlite3"
)

type TaskStatus string

const (
	StatusCreated          TaskStatus = "Created"
	StatusUploading        TaskStatus = "Uploading"
	StatusWaiting          TaskStatus = "Waiting"
	StatusPaused           TaskStatus = "Paused"
	StatusTranscoding      TaskStatus = "Transcoding"
	StatusSuccess          TaskStatus = "Success"
	StatusFailed           TaskStatus = "Failed"
	StatusCancelled        TaskStatus = "Cancelled"
	StatusClientDisconnect TaskStatus = "ClientDisconnect"
)

func (s TaskStatus) IsTerminal() bool {
	return s == StatusSuccess || s == StatusFailed || s == StatusCancelled
}

type TaskPriority int

const (
	PriorityNormal TaskPriority = 0
	PriorityUrgent TaskPriority = 1
)

type Task struct {
	TaskID         string
	Status         TaskStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SourceFileName string
	OutputName     string
	OutputFilePath string
	Progress       float64
	ClientConnID   string
	Priority       TaskPriority
	FFmpegArgs     string
	Resolution     string
	Bitrate        string
	FileCleaned    bool
	TotalChunks    int
	ReceivedChunks map[int]struct{}
	UploadComplete bool
	IsSMBMode      bool
	SMBPath        string
	SMBUser        string
	SMBPassword    string
	uploadFile     *os.File
	fileMu         sync.Mutex // 保护 uploadFile 的 Seek+Write 不被并发分片打断
}

type TaskManager struct {
	tasks          map[string]*Task
	runningCount   int
	waitingQueue   []*Task
	mutex          sync.RWMutex
	db             *sql.DB
	httpPort       int
	stopChan       chan struct{}
	lastNotifyTime map[string]time.Time
	lastNotifyProg map[string]float64
}

var (
	manager      *TaskManager
	onTaskUpdate func(taskID string)
)

func SetTaskUpdateCallback(fn func(taskID string)) {
	onTaskUpdate = fn
}

func Init() error {
	dbPath := filepath.Join(getAppDir(), "task.db")
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)

	if err := createTables(db); err != nil {
		return err
	}

	manager = &TaskManager{
		tasks:          make(map[string]*Task),
		waitingQueue:   []*Task{},
		db:             db,
		stopChan:       make(chan struct{}),
		lastNotifyTime: make(map[string]time.Time),
		lastNotifyProg: make(map[string]float64),
	}

	if err := loadTasksFromDB(); err != nil {
		logger.Warn("task", "Failed to load tasks from DB: ", err)
	}

	ffmpeg.SetCallbacks(MarkSuccess, MarkFailed)

	go startGCTimer()
	go startScheduler()

	return nil
}

func getAppDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func createTables(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS task_list (
		task_id TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		source_file_name TEXT,
		output_file_path TEXT,
		progress REAL DEFAULT 0,
		client_conn_id TEXT,
		priority INTEGER DEFAULT 0,
		ffmpeg_args TEXT,
		resolution TEXT,
		bitrate TEXT,
		file_cleaned INTEGER DEFAULT 0,
		total_chunks INTEGER DEFAULT 0,
		is_smb_mode INTEGER DEFAULT 0,
		smb_path TEXT,
		smb_user TEXT,
		smb_password TEXT
	);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	if err := addColumnIfNotExists(db, "task_list", "is_smb_mode", "INTEGER DEFAULT 0"); err != nil {
		logger.Warn("task", "Failed to add is_smb_mode column: ", err)
	}
	if err := addColumnIfNotExists(db, "task_list", "smb_path", "TEXT"); err != nil {
		logger.Warn("task", "Failed to add smb_path column: ", err)
	}
	if err := addColumnIfNotExists(db, "task_list", "smb_user", "TEXT"); err != nil {
		logger.Warn("task", "Failed to add smb_user column: ", err)
	}
	if err := addColumnIfNotExists(db, "task_list", "smb_password", "TEXT"); err != nil {
		logger.Warn("task", "Failed to add smb_password column: ", err)
	}

	return nil
}

func addColumnIfNotExists(db *sql.DB, tableName, columnName, columnDef string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return err
	}
	defer rows.Close()

	exists := false
	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == columnName {
			exists = true
			break
		}
	}
	if exists {
		return nil
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, columnDef))
	return err
}

func loadTasksFromDB() error {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	rows, err := manager.db.Query("SELECT * FROM task_list")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var task Task
		var createdAtStr, updatedAtStr string
		var fileCleaned int
		var isSMBMode int

		err := rows.Scan(
			&task.TaskID,
			&task.Status,
			&createdAtStr,
			&updatedAtStr,
			&task.SourceFileName,
			&task.OutputFilePath,
			&task.Progress,
			&task.ClientConnID,
			&task.Priority,
			&task.FFmpegArgs,
			&task.Resolution,
			&task.Bitrate,
			&fileCleaned,
			&task.TotalChunks,
			&isSMBMode,
			&task.SMBPath,
			&task.SMBUser,
			&task.SMBPassword,
		)
		if err != nil {
			continue
		}

		task.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		task.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
		task.FileCleaned = fileCleaned == 1
		task.IsSMBMode = isSMBMode == 1
		task.ReceivedChunks = make(map[int]struct{})

		if task.IsSMBMode && (task.Status == StatusCreated || task.Status == StatusUploading) {
			task.Status = StatusWaiting
			task.UploadComplete = true
			saveTaskToDB(&task)
			logger.Info("task", "loadTasksFromDB: recovered SMB task from stale status, taskID=%s", task.TaskID)
		}

		if task.Status == StatusWaiting || task.Status == StatusTranscoding {
			manager.tasks[task.TaskID] = &task
			if task.Status == StatusWaiting {
				manager.waitingQueue = append(manager.waitingQueue, &task)
			} else if task.Status == StatusTranscoding {
				// FVCS重启后，转码进程已不存在，将任务重置为等待状态重新排队
				task.Status = StatusWaiting
				task.UpdatedAt = time.Now()
				saveTaskToDB(&task)
				manager.waitingQueue = append(manager.waitingQueue, &task)
				logger.Info("task", "loadTasksFromDB: reset transcoding task to waiting, taskID=%s", task.TaskID)
			}
		}
	}

	// 启动时恢复任务后，检查是否有运行中的任务，如有则阻止系统休眠
	if manager.runningCount > 0 {
		winapi.PreventSleep()
		logger.Info("task", "Prevent system sleep: recovered %d running tasks on startup", manager.runningCount)
	}

	return nil
}

func startGCTimer() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cleanupTempFiles()
			cleanupFinishedTasks()
		case <-manager.stopChan:
			return
		}
	}
}

func cleanupFinishedTasks() {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	toDelete := make([]string, 0)
	for taskID, task := range manager.tasks {
		if task.Status.IsTerminal() && task.UpdatedAt.Before(cutoff) {
			toDelete = append(toDelete, taskID)
		}
	}

	for _, taskID := range toDelete {
		delete(manager.tasks, taskID)
		delete(manager.lastNotifyTime, taskID)
		delete(manager.lastNotifyProg, taskID)
	}

	if len(toDelete) > 0 {
		logger.Info("task", "Cleaned up %d finished tasks", len(toDelete))
	}
}

func cleanupTempFiles() {
	cfg := config.Get()
	ttl := time.Duration(cfg.TempFileTTLHour) * time.Hour
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	var tempDir string
	if filepath.IsAbs(cfg.TempDir) {
		tempDir = cfg.TempDir
	} else {
		tempDir = filepath.Join(getAppDir(), cfg.TempDir)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-ttl)
	for _, entry := range entries {
		if entry.IsDir() {
			fullPath := filepath.Join(tempDir, entry.Name())
			info, err := os.Stat(fullPath)
			if err == nil && info.ModTime().Before(cutoff) {
				os.RemoveAll(fullPath)
				logger.Info("task", "Cleaned expired temp dir: ", entry.Name())
			}
		}
	}
}

func startScheduler() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tryStartNext()
		case <-manager.stopChan:
			return
		}
	}
}

func tryStartNext() {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	cfg := config.Get()
	if manager.runningCount >= cfg.MaxConcurrentTasks {
		return
	}

	if len(manager.waitingQueue) == 0 {
		return
	}

	sort.Slice(manager.waitingQueue, func(i, j int) bool {
		return manager.waitingQueue[i].Priority > manager.waitingQueue[j].Priority
	})

	task := manager.waitingQueue[0]
	manager.waitingQueue = manager.waitingQueue[1:]

	task.Status = StatusTranscoding
	task.Progress = 0
	task.UpdatedAt = time.Now()
	manager.runningCount++

	// 有任务开始运行时，阻止系统休眠
	if manager.runningCount == 1 {
		winapi.PreventSleep()
		logger.Info("task", "Prevent system sleep: running task started")
	}

	logger.Info("task", "tryStartNext: taskID=%s, sourceFile=%s, waitingQueue size=%d, runningCount=%d",
		task.TaskID, task.SourceFileName, len(manager.waitingQueue), manager.runningCount)

	saveTaskToDB(task)
	fireTaskUpdateLocked(task.TaskID, true)
	go executeTranscode(task)
}

func executeTranscode(task *Task) {
	cfg := config.Get()
	var taskDir string
	if filepath.IsAbs(cfg.TempDir) {
		taskDir = filepath.Join(cfg.TempDir, task.TaskID)
	} else {
		taskDir = filepath.Join(getAppDir(), cfg.TempDir, task.TaskID)
	}

	var inputPath string
	var outputPath string
	var smbMountPath string
	var err error

	if task.IsSMBMode {
		logger.Info("task", "executeTranscode: SMB mode enabled, smbPath=%s", task.SMBPath)

		smbMountPath, err = smb.MountSMBShare(task.SMBPath, task.SMBUser, task.SMBPassword)
		if err != nil {
			logger.Error("task", "Failed to mount SMB share for task ", task.TaskID, ": ", err)
			MarkFailed(task.TaskID)
			return
		}

		inputPath = smb.BuildSMBPath(smbMountPath, task.SourceFileName)
		outputPath = smb.BuildSMBPath(smbMountPath, task.OutputName)

		logger.Info("task", "executeTranscode: SMB inputPath=%s, outputPath=%s", inputPath, outputPath)

		os.MkdirAll(filepath.Dir(outputPath), 0755)
	} else {
		inputPath = filepath.Join(taskDir, task.SourceFileName)
		logger.Info("task", "executeTranscode: taskDir=%s, inputPath=%s", taskDir, inputPath)

		chunkIndices := copyChunks(task)

		if err := assembleChunks(task, inputPath, chunkIndices); err != nil {
			logger.Error("task", "Failed to assemble chunks for task ", task.TaskID, ": ", err)
			MarkFailed(task.TaskID)
			return
		}
	}

	taskInfo := ffmpeg.TaskInfo{
		TaskID:         task.TaskID,
		SourceFileName: task.SourceFileName,
		OutputName:     task.OutputName,
		FFmpegArgs:     task.FFmpegArgs,
	}

	if task.IsSMBMode && smbMountPath != "" {
		task.OutputFilePath = outputPath
		saveTaskToDB(task)
		if err := ffmpeg.StartSMBTranscode(taskInfo, inputPath, outputPath); err != nil {
			logger.Error("task", "Failed to start SMB transcode for task ", task.TaskID, ": ", err)
			smb.UnmountSMBShare(smbMountPath)
			MarkFailed(task.TaskID)
			return
		}

		go func() {
			for {
				t := GetTask(task.TaskID)
				if t == nil || t.Status == StatusSuccess || t.Status == StatusFailed || t.Status == StatusCancelled {
					smb.UnmountSMBShare(smbMountPath)
					break
				}
				time.Sleep(1 * time.Second)
			}
		}()
	} else {
		if err := ffmpeg.StartTranscode(taskInfo, inputPath); err != nil {
			logger.Error("task", "Failed to start transcode for task ", task.TaskID, ": ", err)
			MarkFailed(task.TaskID)
			return
		}
	}

	go monitorProgress(task.TaskID)
}

func copyChunks(task *Task) []int {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	indices := make([]int, 0, len(task.ReceivedChunks))
	for idx := range task.ReceivedChunks {
		indices = append(indices, idx)
	}
	// 重新初始化而非置 nil，防止迟到的分片写入 nil map 导致 panic
	task.ReceivedChunks = make(map[int]struct{})
	return indices
}

func assembleChunks(task *Task, outputPath string, chunkIndices []int) error {
	cfg := config.Get()
	var taskDir string
	if filepath.IsAbs(cfg.TempDir) {
		taskDir = filepath.Join(cfg.TempDir, task.TaskID)
	} else {
		taskDir = filepath.Join(getAppDir(), cfg.TempDir, task.TaskID)
	}
	logger.Info("task", "assembleChunks: taskDir=%s", taskDir)

	tempFile := filepath.Join(taskDir, "temp_upload.dat")

	if _, err := os.Stat(tempFile); os.IsNotExist(err) {
		return fmt.Errorf("temp upload file not found: %s", tempFile)
	}

	if err := os.Rename(tempFile, outputPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

func monitorProgress(taskID string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		t := GetTask(taskID)
		if t == nil {
			return
		}

		if t.Status == StatusSuccess || t.Status == StatusFailed || t.Status == StatusCancelled {
			return
		}

		progress := ffmpeg.GetProgress(taskID)
		if progress > t.Progress {
			updateProgress(taskID, progress)
		}
	}
}

// updateProgress 只更新内存中的进度，不写数据库，避免高频 I/O 阻塞 mutex。
func updateProgress(taskID string, progress float64) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	if task, ok := manager.tasks[taskID]; ok {
		task.Progress = progress
		task.UpdatedAt = time.Now()
	}
	fireTaskUpdateLocked(taskID, false)
}

func fireTaskUpdateLocked(taskID string, force bool) {
	if onTaskUpdate == nil {
		return
	}

	t, ok := manager.tasks[taskID]
	if !ok {
		return
	}

	now := time.Now()
	lastTime, tOk := manager.lastNotifyTime[taskID]
	lastProg, pOk := manager.lastNotifyProg[taskID]

	shouldFire := force
	if !shouldFire {
		if !tOk || !pOk {
			shouldFire = true
		} else if now.Sub(lastTime) >= 3*time.Second {
			shouldFire = true
		} else if t.Progress-lastProg >= 1.0 {
			shouldFire = true
		}
	}

	if shouldFire {
		manager.lastNotifyTime[taskID] = now
		manager.lastNotifyProg[taskID] = t.Progress
		go onTaskUpdate(taskID)
	}
}

func CreateTask(taskID, sourceFileName, outputName, clientConnID string, ffmpegArgs string) *Task {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	task := &Task{
		TaskID:         taskID,
		Status:         StatusCreated,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		SourceFileName: sourceFileName,
		OutputName:     outputName,
		ClientConnID:   clientConnID,
		Priority:       PriorityNormal,
		FFmpegArgs:     ffmpegArgs,
		Progress:       0,
		ReceivedChunks: make(map[int]struct{}),
		UploadComplete: false,
	}

	manager.tasks[taskID] = task
	saveTaskToDB(task)

	return task
}

func CreateSMBTask(taskID, sourceFileName, outputName, clientConnID, ffmpegArgs, smbPath, smbUser, smbPassword string) *Task {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	task := &Task{
		TaskID:         taskID,
		Status:         StatusCreated,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		SourceFileName: sourceFileName,
		OutputName:     outputName,
		ClientConnID:   clientConnID,
		Priority:       PriorityNormal,
		FFmpegArgs:     ffmpegArgs,
		Progress:       0,
		ReceivedChunks: make(map[int]struct{}),
		UploadComplete: false,
		IsSMBMode:      true,
		SMBPath:        smbPath,
		SMBUser:        smbUser,
		SMBPassword:    smbPassword,
	}

	manager.tasks[taskID] = task
	saveTaskToDB(task)

	return task
}

func GetTask(taskID string) *Task {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()
	return manager.tasks[taskID]
}

func GetAllTasks() []*Task {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()

	tasks := make([]*Task, 0, len(manager.tasks))
	for _, task := range manager.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

func GetActiveTasks() []*Task {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()

	tasks := make([]*Task, 0)
	for _, task := range manager.tasks {
		if task.Status != StatusSuccess && task.Status != StatusFailed && task.Status != StatusCancelled {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks
}

func UpdateTask(taskID string, updates map[string]interface{}) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	task, ok := manager.tasks[taskID]
	if !ok {
		return
	}

	if status, ok := updates["status"].(TaskStatus); ok {
		task.Status = status
	}
	if progress, ok := updates["progress"].(float64); ok {
		task.Progress = progress
	}
	if outputPath, ok := updates["outputFilePath"].(string); ok {
		task.OutputFilePath = outputPath
	}
	if resolution, ok := updates["resolution"].(string); ok {
		task.Resolution = resolution
	}
	if bitrate, ok := updates["bitrate"].(string); ok {
		task.Bitrate = bitrate
	}
	if totalChunks, ok := updates["totalChunks"].(int); ok {
		task.TotalChunks = totalChunks
	}

	task.UpdatedAt = time.Now()
	saveTaskToDB(task)
}

func PauseTask(taskID string) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	task, ok := manager.tasks[taskID]
	if !ok {
		return
	}

	if task.Status != StatusWaiting && task.Status != StatusTranscoding {
		return
	}

	wasRunning := task.Status == StatusTranscoding
	task.Status = StatusPaused
	task.UpdatedAt = time.Now()
	if wasRunning {
		manager.runningCount--
		maybeAllowSleep()
	}

	for i, t := range manager.waitingQueue {
		if t.TaskID == taskID {
			manager.waitingQueue = append(manager.waitingQueue[:i], manager.waitingQueue[i+1:]...)
			break
		}
	}

	saveTaskToDB(task)
	fireTaskUpdateLocked(taskID, true)

	if wasRunning {
		ffmpeg.StopTranscode(taskID)
	}
}

func ResumeTask(taskID string) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	task, ok := manager.tasks[taskID]
	if !ok {
		return
	}

	if task.Status != StatusPaused {
		return
	}

	task.Status = StatusWaiting
	task.UpdatedAt = time.Now()
	manager.waitingQueue = append(manager.waitingQueue, task)

	saveTaskToDB(task)
	fireTaskUpdateLocked(taskID, true)
}

func CancelTask(taskID string) {
	manager.mutex.Lock()
	task, ok := manager.tasks[taskID]
	if !ok {
		manager.mutex.Unlock()
		return
	}

	if task.uploadFile != nil {
		task.uploadFile.Close()
		task.uploadFile = nil
	}

	wasRunning := task.Status == StatusTranscoding
	task.Status = StatusCancelled
	task.UpdatedAt = time.Now()
	if wasRunning {
		manager.runningCount--
		maybeAllowSleep()
	}

	for i, t := range manager.waitingQueue {
		if t.TaskID == taskID {
			manager.waitingQueue = append(manager.waitingQueue[:i], manager.waitingQueue[i+1:]...)
			break
		}
	}

	saveTaskToDB(task)
	fireTaskUpdateLocked(taskID, true)
	manager.mutex.Unlock()

	// 进程终止和文件清理在锁外执行，避免 I/O 阻塞其他任务操作
	if wasRunning {
		ffmpeg.StopTranscode(taskID)
	}
	cleanupTaskFiles(task)
}

func MarkUploading(taskID string) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	task, ok := manager.tasks[taskID]
	if !ok {
		return
	}

	task.Status = StatusUploading
	task.UpdatedAt = time.Now()
	saveTaskToDB(task)
}

func MarkUploadComplete(taskID string) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	task, ok := manager.tasks[taskID]
	if !ok {
		logger.Error("task", "MarkUploadComplete: task not found: %s", taskID)
		return
	}

	// 幂等：已标记完成的任务不重复入队，防止双重调度
	if task.UploadComplete {
		logger.Info("task", "MarkUploadComplete: already completed, skip: %s", taskID)
		return
	}

	if task.uploadFile != nil {
		task.uploadFile.Close()
		task.uploadFile = nil
		logger.Info("task", "MarkUploadComplete: closed upload file for task %s", taskID)
	}

	logger.Info("task", "MarkUploadComplete: taskID=%s, status=%s -> %s, waitingQueue size=%d",
		taskID, task.Status, StatusWaiting, len(manager.waitingQueue))

	task.Status = StatusWaiting
	task.UploadComplete = true
	task.UpdatedAt = time.Now()
	manager.waitingQueue = append(manager.waitingQueue, task)

	saveTaskToDB(task)
	fireTaskUpdateLocked(taskID, true)
}

func MarkSuccess(taskID string, outputPath string) {
	manager.mutex.Lock()
	task, ok := manager.tasks[taskID]
	if !ok {
		manager.mutex.Unlock()
		return
	}
	// 已处于终态或已暂停/取消，不重复处理（防止 runningCount 重复递减）
	if task.Status != StatusTranscoding {
		manager.mutex.Unlock()
		return
	}

	task.Status = StatusSuccess
	task.OutputFilePath = outputPath
	task.Progress = 100
	task.UpdatedAt = time.Now()
	manager.runningCount--
	maybeAllowSleep()

	saveTaskToDB(task)
	fireTaskUpdateLocked(taskID, true)
	manager.mutex.Unlock()
}

func MarkFailed(taskID string) {
	manager.mutex.Lock()
	task, ok := manager.tasks[taskID]
	if !ok {
		manager.mutex.Unlock()
		return
	}
	// 仅处理正在转码的任务，防止暂停/取消后 waitForCompletion 回调导致 runningCount 重复递减
	if task.Status != StatusTranscoding {
		manager.mutex.Unlock()
		return
	}

	if task.uploadFile != nil {
		task.uploadFile.Close()
		task.uploadFile = nil
	}

	task.Status = StatusFailed
	task.UpdatedAt = time.Now()
	manager.runningCount--
	maybeAllowSleep()

	saveTaskToDB(task)
	fireTaskUpdateLocked(taskID, true)
	manager.mutex.Unlock()

	// 文件清理在锁外执行，避免 I/O 阻塞其他任务操作
	cleanupTaskFiles(task)
}

func HandleClientDisconnect(clientConnID string) {
	manager.mutex.Lock()

	toCleanup := make([]*Task, 0)
	for _, task := range manager.tasks {
		if task.ClientConnID == clientConnID {
			switch task.Status {
			case StatusCreated, StatusUploading:
				// 正在上传的任务，客户端断开后无法继续，标记为断开并清理
				if task.uploadFile != nil {
					task.uploadFile.Close()
					task.uploadFile = nil
				}
				task.Status = StatusClientDisconnect
				task.UpdatedAt = time.Now()

				saveTaskToDB(task)
				toCleanup = append(toCleanup, task)
			case StatusWaiting:
				// 等待中的任务，保留在队列中，等待客户端重新连接
				// 客户端重新连接后可以继续处理这些任务
				logger.Info("task", "Client disconnected but task %s is still waiting, keeping in queue", task.TaskID)
			}
		}
	}
	manager.mutex.Unlock()

	// 文件清理在锁外执行，避免 I/O 阻塞其他任务操作
	for _, task := range toCleanup {
		cleanupTaskFiles(task)
	}
}

func ClearWaitingTasks() {
	manager.mutex.Lock()
	taskIDs := make([]string, 0, len(manager.waitingQueue))
	for _, task := range manager.waitingQueue {
		taskIDs = append(taskIDs, task.TaskID)
	}
	manager.mutex.Unlock()

	for _, taskID := range taskIDs {
		CancelTask(taskID)
	}
}

func ClearAllTasks() {
	manager.mutex.Lock()
	taskIDs := make([]string, 0, len(manager.tasks))
	for taskID := range manager.tasks {
		taskIDs = append(taskIDs, taskID)
	}
	manager.mutex.Unlock()

	for _, taskID := range taskIDs {
		CancelTask(taskID)
	}
}

func saveTaskToDB(task *Task) {
	if manager.db == nil {
		return
	}

	query := `
	INSERT OR REPLACE INTO task_list (
		task_id, status, created_at, updated_at, source_file_name,
		output_file_path, progress, client_conn_id, priority, ffmpeg_args,
		resolution, bitrate, file_cleaned, total_chunks,
		is_smb_mode, smb_path, smb_user, smb_password
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := manager.db.Exec(
		query,
		task.TaskID,
		task.Status,
		task.CreatedAt.Format(time.RFC3339),
		task.UpdatedAt.Format(time.RFC3339),
		task.SourceFileName,
		task.OutputFilePath,
		task.Progress,
		task.ClientConnID,
		task.Priority,
		task.FFmpegArgs,
		task.Resolution,
		task.Bitrate,
		btoi(task.FileCleaned),
		task.TotalChunks,
		btoi(task.IsSMBMode),
		task.SMBPath,
		task.SMBUser,
		task.SMBPassword,
	)

	if err != nil {
		logger.Error("task", "Failed to save task: ", err)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cleanupTaskFiles(task *Task) {
	if task.FileCleaned {
		return
	}

	cfg := config.Get()
	var taskDir string
	if filepath.IsAbs(cfg.TempDir) {
		taskDir = filepath.Join(cfg.TempDir, task.TaskID)
	} else {
		taskDir = filepath.Join(getAppDir(), cfg.TempDir, task.TaskID)
	}
	if err := os.RemoveAll(taskDir); err == nil {
		task.FileCleaned = true
		saveTaskToDB(task)
		logger.Info("task", "Cleaned task files: ", task.TaskID)
	}
}

func CleanupTaskFiles(taskID string) {
	manager.mutex.Lock()
	task, ok := manager.tasks[taskID]
	if !ok {
		manager.mutex.Unlock()
		return
	}
	manager.mutex.Unlock()

	cleanupTaskFiles(task)
}

func StoreChunk(taskID string, chunkIndex int, reader io.Reader) error {
	manager.mutex.Lock()
	task, ok := manager.tasks[taskID]
	if !ok {
		manager.mutex.Unlock()
		return fmt.Errorf("task not found")
	}

	// 上传已完成的任务拒绝接收迟到的分片，防止写入 nil map 或已关闭的文件
	if task.Status != StatusCreated && task.Status != StatusUploading {
		manager.mutex.Unlock()
		return fmt.Errorf("task is not in uploadable state: %s", task.Status)
	}

	task.ReceivedChunks[chunkIndex] = struct{}{}

	cfg := config.Get()
	var taskDir string
	if filepath.IsAbs(cfg.TempDir) {
		taskDir = filepath.Join(cfg.TempDir, taskID)
	} else {
		taskDir = filepath.Join(getAppDir(), cfg.TempDir, taskID)
	}

	if err := os.MkdirAll(taskDir, 0755); err != nil {
		manager.mutex.Unlock()
		return err
	}

	var file *os.File
	if task.uploadFile == nil {
		tempFile := filepath.Join(taskDir, "temp_upload.dat")
		f, err := os.OpenFile(tempFile, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			manager.mutex.Unlock()
			return err
		}
		task.uploadFile = f
		file = f
	} else {
		file = task.uploadFile
	}
	manager.mutex.Unlock()

	// 用文件锁保护 Seek+Write，防止并发分片互相覆盖偏移量
	task.fileMu.Lock()
	defer task.fileMu.Unlock()

	chunkSize := cfg.ChunkSize
	if chunkSize == 0 {
		chunkSize = 10 * 1024 * 1024
	}
	offset := int64(chunkIndex) * chunkSize

	if _, err := file.Seek(offset, 0); err != nil {
		return err
	}

	buffer := make([]byte, 4*1024*1024) // 4MB 缓冲区，减少系统调用次数
	if _, err := io.CopyBuffer(file, reader, buffer); err != nil {
		return err
	}

	return nil
}

func GetChunk(taskID string, chunkIndex int) (bool, error) {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()

	task, ok := manager.tasks[taskID]
	if !ok {
		return false, fmt.Errorf("task not found")
	}

	_, ok = task.ReceivedChunks[chunkIndex]
	return ok, nil
}

func GetTaskChunkCount(taskID string) int {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()

	task, ok := manager.tasks[taskID]
	if !ok {
		return 0
	}

	return len(task.ReceivedChunks)
}

func SetHTTPPort(port int) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	manager.httpPort = port
}

func GetHTTPPort() int {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()
	return manager.httpPort
}

func GetRunningCount() int {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()
	return manager.runningCount
}

// maybeAllowSleep 当没有运行中的任务时恢复系统默认休眠行为。
// 调用方必须已持有 manager.mutex。
func maybeAllowSleep() {
	if manager.runningCount == 0 {
		winapi.AllowSleep()
		logger.Info("task", "Allow system sleep: no running task")
	}
}

func GetWaitingCount() int {
	manager.mutex.RLock()
	defer manager.mutex.RUnlock()
	return len(manager.waitingQueue)
}

func Stop() {
	close(manager.stopChan)
	if manager.db != nil {
		manager.db.Close()
	}
}
