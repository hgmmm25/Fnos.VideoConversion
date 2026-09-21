package task

// RenderEDL 任务链路（设计文档 03 §3、04 §3、05 §4）
//
// 职责：
//  1. CreateRenderEDLTask：校验结构化载荷 → 建任务（SMB 直读模式，无分片上传）；
//  2. executeRenderEDL：挂载共享 → 构造渲染计划 → 顺序执行 → 成品落盘 → 状态回传；
//  3. 与既有 TaskManager 复用调度、并发上限、进度节流与取消（StopTranscode）能力。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/ffmpeg"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/smb"
)

// 任务类型（Task.TaskType）
const (
	TaskTypeTranscode = "TRANSCODE"
	TaskTypeRenderEDL = "RENDER_EDL"
	TaskTypeGenProxy  = "GEN_PROXY"
)

// normalizeTaskType 空值/非法值一律归为传统转码
func normalizeTaskType(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case TaskTypeRenderEDL:
		return TaskTypeRenderEDL
	case TaskTypeGenProxy:
		return TaskTypeGenProxy
	default:
		return TaskTypeTranscode
	}
}

// RenderEDLRequest 创建 RenderEDL 任务的入参（03 §3.1 CreateRenderEDL）
type RenderEDLRequest struct {
	TaskID        string
	Payload       json.RawMessage
	ProfileKey    string // 前端请求的方案（服务端权威口径以最终生效值为准）
	ClientConnID  string
	SMBPath       string // 素材共享根 UNC
	SMBUser       string
	SMBPassword   string
	CredentialID  string // 本地凭据档案键（07 §5.3 M1）；非空时忽略 SMBUser/SMBPassword
	SMBOutputPath string // 目标目录 UNC（可空）
	TraceID       string // P2-1 任务链路追踪 ID（FVCC 下发时透传）
}

// RenderEDLCreated 创建结果（供 server 层回包）
type RenderEDLCreated struct {
	Task            *Task
	FastCopyAllowed bool
	Warnings        []string
	ProjectID       string
	ProjectRev      int
	Checksum        string
}

// CreateRenderEDLTask 创建结构化剪辑渲染任务。
// 校验失败返回以 E_* 错误码开头的 error，调用方可直接作为 code 回包。
func CreateRenderEDLTask(req RenderEDLRequest) (*RenderEDLCreated, error) {
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, errCode(protocol.ErrCodePayloadInvalid, "TaskId 为空")
	}

	payload, perr := protocol.DecodeRenderPayloadStrict(req.Payload)
	if perr != nil {
		return nil, perr
	}
	if perr := protocol.ValidateRenderEDLPayload(payload); perr != nil {
		return nil, perr
	}
	// checksum 校验：集成期可用 FVCS_SKIP_RENDER_CHECKSUM=1 临时跳过
	if os.Getenv("FVCS_SKIP_RENDER_CHECKSUM") != "1" {
		if !protocol.VerifyRenderChecksum(payload) {
			return nil, errCode(protocol.ErrCodeEDLInvalid, "checksum 校验失败")
		}
	}
	if !payloadAllLocal(payload) {
		return nil, errCode(protocol.ErrCodeAssetNotInRoot, "素材路径越出 sourceRoot")
	}

	if strings.TrimSpace(req.SMBPath) == "" {
		return nil, errCode(protocol.ErrCodeSMBMountFailed, "SMBPath 为空")
	}

	payloadRaw := strings.TrimSpace(string(req.Payload))
	if len(payloadRaw) == 0 {
		buf, _ := json.Marshal(payload)
		payloadRaw = string(buf)
	}

	// 源文件名用于列表展示：单素材取文件名，多素材取首个
	srcName := ""
	if len(payload.Clips) > 0 {
		srcName = payload.Clips[0].File
	}

	task := &Task{
		TaskID:         req.TaskID,
		Status:         StatusWaiting,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		SourceFileName: srcName,
		OutputName:     payload.Output,
		Progress:       0,
		ClientConnID:   req.ClientConnID,
		Priority:       PriorityNormal,
		IsSMBMode:      true,
		SMBPath:        req.SMBPath,
		SMBUser:        req.SMBUser,
		SMBPassword:    req.SMBPassword,
		CredentialID:   strings.TrimSpace(req.CredentialID),
		SMBOutputPath:  req.SMBOutputPath,
		UploadComplete: true,
		TaskType:       TaskTypeRenderEDL,
		PayloadJSON:    payloadRaw,
		ProfileKey:     payload.Profile.PresetKey,
		TraceID:        req.TraceID,
	}
	applyCredentialPrecedence(task)

	manager.mutex.Lock()
	manager.tasks[task.TaskID] = task
	manager.waitingQueue = append(manager.waitingQueue, task)
	saveTaskToDB(task)
	manager.mutex.Unlock()

	logger.InfoT("task", task.TraceID, "CreateRenderEDL: taskID=%s, project=%s, rev=%d, clips=%d, totalMs=%d, preset=%s",
		task.TaskID, payload.ProjectID, payload.ProjectRev, len(payload.Clips), payload.TotalMs, payload.Profile.PresetKey)

	// 同源直通预判（03 §3.1：服务端计算 fastCopyAllowed 并回填）
	fastCopy := previewFastCopy(payload)
	payload.Profile.FastCopyAllowed = fastCopy

	return &RenderEDLCreated{
		Task:            task,
		FastCopyAllowed: fastCopy,
		Warnings:        nil,
		ProjectID:       payload.ProjectID,
		ProjectRev:      payload.ProjectRev,
		Checksum:        payload.Checksum,
	}, nil
}

// previewFastCopy 纯逻辑同源直通预判（不探测素材，保守判定）
func previewFastCopy(p *protocol.RenderTaskPayload) bool {
	if p == nil || p.Profile.PresetKey != protocol.PresetCopySameSource {
		return false
	}
	files := map[string]struct{}{}
	for _, c := range p.Clips {
		files[strings.ToLower(strings.TrimSpace(c.File))] = struct{}{}
		if c.Speed != 0 && c.Speed != 1 {
			return false
		}
	}
	if len(files) != 1 {
		return false
	}
	// 片段须首尾相接、单调递增
	prevOut := int64(-1)
	for i, c := range p.Clips {
		in, err1 := protocol.ParseTimecode(c.In)
		out, err2 := protocol.ParseTimecode(c.Out)
		if err1 != nil || err2 != nil {
			return false
		}
		if i == 0 {
			if in != 0 {
				return false
			}
		} else if in != prevOut {
			return false
		}
		prevOut = out
	}
	return true
}

// payloadAllLocal 校验所有 clip 的相对路径不越出 sourceRoot
func payloadAllLocal(p *protocol.RenderTaskPayload) bool {
	for _, c := range p.Clips {
		if !isSafeRelPath(c.File) {
			return false
		}
	}
	return true
}

// isSafeRelPath 拒绝绝对路径与 ../ 穿越
func isSafeRelPath(rel string) bool {
	s := strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "/") || filepath.IsAbs(s) || strings.Contains(s, ":") {
		return false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

func errCode(code, msg string) error {
	return fmt.Errorf("%s: %s", code, msg)
}

// ============================================================
// 执行器
// ============================================================

// taskWorkingDir 任务临时目录（与 cleanupTaskFiles / executeTranscode 口径一致）
func taskWorkingDir(taskID string) string {
	cfg := config.Get()
	if filepath.IsAbs(cfg.TempDir) {
		return filepath.Join(cfg.TempDir, taskID)
	}
	return filepath.Join(getAppDir(), cfg.TempDir, taskID)
}

// executeRenderEDL 渲染任务主流程（05 §4.2）
func executeRenderEDL(task *Task) {
	taskID := task.TaskID

	payload := &protocol.RenderTaskPayload{}
	if err := json.Unmarshal([]byte(task.PayloadJSON), payload); err != nil {
		logger.Error("task", "RenderEDL: 载荷解析失败 taskID=%v: %v", taskID, err)
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}
	if perr := protocol.ValidateRenderEDLPayload(payload); perr != nil {
		logger.Error("task", "RenderEDL: 载荷校验失败 taskID=%v: %v", taskID, perr.Error())
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}

	// 断点续跑：仅重启恢复的渲染任务可能带 ResumePending（06 §3.3/§3.4），
	// 实际可跳过段数由 ffmpeg.ResumableSegmentPrefix 按 workDir 产物完整性二次确认。
	resumeSegDone := effectiveResumeSegDone(task)
	if resumeSegDone > 0 {
		logger.Info("task", "RenderEDL: 命中续跑断点 taskID=%v, segDone=%d/%d, workDir 待校验",
			taskID, resumeSegDone, task.SegTotal)
	}

	setRenderStage(taskID, ffmpeg.StageRunInfo{Stage: "prepare", OverallPct: task.Progress})

	// ---- 1. 挂载素材共享 ----
	mountPath, err := mountTaskShare(task)
	if err != nil {
		logger.Error("task", "RenderEDL: 挂载共享失败 taskID=%v: %v", taskID, err)
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}
	srcBase := filepath.Join(mountPath, filepath.FromSlash(strings.TrimSpace(payload.SourceRoot)))
	srcUNC, err := makeSrcResolver(srcBase)
	if err != nil {
		logger.Error("task", "RenderEDL: sourceRoot 非法 taskID=%v: %v", taskID, err)
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}

	// ---- 2. 解析成品路径 ----
	outUNC, err := resolveOutputUNC(task, mountPath, payload)
	if err != nil {
		logger.Error("task", "RenderEDL: 成品路径非法 taskID=%v: %v", taskID, err)
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}
	if err := ensureDir(absDirOf(outUNC)); err != nil {
		logger.Error("task", "RenderEDL: 创建目标目录失败 taskID=%v: %v", taskID, err)
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}

	// ---- 3. 构造渲染计划（含素材探测与能力探测）----
	workDir := filepath.Join(taskWorkingDir(taskID), "render")
	caps := ffmpeg.DetectHardwareCaps()
	probes := map[string]*ffmpeg.MediaInfo{}

	plan, err := ffmpeg.BuildRenderEDLCommands(payload, srcUNC, outUNC, workDir, caps, probes)
	if err != nil {
		logger.Error("task", "RenderEDL: 构造渲染计划失败 taskID=%v: %v", taskID, err)
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}

	setRenderProfile(taskID, plan.EffectivePresetKey, plan.Fallback, plan.Warnings)
	if plan.Fallback {
		logger.Warn("task", "RenderEDL: 硬编不可用，已降级 preset=%s taskID=%s", plan.EffectivePresetKey, taskID)
	}

	logger.Info("task", "RenderEDL: 计划就绪 taskID=%s, mode=%s, segments=%d, stages=%d, out=%s",
		taskID, plan.Mode, len(plan.Segments), len(plan.Stages), outUNC)

	// 记录段总数（前端展示与续跑判定），并清除本次已消费的续跑标记
	markRenderPlan(taskID, len(plan.Segments))

	// ---- 4. 顺序执行（续跑时跳过 workDir 内已完整的片段）----
	runErr := ffmpeg.RunRenderPlanResume(taskID, plan, resumeSegDone, func(info ffmpeg.StageRunInfo) {
		setRenderStage(taskID, info)
	})
	if runErr != nil {
		if errors.Is(runErr, ffmpeg.ErrRenderStopped) {
			// 取消/停止：状态已由 CancelTask 落地，这里只做清理
			logger.Info("task", "RenderEDL: 任务被停止 taskID=%v", taskID)
			cleanupTaskFiles(task)
			return
		}
		logger.Error("task", "RenderEDL: 渲染失败 taskID=%v: %v", taskID, runErr)
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}

	// ---- 5. 完成 ----
	if _, err := os.Stat(plan.OutputPath); err != nil {
		logger.Error("task", "RenderEDL: 成品不存在 taskID=%v, path=%v", taskID, plan.OutputPath)
		MarkFailed(taskID)
		cleanupTaskFiles(task)
		return
	}

	MarkSuccess(taskID, plan.OutputPath)
	logger.Info("task", "RenderEDL: 渲染完成 taskID=%s, output=%s", taskID, plan.OutputPath)
	cleanupTaskFiles(task)
}

// makeSrcResolver 构造“素材相对路径 → UNC”解析器，内置目录穿越防护
func makeSrcResolver(srcBase string) (func(string) (string, error), error) {
	base := filepath.Clean(srcBase)
	if !strings.HasSuffix(base, string(os.PathSeparator)) {
		baseClean := base + string(os.PathSeparator)
		return func(rel string) (string, error) {
			if !isSafeRelPath(rel) {
				return "", errCode(protocol.ErrCodeAssetNotInRoot, "素材路径非法: "+rel)
			}
			full := filepath.Join(base, filepath.FromSlash(strings.TrimSpace(rel)))
			if !strings.HasPrefix(strings.ToLower(full), strings.ToLower(baseClean)) &&
				!strings.EqualFold(full, base) {
				return "", errCode(protocol.ErrCodeAssetNotInRoot, "素材越出 sourceRoot: "+rel)
			}
			if _, err := os.Stat(full); err != nil {
				return "", errCode(protocol.ErrCodeAssetMissing, "素材不存在: "+rel)
			}
			return full, nil
		}, nil
	}
	return nil, errCode(protocol.ErrCodePayloadInvalid, "sourceRoot 为空")
}

// resolveOutputUNC 计算成品 UNC（03 §3.1：优先 SMBOutputPath）
func resolveOutputUNC(task *Task, mountPath string, payload *protocol.RenderTaskPayload) (string, error) {
	outRel := strings.TrimSpace(strings.ReplaceAll(payload.Output, "\\", "/"))
	outRel = strings.TrimPrefix(outRel, "./")
	if !isSafeRelPath(outRel) {
		return "", errCode(protocol.ErrCodePayloadInvalid, "output 非法: "+payload.Output)
	}
	if strings.TrimSpace(task.SMBOutputPath) != "" {
		return smb.BuildSMBPath(task.SMBOutputPath, filepath.FromSlash(outRel)), nil
	}
	destRoot := strings.TrimSpace(strings.ReplaceAll(payload.DestRoot, "\\", "/"))
	destRoot = strings.TrimPrefix(destRoot, "./")
	if destRoot != "" {
		if !isSafeRelPath(destRoot) {
			return "", errCode(protocol.ErrCodePayloadInvalid, "destRoot 非法: "+payload.DestRoot)
		}
		return smb.BuildSMBPath(filepath.Join(mountPath, filepath.FromSlash(destRoot)), filepath.FromSlash(outRel)), nil
	}
	return smb.BuildSMBPath(mountPath, filepath.FromSlash(outRel)), nil
}

func absDirOf(p string) string {
	return filepath.Dir(filepath.Clean(p))
}

func ensureDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// markRenderPlan 记录渲染计划的段总数并消费续跑标记（06 §3.4）。
//
// SegTotal 落库供前端展示「第 x/y 段」；ResumePending 只对「重启恢复的那一次执行」
// 有效，计划一旦构造成功即视为已消费，避免同进程内的重试误判为续跑。
func markRenderPlan(taskID string, segTotal int) {
	manager.mutex.Lock()
	t, ok := manager.tasks[taskID]
	if ok {
		if segTotal > 0 {
			t.SegTotal = segTotal
		}
		if t.ResumePending {
			t.ResumePending = false
		}
		saveTaskToDB(t)
	}
	manager.mutex.Unlock()
}

// setRenderStage 合并执行器上报的阶段信息并推送前端（进度节流沿用 TaskManager）。
//
// Stage/SegDone 仅在推进时落库（断点续跑的锚点，06 §3.4）：若进程在片段执行中崩溃，
// 已落库的 SegDone 只会是「已确认完整」的段数，故续跑时不会复用半成品片段。
func setRenderStage(taskID string, info ffmpeg.StageRunInfo) {
	manager.mutex.Lock()
	t, ok := manager.tasks[taskID]
	if !ok {
		manager.mutex.Unlock()
		return
	}
	prevStage, prevSegDone := t.Stage, t.SegDone
	mergeRenderRunInfo(t, info)
	if t.Stage != prevStage || t.SegDone != prevSegDone {
		saveTaskToDB(t)
	}
	force := info.Stage == "prepare" || info.Stage == "finalize"
	fireTaskUpdateLocked(taskID, force)
	manager.mutex.Unlock()
}

// setRenderProfile 回填生效方案与降级信息
func setRenderProfile(taskID, presetKey string, fallback bool, warnings []string) {
	manager.mutex.Lock()
	t, ok := manager.tasks[taskID]
	if ok {
		if presetKey != "" {
			t.ProfileKey = presetKey
		}
		t.Degraded = fallback
		if len(warnings) > 0 {
			t.Warnings = strings.Join(warnings, "; ")
		}
		saveTaskToDB(t)
	}
	manager.mutex.Unlock()
}
