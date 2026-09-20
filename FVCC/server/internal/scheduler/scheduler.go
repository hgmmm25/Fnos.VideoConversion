package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fvcc/internal/edl"
	"fvcc/internal/media"
	"fvcc/internal/node"
	"fvcc/internal/remote"
	"fvcc/internal/security"
	"fvcc/internal/store"
	"fvcc/internal/store/model"
	"fvcc/internal/ws"
	"fvcc/logger"
	"fvcc/smbshare"
)

// Scheduler 任务调度器，每 1 秒执行一次状态分发。
// 主循环仅做快速状态检查与分发，IO 操作（上传/下载）丢入独立协程。
type Scheduler struct {
	store      *store.Store
	remote     *remote.RemoteClient
	hub        *ws.Hub
	pv         *security.PathValidator
	dispatcher remote.RenderDispatcher // 渲染下发通道（B-06 注入；为 nil 时渲染任务保持 QUEUE）
	sel        *node.Selector   // 节点选机/健康分/熔断/能力落库（B-08；P2-1 B轮迁入 internal/node）
	processing sync.Map // taskID -> bool，防止同一任务并发处理
	cancelMap  sync.Map // taskID -> context.CancelFunc，用于取消上传
	chunkSize  int64

	// proxyWf：M4 代理工作流收尾（04 §3.4；P2-1 B轮迁入 internal/media，实现见 internal/media/proxy.go）。
	proxyWf *media.ProxyWorkflow
}

// SetRenderDispatcher 注入渲染下发通道（B-06）。B-05 单测与 main.go 装配均走此入口。
func (s *Scheduler) SetRenderDispatcher(d remote.RenderDispatcher) {
	s.dispatcher = d
}

// NewScheduler 创建调度器。
func NewScheduler(store *store.Store, remote *remote.RemoteClient, hub *ws.Hub, pv *security.PathValidator) *Scheduler {
	s := &Scheduler{
		store:     store,
		remote:    remote,
		hub:       hub,
		pv:        pv,
		sel:       node.NewSelector(store, hub), // 节点选机与健康分（B-08）：指标窗口迁入 internal/node
		chunkSize: model.DefaultChunkSizeMB * 1024 * 1024, // 默认 4MB 分片（与设置项 ChunkSizeMB 同源，MB→字节换算）
	}
	// M4 代理收尾（internal/media）：失败收口走调度域 failRenderTaskPermanent，
	// 源/代理本地根推导由本包 security 域（resolveMediaRootsFor）注入，与提交侧同源。
	s.proxyWf = media.NewProxyWorkflow(store, hub, remote, s,
		media.ProxyPathResolverFunc(func(payload model.GenProxyPayload) (srcLocal, proxyLocal, code, msg string) {
			roots := media.ResolveMediaRoots(store, pv)
			if roots.SourceLocal == "" {
				return "", "", media.ErrCodeRootUnknown, "未配置素材根（videoRoot），无法校验代理"
			}
			srcLocal = roots.SourceLocal
			if sr := strings.TrimSpace(payload.SourceRoot); sr != "" {
				srcLocal = media.LocalPathOf(sr)
			}
			proxyLocal = media.ProxyLocalForSrcRoot(store, pv, srcLocal)
			if proxyLocal == "" {
				return "", "", media.ErrCodeRootUnknown, "未配置代理根（proxyRoot），无法校验代理"
			}
			return filepath.Join(srcLocal, filepath.FromSlash(payload.SrcFile)),
				filepath.Join(proxyLocal, filepath.FromSlash(payload.ProxyFile)), "", ""
		}))
	return s
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

	// 2. 全量任务快照只取一次（P1-1 调度器优化：原实现此处与第 4 步各调一次
	// GetTasks()，1s tick 下每轮产生两份完整副本拷贝；合并后仅一份，降低
	// 高频调度下的分配与排序开销，且语义不变——sortQueueTasks 只排序不修改 store）
	tasks := s.store.GetTasks()
	for _, t := range tasks {
		if t.Status == model.StatusCancelled {
			s.store.MoveToHistory(t.ID)
			s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusCancelled), 0, "已取消")
			continue
		}
	}

	// 3. 冷却到期扫描（06 §4.3）：COOLDOWN 且 next_retry_at <= now → QUEUE。
	// 必须排在"选队首"之前，否则本 tick 已到期的任务要再等一轮才能被调度。
	s.promoteCooldownTasks(time.Now())

	// 4. 遍历活跃任务，分发状态
	// 排序键 = (优先级档位, OrderID)（06 §4.1）：GEN_PROXY 强制低优，
	// 不得插到 RENDER_EDL/TRANSCODE 之前；同档位按 OrderID 升序（越小越先）。
	tasks = sortQueueTasks(tasks)

	// 先处理 QUEUE 任务：串行检查，只推进一个，其他留到下次循环
	if t, ok := s.nextQueueTask(tasks); ok {
		go s.processTask(t)
	}

	// 再处理其他状态任务，并发处理
	for _, t := range tasks {
		if t.Status == model.StatusQueue {
			continue // QUEUE 任务已在上一轮处理
		}
		if t.Status.IsTerminal() || t.Status == model.StatusCancelled || t.Status == model.StatusPaused {
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
func (s *Scheduler) processTask(t model.Task) {
	defer s.processing.Delete(t.ID)

	logger.Debug("scheduler", "processing task: id=%s status=%s file=%s", t.ID, t.Status, t.FileName)

	switch t.Status {
	case model.StatusQueue:
		// 任务分流（06 §4.1）：三种类型共用同一 tasks 表 / tick / OrderID，仅入口不同。
		switch t.TaskType {
		case model.TaskTypeRenderEDL:
			s.handleRenderEDL(t)
		case model.TaskTypeGenProxy:
			s.handleGenProxy(t)
		default:
			s.handleQueue(t) // ""(兼容旧)/TRANSCODE 既有路径，零改动
		}
	case model.StatusCooldown:
		// 冷却中的任务只等 tick 的到期扫描转回 QUEUE（06 §4.3），此处不推进状态。
		return
	case model.StatusUploading:
		// 上传在 handleQueue 中启动的协程内持续推进，此处仅检查远端状态
		s.checkUploadProgress(t)
	case model.StatusWaitingTrans:
		s.checkTranscodeProgress(t)
	case model.StatusTranscoding:
		if isRenderTask(t) {
			// 渲染类任务（RENDER_EDL/GEN_PROXY）的 RUNNING 进度由 B-07 经 WS 事件聚合推进，
			// 不得走既有 HTTP 转码探测（checkTranscodeProgress），避免被误判为失败。
			// M4：GEN_PROXY 额外按窗口轮询 FVCS 终态，成功后做校验/登记/proxy_ready
			// （04 §3.4，实现见 proxy_flow.go）。
			if t.TaskType == model.TaskTypeGenProxy {
				s.proxyWf.Poll(t)
			}
			return
		}
		s.checkTranscodeProgress(t)
	case model.StatusWaitingDown:
		s.startDownload(t)
	case model.StatusDownloading:
		// 下载在 startDownload 协程内持续推进
		s.checkDownloadProgress(t)
	case model.StatusError:
		s.handleError(t)
	}
}

// checkLocalTranscodeProgress 检查本地转码进度（用于状态同步）。
func (s *Scheduler) checkLocalTranscodeProgress(t model.Task) {
	progress := media.GetLocalTranscodeProgress(t.ID)
	if progress > t.Progress && progress < 100 {
		s.store.UpdateTaskStatus(t.ID, model.StatusTranscoding, progress, "")
		s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusTranscoding), progress, "")
	}
}

// hasWaitingTransTask 检查指定服务器是否已有处于 WAITING_TRANS 或 UPLOADING 状态的任务。
// 每台服务器同一时刻最多保留一个待转码/上传中的任务，避免远端队列堆积。
// UPLOADING 也算占用槽位，因为上传完成后就会进入 WAITING_TRANS。
func (s *Scheduler) hasWaitingTransTask(serverID string) bool {
	tasks := s.store.GetTasks()
	for _, t := range tasks {
		if t.ServerID == serverID && (t.Status == model.StatusWaitingTrans || t.Status == model.StatusUploading) {
			return true
		}
	}
	return false
}

// handleQueue QUEUE → UPLOADING：获取传输锁，启动上传协程。
func (s *Scheduler) handleQueue(t model.Task) {
	var server model.Server
	if t.ServerID == "_local_" {
		server = model.Server{
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
			s.failTask(t, "服务器不存在", model.NonRetryable)
			return
		}
	}

	if server.IsLocal {
		s.handleLocalQueue(t, server)
		return
	}

	if server.Status == "offline" {
		logger.Warn("scheduler", "handleQueue failed: server offline, task=%s", t.ID)
		s.failTaskWithCooldown(t, "服务器离线", model.Retryable, 30)
		return
	}

	// 校验源文件存在（SMB模式下文件也在本地fnOS上，同样需要校验）
	settings := s.store.GetSettings()
	if _, err := os.Stat(t.SourceFile); err != nil {
		logger.Error("scheduler", "handleQueue failed: source file not found: %s, err=%v", t.SourceFile, err)
		s.failTask(t, fmt.Sprintf("源文件不存在: %s", t.SourceFile), model.NonRetryable)
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
		s.failTaskWithCooldown(t, fmt.Sprintf("连接服务器失败: %v", err), model.Retryable, 30)
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
			s.failTask(t, fmt.Sprintf("获取本机IP失败: %v", err), model.NonRetryable)
			return
		}

		sourceSMBURL, err := smbshare.BuildSMBURL(localIP, settings.SMBUser, t.SourceFile)
		if err != nil {
			logger.Error("scheduler", "handleQueue failed: build source SMB URL: %v", err)
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("构建源文件SMB路径失败: %v", err), model.NonRetryable)
			return
		}

		outputSMBURL, err := smbshare.BuildSMBURL(localIP, settings.SMBUser, t.OutputFile)
		if err != nil {
			logger.Error("scheduler", "handleQueue failed: build output SMB URL: %v", err)
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("构建输出文件SMB路径失败: %v", err), model.NonRetryable)
			return
		}

		// 源文件和输出文件可能属于不同共享，使用源文件共享的SMB路径作为SMBPath
		// FVCS挂载后，SourceFileName和OutputName使用相对于挂载点的路径
		// 但若属于不同共享，需要FVCS分别挂载——当前设计简化为：要求源和输出在同一共享内
		sourceShare, err := smbshare.FindShareForPath(settings.SMBUser, t.SourceFile)
		if err != nil {
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("查找源文件共享失败: %v", err), model.NonRetryable)
			return
		}
		outputShare, err := smbshare.FindShareForPath(settings.SMBUser, t.OutputFile)
		if err != nil {
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, fmt.Sprintf("查找输出文件共享失败: %v", err), model.NonRetryable)
			return
		}
		if sourceShare.Name != outputShare.Name {
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTask(t, "SMB模式要求源文件和输出文件在同一共享目录内", model.NonRetryable)
			return
		}

		// SMBPath为共享根URL（\\ip\sharename），SourceFileName/OutputName为共享内相对路径
		smbBasePath := fmt.Sprintf("\\\\%s\\%s", localIP, sourceShare.Name)
		sourceRel := strings.TrimPrefix(t.SourceFile, sourceShare.Path)
		sourceRel = strings.TrimPrefix(sourceRel, string(filepath.Separator))
		outputRel := strings.TrimPrefix(t.OutputFile, outputShare.Path)
		outputRel = strings.TrimPrefix(outputRel, string(filepath.Separator))

		remoteTaskID, err := s.remote.CreateSMBTaskWithTrace(server, sourceRel, outputRel, t.FFmpegArgs, smbBasePath, settings.SMBUser, settings.SMBPassword, t.TraceID)
		if err != nil {
			logger.Warn("scheduler", "handleQueue failed: create SMB task failed: %v", err)
			s.store.ReleaseTransLock(server.ID, t.ID)
			s.failTaskWithCooldown(t, fmt.Sprintf("创建SMB任务失败: %v", err), model.Retryable, 30)
			return
		}

		_ = sourceSMBURL
		_ = outputSMBURL

		t.RemoteTaskID = remoteTaskID
		t.Status = model.StatusWaitingTrans
		t.Progress = 0
		t.ErrorMsg = ""
		s.store.UpsertTask(t)
		s.store.SetTaskStartedAt(t.ID, time.Now())
		s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusWaitingTrans), 0, "SMB模式，等待转码")

		s.store.ReleaseTransLock(server.ID, t.ID)
		return
	}

	// HTTP模式：创建远端任务
	remoteTaskID, err := s.remote.CreateTask(server, t.FileName, t.OutputName, t.FFmpegArgs)
	if err != nil {
		logger.Warn("scheduler", "handleQueue failed: create remote task failed: %v", err)
		s.store.ReleaseTransLock(server.ID, t.ID)
		s.failTaskWithCooldown(t, fmt.Sprintf("创建远端任务失败: %v", err), model.Retryable, 30)
		return
	}

	t.RemoteTaskID = remoteTaskID
	t.Status = model.StatusUploading
	t.Progress = 0
	t.UploadChunkIdx = 0
	t.ErrorMsg = ""
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusUploading), 0, "")

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelMap.Store(t.ID, cancel)

	go s.doUpload(ctx, t, server, chunkSize)
}

// doUpload 执行分片上传，持续推进直到完成或失败。
func (s *Scheduler) doUpload(ctx context.Context, t model.Task, server model.Server, chunkSize int64) {
	defer s.cancelMap.Delete(t.ID)

	logger.Info("upload", "Starting upload for task %s, file=%s, chunkSize=%d", t.ID, t.SourceFile, chunkSize)

	file, err := os.Open(t.SourceFile)
	if err != nil {
		s.failTask(t, fmt.Sprintf("打开源文件失败: %v", err), model.NonRetryable)
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
			s.failTask(t, fmt.Sprintf("定位文件偏移失败: %v", err), model.NonRetryable)
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
		if !ok || cur.Status == model.StatusCancelled || cur.Status == model.StatusPaused {
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
				if cur.Status == model.StatusCancelled || cur.Status == model.StatusPaused {
					s.store.ReleaseTransLock(server.ID, t.ID)
					return
				}

				remoteStatus, _, qerr := s.remote.QueryTask(server, t.RemoteTaskID)
				if qerr == nil && (remoteStatus == "Cancelled" || remoteStatus == "Failed") {
					logger.Warn("upload", "Remote task %s status is %s, marking as cancelled", t.RemoteTaskID, remoteStatus)
					cur.Status = model.StatusCancelled
					cur.ErrorMsg = "服务器已终止任务"
					s.store.UpsertTask(cur)
					s.hub.BroadcastTaskUpdate(cur.ID, string(model.StatusCancelled), cur.Progress, "服务器已终止任务")
					s.store.ReleaseTransLock(server.ID, t.ID)
					return
				}

				cur.UploadChunkIdx = index
				s.failTaskWithCooldown(cur, fmt.Sprintf("上传分片 %d 失败: %v", index, uerr), model.Retryable, 30)
				s.store.ReleaseTransLock(server.ID, t.ID)
				return
			}

			index++
			progress := float64(index) * float64(chunkSize) / float64(fileSizeSafe(t.SourceFile)) * 100
			if progress > 100 {
				progress = 100
			}
			s.store.UpdateTaskStatus(t.ID, model.StatusUploading, progress, "")
			s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusUploading), progress, "")
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
			s.failTaskWithCooldown(t, fmt.Sprintf("通知上传完成失败: %v", err), model.Retryable, 30)
			s.store.ReleaseTransLock(server.ID, t.ID)
			return
		}
	}

	logger.Info("upload", "UploadFinish succeeded, task %s entering model.StatusWaitingTrans", t.ID)

	s.store.ReleaseTransLock(server.ID, t.ID)
	s.store.UpdateTaskStatus(t.ID, model.StatusWaitingTrans, 100, "")
	s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusWaitingTrans), 100, "上传完成，等待转码")
}

// checkUploadProgress 上传协程在自行推进，此处为空操作占位。
func (s *Scheduler) checkUploadProgress(t model.Task) {
	// 上传由 doUpload 协程持续推进，主循环不干预
}

// checkTranscodeProgress 查询转码进度。
func (s *Scheduler) checkTranscodeProgress(t model.Task) {
	server, ok := s.store.GetServer(t.ServerID)
	if !ok {
		s.failTask(t, "服务器不存在", model.NonRetryable)
		return
	}

	if server.IsLocal {
		s.checkLocalTranscodeProgress(t)
		return
	}

	rs, err := s.remote.QueryTaskDetail(server, t.RemoteTaskID)
	if err != nil {
		logger.Warn("scheduler", "checkTranscodeProgress: query failed: task=%s remote=%s err=%v", t.ID, t.RemoteTaskID, err)

		time.Sleep(500 * time.Millisecond)
		rs2, retryErr := s.remote.QueryTaskDetail(server, t.RemoteTaskID)
		if retryErr != nil {
			logger.Error("scheduler", "checkTranscodeProgress: retry query also failed: task=%s remote=%s err=%v", t.ID, t.RemoteTaskID, retryErr)
			s.failTaskWithCooldown(t, fmt.Sprintf("查询转码进度失败: %v", err), model.Retryable, 30)
			return
		}
		rs = rs2
		err = nil // 重试成功：避免旧错误影响后续分支判定
	}
	status, progress := rs.Status, rs.Progress

	// 重新读取任务状态，防止在查询期间任务已被取消或暂停
	currentTask, ok := s.store.GetTask(t.ID)
	if !ok || currentTask.Status.IsTerminal() || currentTask.Status == model.StatusPaused {
		return
	}

	if status == "Cancelled" {
		t.Status = model.StatusCancelled
		t.ErrorMsg = "服务器已终止任务"
		s.store.UpsertTask(t)
		s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusCancelled), t.Progress, "服务器已终止任务")
		return
	}

	mappedProgress := progress
	if mappedProgress < 0 {
		mappedProgress = 0
	} else if mappedProgress > 100 {
		mappedProgress = 100
	}

	enteringTranscodingFromWait := t.Status == model.StatusWaitingTrans && status == "Transcoding"
	if enteringTranscodingFromWait {
		mappedProgress = 0
	} else if mappedProgress < t.Progress && mappedProgress < 100 {
		mappedProgress = t.Progress
	}

	switch status {
	case "Success":
		settings := s.store.GetSettings()
		if settings.TransferMode == "smb" {
			s.store.UpdateTaskStatus(t.ID, model.StatusCompleted, 100, "")
			s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusCompleted), 100, "SMB模式转码完成")
			s.maybeDeleteSourceFile(t)
			time.AfterFunc(5*time.Second, func() {
				s.store.MoveToHistory(t.ID)
			})
		} else {
			s.store.UpdateTaskStatus(t.ID, model.StatusWaitingDown, 100, "")
			s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusWaitingDown), 100, "转码完成，等待下载")
		}
	case "Failed":
		// 失败节点细化上报：优先采用节点返回的错误码/阶段/明细，缺失时回退原泛化文案。
		msg := media.RemoteFailureMessage("远端转码失败", rs)
		logger.Error("scheduler", "checkTranscodeProgress: remote failed: task=%s remote=%s detail=%s", t.ID, t.RemoteTaskID, msg)
		s.failTask(t, msg, model.NonRetryable)
	case "Cancelled":
		s.store.UpdateTaskStatus(t.ID, model.StatusCancelled, 0, "远端任务已取消")
	case "Transcoding":
		s.store.UpdateTaskStatus(t.ID, model.StatusTranscoding, mappedProgress, "")
		s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusTranscoding), mappedProgress, "")
	case "Waiting", "Created", "Uploading":
		// 仍在排队，保持状态
	default:
		// 未知状态，保持
	}
}

// startDownload WAITING_DOWN → DOWNLOADING：获取传输锁，启动下载协程。
func (s *Scheduler) startDownload(t model.Task) {
	server, ok := s.store.GetServer(t.ServerID)
	if !ok {
		s.failTask(t, "服务器不存在", model.NonRetryable)
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
		s.failTask(t, fmt.Sprintf("输出路径校验失败: %v", err), model.NonRetryable)
		return
	}

	// 确保输出目录存在
	os.MkdirAll(filepath.Dir(t.OutputFile), 0o755)

	t.Status = model.StatusDownloading
	if t.DownloadOffset == 0 {
		t.Progress = 0
	}
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusDownloading), t.Progress, "")

	go s.doDownload(t, server)
}

// doDownload 执行 Range 断点续传下载。
func (s *Scheduler) doDownload(t model.Task, server model.Server) {
	// 打开输出文件（追加模式）
	flag := os.O_CREATE | os.O_WRONLY
	if t.DownloadOffset > 0 {
		flag |= os.O_APPEND
	}
	f, err := os.OpenFile(t.OutputFile, flag, 0o644)
	if err != nil {
		s.failTask(t, fmt.Sprintf("创建输出文件失败: %v", err), model.NonRetryable)
		s.store.ReleaseTransLock(server.ID, t.ID)
		return
	}
	defer f.Close()

	// 下载（支持断点续传）
	onProgress := func(written, total int64) {
		progress := written * 100 / total
		s.store.UpdateTaskStatus(t.ID, model.StatusDownloading, float64(progress), "")
		s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusDownloading), float64(progress), "")
	}
	n, err := s.remote.DownloadFileWithProgress(server, t.RemoteTaskID, t.DownloadOffset, f, onProgress)
	if err != nil {
		// 记录已下载偏移
		cur, _ := s.store.GetTask(t.ID)
		cur.DownloadOffset += n
		s.failTaskWithCooldown(cur, fmt.Sprintf("下载失败: %v", err), model.Retryable, 30)
		s.store.ReleaseTransLock(server.ID, t.ID)

		s.CleanupIncompleteFiles(t)

		return
	}

	// 下载完成，通知远端
	if err := s.remote.DownloadFinish(server, t.RemoteTaskID); err != nil {
		logger.Warn("download", "DownloadFinish 通知失败: %v", err)
	}

	// 释放锁，标记完成
	s.store.ReleaseTransLock(server.ID, t.ID)
	s.store.UpdateTaskStatus(t.ID, model.StatusCompleted, 100, "")
	s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusCompleted), 100, "转码完成")
	s.maybeDeleteSourceFile(t)

	// 延迟移入历史
	time.AfterFunc(5*time.Second, func() {
		s.store.MoveToHistory(t.ID)
	})
}

// checkDownloadProgress 下载协程在自行推进，此处为空操作占位。
func (s *Scheduler) checkDownloadProgress(t model.Task) {
	// 下载由 doDownload 协程持续推进
}

// handleError 错误状态处理：可重试且重试次数 < 3 则回到 QUEUE。
func (s *Scheduler) handleError(t model.Task) {
	if t.RetryType == model.Retryable && t.RetryCount < 3 {
		t.RetryCount++
		t.Status = model.StatusQueue
		t.CoolDownUntil = nil
		s.store.UpsertTask(t)
		s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusQueue), t.Progress, fmt.Sprintf("第 %d 次重试", t.RetryCount))
	} else {
		// 不可重试或重试次数用尽，移入历史
		s.store.MoveToHistory(t.ID)
		s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusError), t.Progress, t.ErrorMsg)
	}
}

// failTask 标记任务错误。
func (s *Scheduler) failTask(t model.Task, msg string, retryType model.RetryType) {
	t.Status = model.StatusError
	t.ErrorMsg = msg
	t.RetryType = retryType
	if retryType == model.NonRetryable {
		t.CoolDownSec = 0
	} else {
		t.CoolDownSec = 30
	}
	now := time.Now()
	t.FinishedAt = &now
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusError), t.Progress, msg)
	logger.ErrorT("task", t.TraceID, "%s ERROR: %s (%s)", t.ID, msg, retryType)

	if retryType == model.NonRetryable {
		s.CleanupIncompleteFiles(t)
	}
}

// failTaskWithCooldown 标记任务错误并设置冷却时间。
func (s *Scheduler) failTaskWithCooldown(t model.Task, msg string, retryType model.RetryType, coolDownSec int) {
	t.Status = model.StatusError
	t.ErrorMsg = msg
	t.RetryType = retryType
	t.CoolDownSec = coolDownSec
	if coolDownSec > 0 {
		coolDown := time.Now().Add(time.Duration(coolDownSec) * time.Second)
		t.CoolDownUntil = &coolDown
	}
	now := time.Now()
	t.FinishedAt = &now
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusError), t.Progress, msg)
	logger.ErrorT("task", t.TraceID, "%s ERROR: %s (%s, cooldown=%ds)", t.ID, msg, retryType, coolDownSec)

	if retryType == model.NonRetryable {
		s.CleanupIncompleteFiles(t)
	}
}

// ===== B-05：渲染类任务分流与冷却调度（06 §4.1 / §4.3 / §4.5）=====

// renderMaxRetryDefault 渲染类任务重试上限（06 §4.3「MaxRetry 默认 5」）。
// 说明：既有转码路径使用 settings.maxRetry（DefaultSettings 默认 3），两者语义域不同；
// 渲染路径固定采用 06 §4.3 口径，保证退避序列 30/60/120/240/480s 可完整验证。
const renderMaxRetryDefault = 5

// defaultDispatchLockSec 派发锁缺省有效期（秒），节点未配置 lockExpireSec 时使用。
const defaultDispatchLockSec = 300

// ===== B-05 首次使用的错误码（06 §4.3 白名单）=====
// E_SMB_MOUNT_FAILED / E_RENDER_FAILED / E_PAYLOAD_MISSING 已随 remote 域收敛至
// internal/remote（P2-1 B轮），经 remote_shim.go 转发，此处保留调度域专属码。
const (
	errCodeTimeoutStall  = "E_TIMEOUT_STALL"
	errCodeDiskFull      = "E_DISK_FULL"
	errCodeFFmpegMissing = "E_FFMPEG_MISSING"
	// E_NODE_NOT_FOUND：06 §4.3 未定义，实现补充（见 10 号台账）。
	errCodeNodeNotFound = "E_NODE_NOT_FOUND"
	// errCodeCredentialMissing 挂载前缺凭据（代理 E_RENDER_FAILED 闭环，节点侧前置判定）。
	errCodeCredentialMissing = "E_CREDENTIAL_MISSING"
)

// retryableRenderErrs 可重试错误码白名单（06 §4.3）。
var retryableRenderErrs = []string{
	edl.ErrCodeNodeOffline, remote.ErrCodeSMBMountFailed, errCodeTimeoutStall, remote.ErrCodeRenderFailed,
}

// nonRetryableRenderErrs 显式不可重试错误码，判定优先于白名单（06 §4.3）。
var nonRetryableRenderErrs = []string{
	errCodeDiskFull, errCodeFFmpegMissing, edl.ErrCodeAssetMissing, edl.ErrCodeAssetNotInRoot,
	edl.ErrCodeEDLInvalid, edl.ErrCodeProfileInvalid, errCodeNodeNotFound, remote.ErrCodePayloadMissing,
	errCodeCredentialMissing,
}

// isRenderTask 判断是否渲染类任务（RENDER_EDL / GEN_PROXY）。
func isRenderTask(t model.Task) bool {
	return t.TaskType == model.TaskTypeRenderEDL || t.TaskType == model.TaskTypeGenProxy
}

// taskPriorityRank 调度优先级档位（06 §4.1）：GEN_PROXY 强制 low（=1），其余为 0。
func taskPriorityRank(t model.Task) int {
	if t.TaskType == model.TaskTypeGenProxy {
		return 1
	}
	return 0
}

// lessQueueTask QUEUE 排序键 = (优先级档位, OrderID)（06 §4.1）：
// 同档位按 OrderID 升序；代理任务档位更低，绝不会插到 RenderEDL/Transcode 之前。
func lessQueueTask(a, b model.Task) bool {
	ra, rb := taskPriorityRank(a), taskPriorityRank(b)
	if ra != rb {
		return ra < rb
	}
	return a.OrderID < b.OrderID
}

// sortQueueTasks 按调度优先级排序（06 §4.1），不改写原切片内容语义。
func sortQueueTasks(tasks []model.Task) []model.Task {
	sort.SliceStable(tasks, func(i, j int) bool { return lessQueueTask(tasks[i], tasks[j]) })
	return tasks
}

// nextQueueTask 按 (优先级档位, OrderID) 取第一个可推进的 QUEUE 任务，并标记 processing
// 防止同一任务并发处理。ok=false 表示本 tick 无任务可推进。
func (s *Scheduler) nextQueueTask(tasks []model.Task) (model.Task, bool) {
	now := time.Now()
	for _, t := range tasks {
		if t.Status != model.StatusQueue || t.Status.IsTerminal() || t.Status == model.StatusPaused {
			continue
		}
		if t.CoolDownUntil != nil && now.Before(*t.CoolDownUntil) {
			continue
		}
		if _, loaded := s.processing.LoadOrStore(t.ID, true); loaded {
			continue
		}
		return t, true
	}
	return model.Task{}, false
}

// handleRenderEDL RENDER_EDL 任务的 QUEUE 分发（06 §4.1）。
func (s *Scheduler) handleRenderEDL(t model.Task) { s.dispatchRenderLike(t) }

// handleGenProxy GEN_PROXY 任务的 QUEUE 分发（06 §4.1 / 04 §3.5）。
// 低优先级由调度排序（lessQueueTask）保证，本函数不再重复插队判断。
func (s *Scheduler) handleGenProxy(t model.Task) { s.dispatchRenderLike(t) }

// dispatchRenderLike 渲染类任务共用派发流程（06 §4.2）：
// 载荷预检 → 目标节点校验 → 取锁 → 下发 → RUNNING / COOLDOWN / FAILED。
func (s *Scheduler) dispatchRenderLike(t model.Task) {
	if strings.TrimSpace(t.PayloadJSON) == "" {
		// 载荷缺失属提交侧缺陷，重试无意义（06 §4.3：不计入重试，直接失败）
		s.failRenderTaskPermanent(t, remote.ErrCodePayloadMissing, "任务载荷为空（payload_json 缺失），无法下发")
		return
	}
	if s.dispatcher == nil {
		// TODO(B-06): remote.go 实现 RenderDispatcher 并经 main.go 注入后，本分支不再触发。
		logger.Warn("scheduler", "渲染下发通道未就绪(B-06)，任务保持 QUEUE: id=%s type=%s", t.ID, t.TaskType)
		return
	}

	server, ok := s.resolveRenderNode(t)
	if !ok {
		return // 状态已就地推进，或有意保持 QUEUE（等待 B-08 选机）
	}

	lockSec := server.LockExpireSec
	if lockSec <= 0 {
		lockSec = defaultDispatchLockSec
	}
	// 06 §4.5：转码锁保证同一节点同时只有一个"派发中"任务；编码锁为同 checksum 的跨实例冗余闸门。
	if !s.store.AcquireTransLock(server.ID, t.ID, lockSec) {
		logger.Info("scheduler", "节点已有派发中的任务，任务 %s 下轮再试 (server=%s)", t.ID, server.ID)
		return
	}
	if !s.store.AcquireCodeLock(server.ID, t.ID, lockSec) {
		s.store.ReleaseTransLock(server.ID, t.ID)
		logger.Info("scheduler", "编码锁被占用，任务 %s 下轮再试 (server=%s)", t.ID, server.ID)
		return
	}

	var remoteTaskID string
	var derr error
	s.NoteNodeAttempt(server.ID, time.Now()) // 节点选机与健康分（B-08）：失败率分母（06 §5.2）
	if t.TaskType == model.TaskTypeGenProxy {
		remoteTaskID, derr = s.dispatcher.CreateGenProxyWithTrace(server, t, t.TraceID)
	} else {
		remoteTaskID, derr = s.dispatcher.CreateRenderEDLWithTrace(server, t, t.TraceID)
	}
	// 释放时机（06 §4.5）：进入 RUNNING 或派发失败后立即释放。
	s.store.ReleaseCodeLock(server.ID, t.ID)
	s.store.ReleaseTransLock(server.ID, t.ID)

	if derr != nil {
		code, retryable := classifyRenderError(derr)
		s.NoteNodeFailure(server.ID, code, time.Now()) // 节点选机与健康分（B-08）：健康分/熔断入账
		if !retryable {
			s.failRenderTaskPermanent(t, code, derr.Error())
			return
		}
		s.failRenderTaskWithCooldown(t, code, derr.Error())
		return
	}

	// 派发成功 → RUNNING（线协议），server_id 落库，next_retry_at 清空（06 §4.2）。
	t.Status = model.StatusTranscoding
	t.ServerID = server.ID
	t.ServerName = server.Name
	t.RemoteTaskID = remoteTaskID
	t.Stage = model.StagePrepare
	t.SegIndex = 0
	t.CoolDownUntil = nil
	t.CoolDownSec = 0
	t.CooldownReason = ""
	t.ErrorMsg = ""
	s.store.UpsertTask(t)
	s.store.SetTaskStartedAt(t.ID, time.Now())
	s.hub.BroadcastTaskUpdate(t.ID, model.StatusTranscoding.WireName(), t.Progress, "已下发渲染节点，等待进度上报")
	s.NoteNodeSuccess(server.ID, time.Now()) // B-08：成功入账（探测任务成功即解除熔断）
	logger.InfoT("scheduler", t.TraceID, "已下发 %s 任务 %s → 节点 %s (remote=%s)", t.TaskType, t.ID, server.ID, remoteTaskID)
}

// ===== B-07：调度侧进度回传接通（06 §4.2 进度 loop / §6 事件聚合）=====

// HandleRemoteProgress 处理 FVCS 经 WS 回传的进度（B-07 接通链路）：
// 以 server_id + remote_task_id 反查本端任务（B-06 约定下发 TaskId 复用本端 ID，
// 故亦按本端 task_id 兜底匹配）→ 渲染任务落库 stage/seg/out_time_ms 并按 500ms
// 聚合广播（含「第 x/y 段」文案）；转码任务保持既有进度语义（(0,100] 且不回退）。
func (s *Scheduler) HandleRemoteProgress(serverID string, p remote.RemoteProgress) {
	if p.TaskID == "" {
		return
	}
	t, ok := s.findProgressTask(serverID, p.TaskID)
	if !ok {
		logger.Debug("scheduler", "忽略未匹配的进度上报: server=%s remote_task=%s", serverID, p.TaskID)
		return
	}

	if !isRenderTask(t) {
		// 既有转码路径：仅接受 (0,100] 且不回退的进度，保持原语义。
		if p.Progress <= 0 || p.Progress > 100 || t.Progress >= p.Progress {
			return
		}
		s.store.UpdateTaskStatus(t.ID, t.Status, p.Progress, "")
		s.hub.BroadcastTaskUpdate(t.ID, string(t.Status), p.Progress, "")
		return
	}

	updated, ok := s.store.ApplyRenderProgress(t.ID, store.RenderProgress{
		Progress:  p.Progress,
		Stage:     p.Stage,
		SegIndex:  p.SegIndex,
		SegTotal:  p.SegTotal,
		OutTimeMs: p.OutTimeMs,
		TotalMs:   p.TotalMs,
		Speed:     p.Speed,
	})
	if !ok {
		return
	}
	s.hub.BroadcastTaskUpdateFull(updated.ID, updated.Status.WireName(), int(updated.Progress),
		updated.Stage, renderSegInfo(updated), renderProgressLabel(updated),
		updated.OutTimeMs, updated.TotalMs, updated.Speed)
}

// findProgressTask 以 server_id + remote_task_id（或本端 task_id）反查进行中的任务（B-07）。
// 已收口任务（完成/取消/失败/暂停）不参与匹配，避免迟到上报把终态任务拉回中间态。
func (s *Scheduler) findProgressTask(serverID, remoteTaskID string) (model.Task, bool) {
	for _, t := range s.store.GetTasks() {
		if t.Status.IsTerminal() || t.Status == model.StatusError || t.Status == model.StatusPaused {
			continue
		}
		if serverID != "" && t.ServerID != "" && t.ServerID != serverID {
			continue
		}
		if t.RemoteTaskID != "" && t.RemoteTaskID == remoteTaskID {
			return t, true
		}
		if t.ID == remoteTaskID {
			return t, true
		}
	}
	return model.Task{}, false
}

// renderSegInfo 由任务快照构造分段进度；无分段信息（segTotal<=0）时返回 nil，
// 消息里不出现 seg 字段（06 §6）。
func renderSegInfo(t model.Task) *ws.SegInfo {
	if t.SegTotal <= 0 {
		return nil
	}
	return &ws.SegInfo{Index: t.SegIndex, Total: t.SegTotal}
}

// renderProgressLabel 把 stage/seg 聚合为前端可见文案（06 §6：分段进度聚合成「第 x/y 段」）。
func renderProgressLabel(t model.Task) string {
	switch t.Stage {
	case model.StagePrepare:
		return "准备中"
	case model.StageSegment:
		if t.SegTotal > 0 && t.SegIndex > 0 {
			return fmt.Sprintf("第 %d/%d 段", t.SegIndex, t.SegTotal)
		}
		return "分段渲染中"
	case model.StageConcat:
		return "拼接中"
	case model.StageMux:
		return "封装中"
	case model.StageFinalize:
		return "收尾中"
	default:
		return ""
	}
}

// BroadcastNodeStatus 广播渲染节点状态变化（06 §6，B-07）：
// status 变化（online↔offline）或 healthScore 跨 20 分档时推送；healthScore 未落地
// （B-08）时传 healthScoreUnknown，仅按状态变化广播。
func (s *Scheduler) BroadcastNodeStatus(serverID, status string, healthScore int, reason string) bool {
	return s.hub.BroadcastNodeStatus(serverID, status, healthScore, reason)
}

// resolveRenderNode 解析并校验渲染任务的目标节点（06 §4.2 选机 + §5.3 pickNode）。
// 显式指定 serverId 时校验存在性/在线/版本；未指定时按健康分+空闲槽+能力打分自动选机，
// 无可选节点则保持 QUEUE（不消耗重试次数，等节点恢复后自然推进）。ok=false 表示调用方直接返回。
func (s *Scheduler) resolveRenderNode(t model.Task) (model.Server, bool) {
	server, err := s.pickNode(t, time.Now())
	if err != nil {
		if errors.Is(err, node.ErrNoSelectableNode) {
			logger.Info("scheduler", "任务 %s 暂无可用渲染节点（离线/熔断/版本不符），保持 QUEUE", t.ID)
			return model.Server{}, false
		}
		code, retryable := classifyRenderError(err)
		if !retryable {
			s.failRenderTaskPermanent(t, code, err.Error())
			return model.Server{}, false
		}
		s.failRenderTaskWithCooldown(t, code, err.Error())
		return model.Server{}, false
	}
	return server, true
}

// classifyRenderError 从下发错误中提取错误码并判定是否可重试（06 §4.3）。
// 未识别错误归一为 E_RENDER_FAILED（白名单内、可重试），由重试上限兜底。
func classifyRenderError(err error) (code string, retryable bool) {
	if err == nil {
		return remote.ErrCodeRenderFailed, true
	}
	msg := err.Error()
	for _, c := range nonRetryableRenderErrs {
		if strings.Contains(msg, c) {
			return c, false
		}
	}
	for _, c := range retryableRenderErrs {
		if strings.Contains(msg, c) {
			return c, true
		}
	}
	return remote.ErrCodeRenderFailed, true
}

// FailPermanent 适配 media.ProxyFinalizer 接口：按 taskID 取出任务后走不可重试失败收口。
func (s *Scheduler) FailPermanent(taskID string, code, msg string) {
	t, ok := s.store.GetTask(taskID)
	if !ok {
		return
	}
	s.failRenderTaskPermanent(t, code, msg)
}

// failRenderTaskPermanent 渲染类任务的不可重试失败（06 §4.3）：状态置 ERROR（线协议 FAILED）、
// 不计入重试；下一 tick 由既有 handleError 归档到 task_history。
func (s *Scheduler) failRenderTaskPermanent(t model.Task, code, msg string) {
	t.Status = model.StatusError
	t.ErrorMsg = fmt.Sprintf("[%s] %s", code, msg)
	t.RetryType = model.NonRetryable
	t.CooldownReason = code
	t.CoolDownUntil = nil
	t.CoolDownSec = 0
	now := time.Now()
	t.FinishedAt = &now
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, model.StatusError.WireName(), t.Progress, t.ErrorMsg)
	logger.ErrorT("task", t.TraceID, "%s FAILED(%s): %s", t.ID, code, msg)
}

// failRenderTaskWithCooldown 渲染类任务派发失败 → COOLDOWN（06 §4.3）：
// attempt += 1，退避 cooldownSec = min(30×2^attempt, 600)（30/60/120/240/480s），
// next_retry_at = now + cooldownSec；达到重试上限则直接转 ERROR（FAILED）。
func (s *Scheduler) failRenderTaskWithCooldown(t model.Task, code, msg string) {
	attempt := t.RetryCount
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= renderMaxRetryDefault {
		s.failRenderTaskPermanent(t, code,
			fmt.Sprintf("重试次数用尽(%d/%d): %s", attempt, renderMaxRetryDefault, msg))
		return
	}
	coolSec := store.CooldownBackoffSec(attempt)
	until := time.Now().Add(time.Duration(coolSec) * time.Second)
	t.Status = model.StatusCooldown
	t.RetryCount = attempt + 1
	t.CoolDownSec = coolSec
	t.CoolDownUntil = &until
	t.CooldownReason = code
	t.RetryType = model.Retryable
	t.ErrorMsg = fmt.Sprintf("[%s] %s", code, msg)
	s.store.UpsertTask(t)
	s.hub.BroadcastTaskUpdate(t.ID, model.StatusCooldown.WireName(), t.Progress, t.ErrorMsg)
	logger.WarnT("scheduler", t.TraceID, "任务 %s → COOLDOWN %ds (code=%s, retry=%d/%d)",
		t.ID, coolSec, code, t.RetryCount, renderMaxRetryDefault)
}

// promoteCooldownTasks 冷却到期扫描（06 §4.3）：
// status=COOLDOWN 且 next_retry_at <= now → 置 QUEUE，保留原 OrderID（不改变用户可见顺序）。
func (s *Scheduler) promoteCooldownTasks(now time.Time) {
	for _, t := range s.store.ListCooldownDue(now) {
		if t.Status != model.StatusCooldown {
			continue
		}
		t.Status = model.StatusQueue
		t.CoolDownUntil = nil // next_retry_at = null
		t.CoolDownSec = 0
		s.store.UpsertTask(t)
		s.hub.BroadcastTaskUpdate(t.ID, model.StatusQueue.WireName(), t.Progress, "冷却到期，重新入队")
		logger.Info("scheduler", "冷却到期 → QUEUE: id=%s type=%s attempt=%d orderID=%d",
			t.ID, t.TaskType, t.RetryCount, t.OrderID)
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
func (s *Scheduler) CleanupIncompleteFiles(t model.Task) {
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
func (s *Scheduler) maybeDeleteSourceFile(t model.Task) {
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
func (s *Scheduler) handleLocalQueue(t model.Task, server model.Server) {
	if _, err := os.Stat(t.SourceFile); err != nil {
		logger.Error("scheduler", "handleLocalQueue failed: source file not found: %s, err=%v", t.SourceFile, err)
		s.failTask(t, fmt.Sprintf("源文件不存在: %s", t.SourceFile), model.NonRetryable)
		return
	}

	// 检查并发限制
	settings := s.store.GetSettings()
	maxConcurrent := settings.MaxLocalTranscodeCount
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	currentRunning := int(media.RunningCount())
	if currentRunning >= maxConcurrent {
		logger.Info("scheduler", "Local transcode queue full: current=%d max=%d, task=%s waiting", currentRunning, maxConcurrent, t.ID)
		t.Status = model.StatusQueue
		s.store.UpsertTask(t)
		return
	}

	os.MkdirAll(filepath.Dir(t.OutputFile), 0o755)

	t.Status = model.StatusTranscoding
	t.Progress = 0
	s.store.UpsertTask(t)
	s.store.SetTaskStartedAt(t.ID, time.Now())
	s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusTranscoding), 0, "开始本地转码")

	// 增加运行计数
	media.AddRunningCount(1)

	logger.Info("scheduler", "Starting local transcode for task %s (running=%d/%d)", t.ID, int(media.RunningCount()), maxConcurrent)

	err := media.StartLocalTranscode(
		t.ID,
		t.SourceFile,
		t.OutputFile,
		t.FFmpegArgs,
		func(progress float64) {
			s.store.UpdateTaskStatus(t.ID, model.StatusTranscoding, progress, "")
			s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusTranscoding), progress, "")
		},
		func(err error) {
			if err != nil {
				logger.Error("scheduler", "Local transcode failed for task %s: %v", t.ID, err)
				s.failTask(t, fmt.Sprintf("本地转码失败: %v", err), model.NonRetryable)
			} else {
				now := time.Now()
				s.store.SetTaskFinishedAt(t.ID, now)
				logger.InfoT("scheduler", t.TraceID, "任务完成(本地): id=%s", t.ID)
				s.store.UpdateTaskStatus(t.ID, model.StatusCompleted, 100, "")
				s.hub.BroadcastTaskUpdate(t.ID, string(model.StatusCompleted), 100, "转码完成")
				s.maybeDeleteSourceFile(t)
				time.AfterFunc(5*time.Second, func() {
					s.store.MoveToHistory(t.ID)
				})
			}
		},
	)

	if err != nil {
		logger.Error("scheduler", "Failed to start local transcode for task %s: %v", t.ID, err)
		s.failTask(t, fmt.Sprintf("启动本地转码失败: %v", err), model.NonRetryable)
	}
}

// ===== P2-1 B轮：internal/node / internal/media 委托方法（自根包 shim 迁入）=====

// pickNode 选机委托（B-08；实现 internal/node.Selector）。
func (s *Scheduler) pickNode(t model.Task, now time.Time) (model.Server, error) {
	return s.sel.PickNode(t, now)
}

// NoteNodeAttempt 记录节点选机尝试（B-08 失败率分母）。
func (s *Scheduler) NoteNodeAttempt(serverID string, now time.Time) {
	s.sel.NoteNodeAttempt(serverID, now)
}

// NoteNodeFailure 记录节点失败并计入健康分/熔断（B-08）。
func (s *Scheduler) NoteNodeFailure(serverID, code string, now time.Time) {
	s.sel.NoteNodeFailure(serverID, code, now)
}

// NoteNodeSuccess 记录节点成功并解除熔断（B-08）。
func (s *Scheduler) NoteNodeSuccess(serverID string, now time.Time) {
	s.sel.NoteNodeSuccess(serverID, now)
}

// HealthScoreOf 查询节点健康分（B-08；供 WS 断线广播等根包调用点使用）。
func (s *Scheduler) HealthScoreOf(serverID string) int {
	return s.sel.HealthScoreOf(serverID)
}

// NodeFreeSlots 查询节点空闲槽位（B-08）。
func (s *Scheduler) NodeFreeSlots(serverID string, t model.Task) int {
	return s.sel.NodeFreeSlots(serverID, t)
}

// ApplyNodeHello 应用节点 hello 能力上报（B-08）。
func (s *Scheduler) ApplyNodeHello(caps model.NodeCaps, now time.Time) {
	s.sel.ApplyNodeHello(caps, now)
}

// SetProxyProbe 注入代理校验探测通道（转发至 internal/media.ProxyWorkflow；单测/装配入口）。
func (s *Scheduler) SetProxyProbe(p media.ProxyProber) { s.proxyWf.SetProbe(p) }

// CancelUpload 取消进行中的上传（pause/cancel 任务时由 api 层调用）。
func (s *Scheduler) CancelUpload(taskID string) bool {
	if cancel, ok := s.cancelMap.LoadAndDelete(taskID); ok {
		cancel.(context.CancelFunc)()
		return true
	}
	return false
}
