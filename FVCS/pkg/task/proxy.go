package task

// GenProxy 任务链路（设计文档 04 §3.2 / §3.3）
//
// 职责：
//  1. CreateGenProxyTask：校验代理载荷 → 建低优先级任务（SMB 直读模式，无分片上传）；
//  2. executeGenProxy：挂载共享 → 构造代理命令 → 执行 → 代理文件落盘 → 状态回传；
//  3. 代理任务为低优先级后台任务（PriorityLow），不与正式渲染争抢调度位。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Fnos.VC_Service/pkg/ffmpeg"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/smb"
)

// GenProxyRequest 创建 GenProxy 任务的入参（04 §3.2）
type GenProxyRequest struct {
	TaskID        string
	Payload       json.RawMessage
	ClientConnID  string
	SMBPath       string // 素材共享根 UNC
	SMBUser       string
	SMBPassword   string
	CredentialID  string // 本地凭据档案键（07 §5.3 M1）；非空时忽略 SMBUser/SMBPassword
	SMBOutputPath string // 代理输出目录 UNC（可空，空则回落到共享根）
	TraceID       string // P2-1 任务链路追踪 ID（FVCC 下发时透传）
}

// GenProxyCreated 创建结果（供 server 层回包）
type GenProxyCreated struct {
	Task      *Task
	ProxyFile string // 相对输出目录的代理路径（与请求 payload 一致）
	Template  string // 生效模板键（服务端权威）
}

// CreateGenProxyTask 创建代理生成任务。
// 校验失败返回以 E_* 错误码开头的 error，调用方可直接作为 code 回包。
func CreateGenProxyTask(req GenProxyRequest) (*GenProxyCreated, error) {
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, errCode(protocol.ErrCodePayloadInvalid, "TaskId 为空")
	}

	payload, perr := protocol.DecodeGenProxyPayloadStrict(req.Payload)
	if perr != nil {
		return nil, perr
	}
	if perr := protocol.ValidateGenProxyPayload(payload); perr != nil {
		return nil, perr
	}
	if !isSafeRelPath(payload.SrcFile) || !isSafeRelPath(payload.ProxyFile) {
		return nil, errCode(protocol.ErrCodeAssetNotInRoot, "素材/代理路径越出共享根")
	}
	if strings.TrimSpace(req.SMBPath) == "" {
		return nil, errCode(protocol.ErrCodeSMBMountFailed, "SMBPath 为空")
	}

	payloadRaw := strings.TrimSpace(string(req.Payload))
	if len(payloadRaw) == 0 {
		buf, _ := json.Marshal(payload)
		payloadRaw = string(buf)
	}

	srcName := strings.TrimSpace(payload.SrcFile)
	if srcName == "" {
		srcName = payload.AssetID
	}

	task := &Task{
		TaskID:         req.TaskID,
		Status:         StatusWaiting,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		SourceFileName: srcName,
		OutputName:     strings.TrimSpace(payload.ProxyFile),
		Progress:       0,
		ClientConnID:   req.ClientConnID,
		Priority:       PriorityLow,
		IsSMBMode:      true,
		SMBPath:        req.SMBPath,
		SMBUser:        req.SMBUser,
		SMBPassword:    req.SMBPassword,
		CredentialID:   strings.TrimSpace(req.CredentialID),
		SMBOutputPath:  req.SMBOutputPath,
		UploadComplete: true,
		TaskType:       TaskTypeGenProxy,
		PayloadJSON:    payloadRaw,
		ProfileKey:     payload.Template.PresetKey,
		TraceID:        req.TraceID,
	}
	applyCredentialPrecedence(task)

	manager.mutex.Lock()
	manager.tasks[task.TaskID] = task
	manager.waitingQueue = append(manager.waitingQueue, task)
	saveTaskToDB(task)
	manager.mutex.Unlock()

	logger.InfoT("task", task.TraceID, "CreateGenProxy: taskID=%s, asset=%s, src=%s, proxy=%s, preset=%s",
		task.TaskID, payload.AssetID, payload.SrcFile, payload.ProxyFile, payload.Template.PresetKey)

	return &GenProxyCreated{
		Task:      task,
		ProxyFile: payload.ProxyFile,
		Template:  payload.Template.PresetKey,
	}, nil
}

// executeGenProxy 代理任务主流程（04 §3.3）
func executeGenProxy(task *Task) {
	taskID := task.TaskID

	payload := &protocol.GenProxyPayload{}
	if err := json.Unmarshal([]byte(task.PayloadJSON), payload); err != nil {
		logger.Error("task", "GenProxy: 载荷解析失败 taskID=%v: %v", taskID, err)
		failGenProxy(task, protocol.ErrCodeEDLInvalid, "代理载荷解析失败: "+err.Error())
		return
	}
	if perr := protocol.ValidateGenProxyPayload(payload); perr != nil {
		logger.Error("task", "GenProxy: 载荷校验失败 taskID=%v: %v", taskID, perr.Error())
		failGenProxy(task, protocol.ErrCodePayloadInvalid, "代理载荷校验失败: "+perr.Error())
		return
	}

	setRenderStage(taskID, ffmpeg.StageRunInfo{Stage: "prepare", OverallPct: task.Progress})

	// ---- 1. 挂载素材共享 ----
	mountPath, err := mountTaskShare(task)
	if err != nil {
		logger.Error("task", "GenProxy: 挂载共享失败 taskID=%v: %v", taskID, err)
		// 失败节点细化上报：缺凭据（E_CREDENTIAL_MISSING）与共享不可达（E_SMB_MOUNT_FAILED）区分，
		// 便于 FVCC 侧直接给出"补齐凭据"或"检查共享"的可操作提示。
		failGenProxy(task, mountFailureCode(err), "挂载素材共享失败（共享 "+task.SMBPath+"）: "+err.Error())
		return
	}

	srcUNC := smb.BuildSMBPath(mountPath, filepath.FromSlash(strings.TrimSpace(payload.SrcFile)))
	if _, err := os.Stat(srcUNC); err != nil {
		logger.Error("task", "GenProxy: 素材不存在 taskID=%v, path=%v", taskID, srcUNC)
		failGenProxy(task, protocol.ErrCodeAssetMissing, "素材文件不存在: "+srcUNC)
		return
	}

	// ---- 2. 解析代理输出路径（04 §3.2：优先 smbOutputPath 下的 _proxy 目录）----
	outBase := mountPath
	if strings.TrimSpace(task.SMBOutputPath) != "" {
		outBase = strings.TrimSpace(task.SMBOutputPath)
	}
	proxyUNC := smb.BuildSMBPath(outBase, filepath.FromSlash(strings.TrimSpace(payload.ProxyFile)))
	if err := ensureDir(absDirOf(proxyUNC)); err != nil {
		logger.Error("task", "GenProxy: 创建代理目录失败 taskID=%v: %v", taskID, err)
		failGenProxy(task, protocol.ErrCodeRenderFailed, "创建代理输出目录失败（目录 "+absDirOf(proxyUNC)+"）: "+err.Error())
		return
	}

	// ---- 3. 构造代理命令（软编默认，硬编可用时空闲升级）----
	workDir := filepath.Join(taskWorkingDir(taskID), "proxy")
	caps := ffmpeg.DetectHardwareCaps()

	args, err := ffmpeg.BuildProxyCommand(srcUNC, proxyUNC, payload.Template, caps)
	if err != nil {
		logger.Error("task", "GenProxy: 构造代理命令失败 taskID=%v: %v", taskID, err)
		failGenProxy(task, protocol.ErrCodeRenderFailed, "构造代理命令失败: "+err.Error())
		return
	}

	plan := &ffmpeg.RenderPlan{
		Mode:       "proxy",
		WorkDir:    workDir,
		OutputPath: proxyUNC,
		TempOutput: proxyUNC,
		FinalFrom:  proxyUNC,
		Stages: []ffmpeg.RenderStage{
			{Name: ffmpeg.StagePrepare, WeightPct: 1},
			// 代理直接写目标 UNC，无 concat/落盘阶段（04 §3.3 不做裁剪、完整时长）
			{Name: ffmpeg.StageMux, Args: args, Output: proxyUNC, WeightPct: 98},
			{Name: ffmpeg.StageFinalize, WeightPct: 1},
		},
	}

	logger.Info("task", "GenProxy: 命令就绪 taskID=%s, src=%s, out=%s, preset=%s",
		taskID, srcUNC, proxyUNC, payload.Template.PresetKey)

	// ---- 4. 执行（代理为单条命令、无分段，故不参与断点续跑）----
	runErr := ffmpeg.RunRenderPlan(taskID, plan, func(info ffmpeg.StageRunInfo) {
		setRenderStage(taskID, info)
	})
	if runErr != nil {
		if errors.Is(runErr, ffmpeg.ErrRenderStopped) {
			logger.Info("task", "GenProxy: 任务被停止 taskID=%v", taskID)
			cleanupTaskFiles(task)
			return
		}
		logger.Error("task", "GenProxy: 生成失败 taskID=%v: %v", taskID, runErr)
		failGenProxy(task, protocol.ErrCodeRenderFailed, "代理渲染失败"+stageDetail(taskID)+": "+runErr.Error())
		return
	}

	// ---- 5. 完成 ----
	if _, err := os.Stat(proxyUNC); err != nil {
		logger.Error("task", "GenProxy: 代理文件不存在 taskID=%v, path=%v", taskID, proxyUNC)
		failGenProxy(task, protocol.ErrCodeRenderFailed, "代理产物缺失（渲染已退出但未生成文件）: "+proxyUNC)
		return
	}

	MarkSuccess(taskID, proxyUNC)
	logger.Info("task", "GenProxy: 代理生成完成 taskID=%s, output=%s", taskID, proxyUNC)
	cleanupTaskFiles(task)
}

// failGenProxy 代理失败收口（失败节点细化上报）：
// 记录具体错误码与失败阶段明细并清理任务临时文件。
// 明细文本经 sanitizeSMBMessage 清洗，确保不含账号口令等敏感凭据。
func failGenProxy(task *Task, code, msg string) {
	if task == nil {
		return
	}
	MarkFailedWithReason(task.TaskID, code, sanitizeSMBMessage(task, msg))
	cleanupTaskFiles(task)
}

// mountFailureCode 挂载失败细化：缺凭据场景上报 E_CREDENTIAL_MISSING，其余归 E_SMB_MOUNT_FAILED。
func mountFailureCode(err error) string {
	if err != nil && strings.Contains(err.Error(), protocol.ErrCodeCredentialMissing) {
		return protocol.ErrCodeCredentialMissing
	}
	return protocol.ErrCodeSMBMountFailed
}

// sanitizeSMBMessage 清除失败明细中可能出现的账号口令（net use 报错会回显命令行），
// 保证上报 FVCC 的错误信息不含敏感凭据。
func sanitizeSMBMessage(task *Task, msg string) string {
	if task == nil {
		return msg
	}
	if pw := task.SMBPassword; pw != "" {
		msg = strings.ReplaceAll(msg, pw, "******")
	}
	if user := strings.TrimSpace(task.SMBUser); user != "" {
		msg = strings.ReplaceAll(msg, "/user:"+user, "/user:******")
	}
	return msg
}

// stageDetail 取任务当前渲染阶段并格式化为失败明细后缀（无阶段时为空串）。
// 必须在持有 manager 锁的调用之外使用（failGenProxy 内部会再次加锁）。
func stageDetail(taskID string) string {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if t, ok := manager.tasks[taskID]; ok {
		if s := strings.TrimSpace(t.Stage); s != "" {
			return "（阶段 " + s + "）"
		}
	}
	return ""
}
