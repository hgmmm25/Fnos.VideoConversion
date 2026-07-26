package main

import (
	"context"
	"errors"
	"fmt"
	"fvcc/smbshare"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fvcc/logger"
)

// Scheduler 任务调度器，每 1 秒执行一次状态分发。
// 主循环仅做快速状态检查与分发，IO 操作（上传/下载）丢入独立协程。
type Scheduler struct {
	store      *Store
	remote     *RemoteClient
	hub        *Hub
	pv         *PathValidator
	processing sync.Map // taskID -> bool，防止同一任务并发处理
	cancelMap  sync.Map // taskID -> context.CancelFunc，用于取消上传
	chunkSize  int64
}

// NewScheduler 创建调度器。
func NewScheduler(store *Store, remote *RemoteClient, hub *Hub, pv *PathValidator) *Scheduler {
	return &Scheduler{
		store:     store,
		remote:    remote,
		hub:       hub,
		pv:        pv,
		chunkSize: 4 * 1024 * 1024, // 4MB 分片
	}
}

// Start 启动 1 秒调度循环。
func (s *Scheduler) Start() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	logger.Info("scheduler", "started, interval=1s")

	for range ticker.C {
		s.tick()
	}
}

func (s *Scheduler) tick() {
	// 1. 回收过期锁
	s.store.SweepExpiredLocks()

	// 2. 处理已取消任务：移至历史
	tasks := s.store.GetTasks()
	for _, t := range tasks {
		if t.Status == StatusCancelled {
			s.store.MoveToHistory(t.ID)
			s.hub.BroadcastTaskUpdate(t.ID, string(StatusCancelled), 0, "已取消")
			continue
		}
	}

	// 3. 遍历活跃任务，分发状态
	// 按 OrderID 排序，保证任务按创建顺序处理（OrderID 越小优先级越高）
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if tasks[j].OrderID < tasks[i].OrderID {
				tasks[i], tasks[j] = tasks[j], tasks[i]
			}
		}
	}

	// 先处理 QUEUE 任务：串行检查，找到第一个可推进的任务后停止
	// 这样保证按 OrderID 顺序推进，不会多个任务同时通过槽位检查
	for _, t := range tasks {
		if t.Status != StatusQueue {
			continue
		}
		if t.Status.IsTerminal() || t.Status == StatusCancelled || t.Status == StatusPaused {
			continue
		}
		if t.CoolDownUntil != nil && time.Now().Before(*t.CoolDownUntil) {
			continue
		}
		if _, loaded := s.processing.LoadOrStore(t.ID, true); loaded {
			continue
		}
		go s.processTask(t)
		break // 只启动一个 QUEUE 任务，其他留到下次循环
	}

	// 再处理其他状态任务，并发处理
	for _, t := range tasks {
		if t.Status == StatusQueue {
			continue // QUEUE 任务已在上一轮处理
		}
		if t.Status.IsTerminal() || t.Status == StatusCancelled || t.Status == StatusPaused {
			continue
		}
		if t.CoolDownUntil != nil && time.Now().Before(*t.CoolDownUntil) {
			continue
		}
		if _, loaded := s.processing.LoadOrStore(t.ID, true); loaded {
			continue
		}
		go s.processTask(t)
	}
}

// processTask 处理单个任务的状态机流转。
func (s *Scheduler) processTask(t Task) {
	defer s.processing.Delete(t.ID)

	logger.Debug("scheduler", "processing task: id=%s status=%s file=%s", t.ID, t.Status, t.FileName)

	switch t.Status {
	case StatusQueue:
		s.handleQueue(t)
	case StatusUploading:
		// 上传在 handleQueue 中启动的协程内持续推进，此处仅检查远端状态
		s.checkUploadProgress(t)
	case StatusWaitingTrans:
		s.checkTranscodeProgress(t)
	case StatusTranscoding:
		s.checkTranscodeProgress(t)
	case StatusWaitingDown:
		s.startDownload(t)
	case StatusDownloading:
		// 下载在 startDownload 协程内持续推进
		s.checkDownloadProgress(t)
	case StatusError:
		s.handleError(t)
	}
}

// checkLocalTranscodeProgress 检查本地转码进度（用于状态同步）。
func (s *Scheduler) checkLocalTranscodeProgress(t Task) {
	progress := GetLocalTranscodeProgress(t.ID)
	if progress > t.Progress && progress < 100 {
		s.store.UpdateTaskStatus(t.ID, StatusTranscoding, progress, "")
		s.hub.BroadcastTaskUpdate(t.ID, string(StatusTranscoding), progress, "")
	}
}

// hasWaitingTransTask 检查指定服务器是否已有处于 WAITING_TRANS 或 UPLOADING 状态的任务。
// 每台服务器同一时刻最多保留一个待转码/上传中的任务，避免远端队列堆积。
// UPLOADING 也算占用槽位，因为上传完成后就会进入 WAITING_TRANS。
func (s *Scheduler) hasWaitingTransTask(serverID string) bool {
	tasks := s.store.GetTasks()
	for _, t := range tasks {
		if t.ServerID == serverID && (t.Status == StatusWaitingTrans || t.Status == StatusUploading) {
			return true
		}
	}
	return false
}

// handleQueue QUEUE → UPLOADING：获取传输锁，启动上传协程。
func (s *Scheduler) handleQueue(t Task) {
	var server Server
	if t.ServerID == "_local_" {
		server = Server{
			ID:      "_local_",
			Name:    "fnNAS 自转码",
			Status:  "online",
			IsLocal: true,
		}
	} else {
		var ok bool
		server, ok = s.store.GetServer(t.ServerID)
		if !ok {
			logger.Error("scheduler", "handleQueue failed: server not found, task=%s", t.ID)
			s.failTask(t, "服务器不存在", NonRetryable)
			return
		}
	}

	if server.IsLocal {
		s.handleLocalQueue(t, server)
		return
	}

	if server.Status == "offline" {
		logger.Warn("scheduler", "handleQueue failed: server offline, task=%s", t.ID)
		s.failTaskWithCooldown(t, "服务器离线", Retryable, 30)
		return
	}

	// 校验源文件存在（SMB模式下文件也在本地fnOS上，同样需要校验）
	settings := s.store.GetSettings()
	if _, err := os.Stat(t.SourceFile); err != nil {
		logger.Error("scheduler", "handleQueue failed: source file not found: %s, err=%v", t.SourceFile, err)
		s.failTask(t, fmt.Sprintf("源文件不存在: %s", t.SourceFile), NonRetryable)
		return
	}

	// 获取传输锁
	lockSec := server.LockExpireSec
	if lockSec == 0 {
		lockSec = 120
	}
	if !s.store.AcquireTransLock(server.ID, t.ID, lockSec) {
		return
	}

	// 连接远端服务器
	_, chunkSizeMB, err := s.remote.Connect(server)
	if err != nil {
		logger.Warn("scheduler", "handleQueue failed: connect server failed: %v", err)
		s.store.ReleaseTransLock(server.ID, t.ID)
		s.failTaskWithCooldown(t, fmt.Sprintf("连接服务器失败: %v", err), Retryable, 30)
		return
	}

	chunkSize := int64(chunkSizeMB) * 1024 * 1024

	// 每台服务器同一时刻最多一个待转码（WAITING_TRANS）任务
	// 如果该服务器已有待转码任务，保持 QUEUE 状态，等下一轮调度再检查
	if s.hasWaitingTransTask(server.ID) {
		s.store.ReleaseTransLock(server.ID, t.ID)
		return
	}

	if settings.TransferMode == "smb" {
		// SMB模式：拼接SMB URL供FVCS挂载访问
		// 源文件和输出文件可以是不同共享，需分别构建SMB URL
		localIP, err := smbshare.GetLocalIP()
		if err != nil {
			logger.Error("scheduler", "handleQueue failed: get local IP: %v", err)
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("获取本机IP失败: %v", err), NonRetryable)
			return
		}

		sourceSMBURL, err := smbshare.BuildSMBURL(localIP, settings.SMBUser, t.SourceFile)
		if err != nil {
			logger.Error("scheduler", "handleQueue failed: build source SMB URL: %v", err)
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("构建源文件SMB路径失败: %v", err), NonRetryable)
			return
		}

		outputSMBURL, err := smbshare.BuildSMBURL(localIP, settings.SMBUser, t.OutputFile)
		if err != nil {
			logger.Error("scheduler", "handleQueue failed: build output SMB URL: %v", err)
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("构建输出文件SMB路径失败: %v", err), NonRetryable)
			return
		}

		// 源文件和输出文件可能属于不同共享，使用源文件共享的SMB路径作为SMBPath
		// FVCS挂载后，SourceFileName和OutputName使用相对于挂载点的路径
		// 但若属于不同共享，需要FVCS分别挂载——当前设计简化为：要求源和输出在同一共享内
		sourceShare, err := smbshare.FindShareForPath(settings.SMBUser, t.SourceFile)
		if err != nil {
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("查找源文件共享失败: %v", err), NonRetryable)
			return
		}
		outputShare, err := smbshare.FindShareForPath(settings.SMBUser, t.OutputFile)
		if err != nil {
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("查找输出文件共享失败: %v", err), NonRetryable)
			return
		}
		if sourceShare.Name != outputShare.Name {
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, "SMB模式要求源文件和输出文件在同一共享目录内", NonRetryable)
			return
		}

		// SMBPath为共享根URL（\\ip\sharename），SourceFileName/OutputName为共享内相对路径
		smbBasePath := fmt.Sprintf("\\\\%s\\%s", localIP, sourceShare.Name)
		sourceRel := strings.TrimPrefix(t.SourceFile, sourceShare.Path)
		sourceRel = strings.TrimPrefix(sourceRel, string(filepath.Separator))
		outputRel := strings.TrimPrefix(t.OutputFile, outputShare.Path)
		outputRel = strings.TrimPrefix(outputRel, string(filepath.Separator))

		remoteTaskID, err := s.remote.CreateSMBTask(server, sourceRel, outputRel, t.FFmpegArgs, smbBasePath, settings.SMBUser, settings.SMBPassword)
		if err != nil {
			logger.Warn("scheduler", "handleQueue failed: create SMB task failed: %v", err)
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTaskWithCooldown(t, fmt.Sprintf("创建SMB任务失败: %v", err), Retryable, 30)
			return
		}

		_ = sourceSMBURL
		_ = outputSMBURL

		t.RemoteTaskID = remoteTaskID
		t.Status = StatusWaitingTrans
		t.Progress = 0
		t.ErrorMsg = ""
		s.store.UpsertTask(t)
		s.hub.BroadcastTaskUpdate(t.ID, string(StatusWaitingTrans), 0, "SMB模式，等待转码")

		s.store.ReleaseTransLock(server.ID, t.ID)
		return
	}

	// HTTP模式：创建远端任务
	remoteTaskID, err := s.remote.CreateTask(server, t.FileName, t.OutputName, t.FFmpegArgs)
	if err != nil {
		logger.Warn("scheduler", "handleQueue failed: create remote task failed: %v", err)
		s.store.ReleaseTransLock(server.ID, t.ID)
		s.failTaskWithCooldown(t, fmt.Sprintf("创建远端任务失败: %v", err), Retryable, 30)
		return
	}

	t.RemoteTaskID = remoteTaskID
	t.Status = StatusUploading
	t.Progress = 0
	t.UploadChunkIdx = 0
	t.ErrorMsg = ""
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(StatusUploading), 0, "")

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelMap.Store(t.ID, cancel)

	go s.doUpload(ctx, t, server, chunkSize)
}

// doUpload 执行分片上传，持续推进直到完成或失败。
func (s *Scheduler) doUpload(ctx context.Context, t Task, server Server, chunkSize int64) {
	defer s.cancelMap.Delete(t.ID)

	logger.Info("upload", "Starting upload for task %s, file=%s, chunkSize=%d", t.ID, t.SourceFile, chunkSize)

	file, err := os.Open(t.SourceFile)
	if err != nil {
		s.failTask(t, fmt.Sprintf("打开源文件失败: %v", err), NonRetryable)
		s.store.ReleaseTransLock(server.ID, t.ID)
		return
	}
	defer file.Close()

	fileInfo, _ := file.Stat()
	logger.Debug("upload", "File opened: %s, size=%d", t.SourceFile, fileInfo.Size())

	if t.UploadChunkIdx > 0 {
		logger.Info("upload", "Resuming from chunk %d", t.UploadChunkIdx)
		_, err = file.Seek(int64(t.UploadChunkIdx)*chunkSize, 0)
		if err != nil {
			s.failTask(t, fmt.Sprintf("定位文件偏移失败: %v", err), NonRetryable)
			s.store.ReleaseTransLock(server.ID, t.ID)
			return
		}
	}

	buf := make([]byte, chunkSize)
	index := t.UploadChunkIdx

	for {
		select {
		case <-ctx.Done():
			logger.Info("upload", "Task %s cancelled via context", t.ID)
			s.store.ReleaseTransLock(server.ID, t.ID)
			return
		default:
		}

		cur, ok := s.store.GetTask(t.ID)
		if !ok || cur.Status == StatusCancelled || cur.Status == StatusPaused {
			logger.Info("upload", "Task %s cancelled or paused", t.ID)
			s.store.ReleaseTransLock(server.ID, t.ID)
			return
		}

		n, err := file.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			isLast := err != nil

			if uerr := s.remote.UploadChunk(ctx, server, t.RemoteTaskID, index, isLast, chunk); uerr != nil {
				logger.Warn("upload", "Failed to upload chunk %d: %v", index, uerr)
				cur, _ := s.store.GetTask(t.ID)
				if cur.Status == StatusCancelled || cur.Status == StatusPaused {
					s.store.ReleaseTransLock(server.ID, t.ID)
					return
				}

				remoteStatus, _, qerr := s.remote.QueryTask(server, t.RemoteTaskID)
				if qerr == nil && (remoteStatus == "Cancelled" || remoteStatus == "Failed") {
					logger.Warn("upload", "Remote task %s status is %s, marking as cancelled", t.RemoteTaskID, remoteStatus)
					cur.Status = StatusCancelled
					cur.ErrorMsg = "服务器已终止任务"
					s.store.UpsertTask(cur)
					s.hub.BroadcastTaskUpdate(cur.ID, string(StatusCancelled), cur.Progress, "服务器已终止任务")
					s.store.ReleaseTransLock(server.ID, t.ID)
					return
				}

				cur.UploadChunkIdx = index
				s.failTaskWithCooldown(cur, fmt.Sprintf("上传分片 %d 失败: %v", index, uerr), Retryable, 30)
				s.store.ReleaseTransLock(server.ID, t.ID)
				return
			}

			index++
			progress := float64(index) * float64(chunkSize) / float64(fileSizeSafe(t.SourceFile)) * 100
			if progress > 100 {
				progress = 100
			}
			s.store.UpdateTaskStatus(t.ID, StatusUploading, progress, "")
			s.hub.BroadcastTaskUpdate(t.ID, string(StatusUploading), progress, "")
		}
		if err != nil {
			break
		}
	}

	logger.Info("upload", "All chunks uploaded, calling UploadFinish for task %s", t.ID)

	if err := s.remote.UploadFinish(server, t.RemoteTaskID); err != nil {
		logger.Warn("upload", "UploadFinish failed: %v, querying remote task to confirm state", err)
		remoteStatus, _, qerr := s.remote.QueryTask(server, t.RemoteTaskID)
		if qerr == nil && remoteStatus != "Created" && remoteStatus != "Uploading" {
			logger.Info("upload", "UploadFinish failed but remote already advanced to status=%s, treating as success: task=%s", remoteStatus, t.ID)
		} else {
			logger.Warn("upload", "QueryTask after UploadFinish failed or still in Uploading/Created: err=%v, status=%s", qerr, remoteStatus)
			s.failTaskWithCooldown(t, fmt.Sprintf("通知上传完成失败: %v", err), Retryable, 30)
			s.store.ReleaseTransLock(server.ID, t.ID)
			return
		}
	}

	logger.Info("upload", "UploadFinish succeeded, task %s entering StatusWaitingTrans", t.ID)

	s.store.ReleaseTransLock(server.ID, t.ID)
	s.store.UpdateTaskStatus(t.ID, StatusWaitingTrans, 100, "")
	s.hub.BroadcastTaskUpdate(t.ID, string(StatusWaitingTrans), 100, "上传完成，等待转码")
}

// checkUploadProgress 上传协程在自行推进，此处为空操作占位。
func (s *Scheduler) checkUploadProgress(t Task) {
	// 上传由 doUpload 协程持续推进，主循环不干预
}

// checkTranscodeProgress 查询转码进度。
func (s *Scheduler) checkTranscodeProgress(t Task) {
	server, ok := s.store.GetServer(t.ServerID)
	if !ok {
		s.failTask(t, "服务器不存在", NonRetryable)
		return
	}

	if server.IsLocal {
		s.checkLocalTranscodeProgress(t)
		return
	}

	status, progress, err := s.remote.QueryTask(server, t.RemoteTaskID)
	if err != nil {
		logger.Warn("scheduler", "checkTranscodeProgress: query failed: task=%s remote=%s err=%v", t.ID, t.RemoteTaskID, err)

		time.Sleep(500 * time.Millisecond)
		_, _, retryErr := s.remote.QueryTask(server, t.RemoteTaskID)
		if retryErr != nil {
			logger.Error("scheduler", "checkTranscodeProgress: retry query also failed: task=%s remote=%s err=%v", t.ID, t.RemoteTaskID, retryErr)
			s.failTaskWithCooldown(t, fmt.Sprintf("查询转码进度失败: %v", err), Retryable, 30)
			return
		}
		err = nil
	}

	// 重新读取任务状态，防止在查询期间任务已被取消或暂停
	currentTask, ok := s.store.GetTask(t.ID)
	if !ok || currentTask.Status.IsTerminal() || currentTask.Status == StatusPaused {
		return
	}

	if status == "Cancelled" {
		t.Status = StatusCancelled
		t.ErrorMsg = "服务器已终止任务"
		s.store.UpsertTask(t)
		s.hub.BroadcastTaskUpdate(t.ID, string(StatusCancelled), t.Progress, "服务器已终止任务")
		return
	}

	mappedProgress := progress
	if mappedProgress < 0 {
		mappedProgress = 0
	} else if mappedProgress > 100 {
		mappedProgress = 100
	}

	enteringTranscodingFromWait := t.Status == StatusWaitingTrans && status == "Transcoding"
	if enteringTranscodingFromWait {
		mappedProgress = 0
	} else if mappedProgress < t.Progress && mappedProgress < 100 {
		mappedProgress = t.Progress
	}

	switch status {
	case "Success":
		settings := s.store.GetSettings()
		if settings.TransferMode == "smb" {
			s.store.UpdateTaskStatus(t.ID, StatusCompleted, 100, "")
			s.hub.BroadcastTaskUpdate(t.ID, string(StatusCompleted), 100, "SMB模式转码完成")
			s.maybeDeleteSourceFile(t)
			time.AfterFunc(5*time.Second, func() {
				s.store.MoveToHistory(t.ID)
			})
		} else {
			s.store.UpdateTaskStatus(t.ID, StatusWaitingDown, 100, "")
			s.hub.BroadcastTaskUpdate(t.ID, string(StatusWaitingDown), 100, "转码完成，等待下载")
		}
	case "Failed":
		s.failTask(t, "远端转码失败", NonRetryable)
	case "Cancelled":
		s.store.UpdateTaskStatus(t.ID, StatusCancelled, 0, "远端任务已取消")
	case "Transcoding":
		s.store.UpdateTaskStatus(t.ID, StatusTranscoding, mappedProgress, "")
		s.hub.BroadcastTaskUpdate(t.ID, string(StatusTranscoding), mappedProgress, "")
	case "Waiting", "Created", "Uploading":
		// 仍在排队，保持状态
	default:
		// 未知状态，保持
	}
}

// startDownload WAITING_DOWN → DOWNLOADING：获取传输锁，启动下载协程。
func (s *Scheduler) startDownload(t Task) {
	server, ok := s.store.GetServer(t.ServerID)
	if !ok {
		s.failTask(t, "服务器不存在", NonRetryable)
		return
	}

	lockSec := server.LockExpireSec
	if lockSec == 0 {
		lockSec = 120
	}
	if !s.store.AcquireTransLock(server.ID, t.ID, lockSec) {
		return
	}

	// 校验输出路径
	if err := s.pv.Validate(t.OutputFile); err != nil {
		s.store.ReleaseTransLock(server.ID, t.ID)
		s.failTask(t, fmt.Sprintf("输出路径校验失败: %v", err), NonRetryable)
		return
	}

	// 确保输出目录存在
	os.MkdirAll(filepath.Dir(t.OutputFile), 0o755)

	t.Status = StatusDownloading
	if t.DownloadOffset == 0 {
		t.Progress = 0
	}
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(StatusDownloading), t.Progress, "")

	go s.doDownload(t, server)
}

// doDownload 执行 Range 断点续传下载。
func (s *Scheduler) doDownload(t Task, server Server) {
	// 打开输出文件（追加模式）
	flag := os.O_CREATE | os.O_WRONLY
	if t.DownloadOffset > 0 {
		flag |= os.O_APPEND
	}
	f, err := os.OpenFile(t.OutputFile, flag, 0o644)
	if err != nil {
		s.failTask(t, fmt.Sprintf("创建输出文件失败: %v", err), NonRetryable)
		s.store.ReleaseTransLock(server.ID, t.ID)
		return
	}
	defer f.Close()

	// 下载（支持断点续传）
	onProgress := func(written, total int64) {
		progress := written * 100 / total
		s.store.UpdateTaskStatus(t.ID, StatusDownloading, float64(progress), "")
		s.hub.BroadcastTaskUpdate(t.ID, string(StatusDownloading), float64(progress), "")
	}
	n, err := s.remote.DownloadFileWithProgress(server, t.RemoteTaskID, t.DownloadOffset, f, onProgress)
	if err != nil {
		// 记录已下载偏移
		cur, _ := s.store.GetTask(t.ID)
		cur.DownloadOffset += n
		s.failTaskWithCooldown(cur, fmt.Sprintf("下载失败: %v", err), Retryable, 30)
		s.store.ReleaseTransLock(server.ID, t.ID)

		s.cleanupIncompleteFiles(t)

		return
	}

	// 下载完成，通知远端
	if err := s.remote.DownloadFinish(server, t.RemoteTaskID); err != nil {
		logger.Warn("download", "DownloadFinish 通知失败: %v", err)
	}

	// 释放锁，标记完成
	s.store.ReleaseTransLock(server.ID, t.ID)
	s.store.UpdateTaskStatus(t.ID, StatusCompleted, 100, "")
	s.hub.BroadcastTaskUpdate(t.ID, string(StatusCompleted), 100, "转码完成")
	s.maybeDeleteSourceFile(t)

	// 延迟移入历史
	time.AfterFunc(5*time.Second, func() {
		s.store.MoveToHistory(t.ID)
	})
}

// checkDownloadProgress 下载协程在自行推进，此处为空操作占位。
func (s *Scheduler) checkDownloadProgress(t Task) {
	// 下载由 doDownload 协程持续推进
}

// handleError 错误状态处理：可重试且重试次数 < 3 则回到 QUEUE。
func (s *Scheduler) handleError(t Task) {
	if t.RetryType == Retryable && t.RetryCount < 3 {
		t.RetryCount++
		t.Status = StatusQueue
		t.CoolDownUntil = nil
		s.store.UpsertTask(t)
		s.hub.BroadcastTaskUpdate(t.ID, string(StatusQueue), t.Progress, fmt.Sprintf("第 %d 次重试", t.RetryCount))
	} else {
		// 不可重试或重试次数用尽，移入历史
		s.store.MoveToHistory(t.ID)
		s.hub.BroadcastTaskUpdate(t.ID, string(StatusError), t.Progress, t.ErrorMsg)
	}
}

// failTask 标记任务错误。
func (s *Scheduler) failTask(t Task, msg string, retryType RetryType) {
	t.Status = StatusError
	t.ErrorMsg = msg
	t.RetryType = retryType
	if retryType == NonRetryable {
		t.CoolDownSec = 0
	} else {
		t.CoolDownSec = 30
	}
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(StatusError), t.Progress, msg)
	logger.Error("task", "%s ERROR: %s (%s)", t.ID, msg, retryType)

	if retryType == NonRetryable {
		s.cleanupIncompleteFiles(t)
	}
}

// failTaskWithCooldown 标记任务错误并设置冷却时间。
func (s *Scheduler) failTaskWithCooldown(t Task, msg string, retryType RetryType, coolDownSec int) {
	t.Status = StatusError
	t.ErrorMsg = msg
	t.RetryType = retryType
	t.CoolDownSec = coolDownSec
	if coolDownSec > 0 {
		coolDown := time.Now().Add(time.Duration(coolDownSec) * time.Second)
		t.CoolDownUntil = &coolDown
	}
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(StatusError), t.Progress, msg)
	logger.Error("task", "%s ERROR: %s (%s, cooldown=%ds)", t.ID, msg, retryType, coolDownSec)

	if retryType == NonRetryable {
		s.cleanupIncompleteFiles(t)
	}
}

// fileSizeSafe 安全获取文件大小，失败返回 1。
func fileSizeSafe(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 1
}

// cleanupIncompleteFiles 清理任务失败或取消时的未完成文件。
func (s *Scheduler) cleanupIncompleteFiles(t Task) {
	if t.OutputFile == "" {
		return
	}

	if err := os.Remove(t.OutputFile); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("cleanup", "Failed to remove incomplete output file %s: %v", t.OutputFile, err)
		}
	} else {
		logger.Info("cleanup", "Removed incomplete output file %s", t.OutputFile)
	}
}

// maybeDeleteSourceFile 根据转码方案的 DeleteSource 设置，转码成功后删除源文件。
func (s *Scheduler) maybeDeleteSourceFile(t Task) {
	if t.ProfileID == "" || t.SourceFile == "" {
		return
	}
	profile, ok := s.store.GetProfile(t.ProfileID)
	if !ok || !profile.DeleteSource {
		return
	}
	// 仅当输出文件存在时才删除源文件，避免转码实际失败却误删源文件
	if _, err := os.Stat(t.OutputFile); err != nil {
		logger.Info("deleteSource", "Output file not found, skip deleting source: %s", t.OutputFile)
		return
	}
	if err := os.Remove(t.SourceFile); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("deleteSource", "Failed to remove source file %s: %v", t.SourceFile, err)
		}
	} else {
		logger.Info("deleteSource", "Removed source file after successful transcode: %s", t.SourceFile)
	}
}

// handleLocalQueue 处理本地转码任务：QUEUE → TRANSCODING。
func (s *Scheduler) handleLocalQueue(t Task, server Server) {
	if _, err := os.Stat(t.SourceFile); err != nil {
		logger.Error("scheduler", "handleLocalQueue failed: source file not found: %s, err=%v", t.SourceFile, err)
		s.failTask(t, fmt.Sprintf("源文件不存在: %s", t.SourceFile), NonRetryable)
		return
	}

	// 检查并发限制
	settings := s.store.GetSettings()
	maxConcurrent := settings.MaxLocalTranscodeCount
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	currentRunning := int(atomic.LoadInt32(&runningCount))
	if currentRunning >= maxConcurrent {
		logger.Info("scheduler", "Local transcode queue full: current=%d max=%d, task=%s waiting", currentRunning, maxConcurrent, t.ID)
		t.Status = StatusQueue
		s.store.UpsertTask(t)
		return
	}

	os.MkdirAll(filepath.Dir(t.OutputFile), 0o755)

	t.Status = StatusTranscoding
	t.Progress = 0
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(StatusTranscoding), 0, "开始本地转码")

	// 增加运行计数
	atomic.AddInt32(&runningCount, 1)

	logger.Info("scheduler", "Starting local transcode for task %s (running=%d/%d)", t.ID, int(atomic.LoadInt32(&runningCount)), maxConcurrent)

	err := StartLocalTranscode(
		t.ID,
		t.SourceFile,
		t.OutputFile,
		t.FFmpegArgs,
		func(progress float64) {
			s.store.UpdateTaskStatus(t.ID, StatusTranscoding, progress, "")
			s.hub.BroadcastTaskUpdate(t.ID, string(StatusTranscoding), progress, "")
		},
		func(err error) {
			if err != nil {
				logger.Error("scheduler", "Local transcode failed for task %s: %v", t.ID, err)
				s.failTask(t, fmt.Sprintf("本地转码失败: %v", err), NonRetryable)
			} else {
				logger.Info("scheduler", "Local transcode completed for task %s", t.ID)
				s.store.UpdateTaskStatus(t.ID, StatusCompleted, 100, "")
				s.hub.BroadcastTaskUpdate(t.ID, string(StatusCompleted), 100, "转码完成")
				s.maybeDeleteSourceFile(t)
				time.AfterFunc(5*time.Second, func() {
					s.store.MoveToHistory(t.ID)
				})
			}
		},
	)

	if err != nil {
		logger.Error("scheduler", "Failed to start local transcode for task %s: %v", t.ID, err)
		s.failTask(t, fmt.Sprintf("启动本地转码失败: %v", err), NonRetryable)
	}
}
