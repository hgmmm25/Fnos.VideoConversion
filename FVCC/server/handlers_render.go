package main

// B-04：渲染提交（POST /app/fvcc/api/edl/projects/:id/render）。
//
// 契约依据：
//   - 03 §4.4：请求 {presetKey, outputName, serverId?}，响应 {taskId,status,serverId,output,totalMs,fastCopyAllowed}；
//     outputName 不含扩展名，服务端补 .mp4 并做重名追加（_1/_2…）。
//   - 06 §4.4：幂等键 = clips+profile 规范化 sha256 前 16 位（projectRev / output 不参与）；
//     命中进行中（QUEUE/RUNNING/COOLDOWN）或成品仍在的成功任务 → 复用同一 taskId；force:true 跳过幂等。
//   - 03 §5.1：本文件承担第三道闸门「下发前二次校验 + 素材存在性预检」。
//   - 07 §3.5：输出名规范化（单段、禁保留名、追加 _1.._99 且不得覆盖既有文件）。
//
// 职责边界：
//   - 只做「提交侧」：预设闸门、项目内容闸门、根路径解析、素材存在性预检、输出名规范化、
//     totalMs / fastCopyAllowed 计算、幂等复用、任务入队（QUEUE）。
//   - 选机（B-08）、下发（B-06）、WS 聚合（B-07）不在本文件：serverId 缺省时任务保持
//     ServerID=""，由 B-05 调度器按 06 §5.3 选机后回填。
//   - 扩展名白名单、落地 EvalSymlinks、盘余量（507 E_DISK_FULL）留给 D-02 / E 组（见 10 号台账决策表）。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"fvcc/internal/security"
	"fvcc/logger"
)

// ===== 输出名与载荷约束（03 §4.4 / 07 §3.5 / 05 §5.1）=====
const (
	renderOutputBaseMax  = 80
	renderOutputMaxTries = 99 // 07 §3.5：重名追加 _1.._99，禁止覆盖
	renderVideoPixFmt    = "yuv420p"
	renderAudioKbps      = "192k"
	renderAudioChannels  = 2
)

// reOutputBase 输出名基本名白名单（07 §3.5：单段、无路径分隔符）。
// 首字符与后续字符均允许中英文/数字/_，中段另允许 - 与 .；与 07 §3.5 的 \p{Han} 首字符口径一致。
var reOutputBase = regexp.MustCompile(`^[A-Za-z0-9_\x{4e00}-\x{9fa5}][A-Za-z0-9_.\-\x{4e00}-\x{9fa5}]{0,79}$`)

// reservedOutputBase Windows 保留设备名（07 §3.5），不区分大小写、忽略扩展名比较。
var reservedOutputBase = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// renderSubmitInput POST /edl/projects/:id/render 请求体（03 §4.4 + 06 §4.4 force）。
type renderSubmitInput struct {
	PresetKey  string `json:"presetKey"`
	OutputName string `json:"outputName"`
	ServerID   string `json:"serverId"`
	Force      bool   `json:"force"` // true 跳过幂等检查，强制重渲染（06 §4.4）
}

// ===== Handler =====

// renderEDLProject POST /edl/projects/:id/render（B-04）。
func (h *Handlers) renderEDLProject(c *gin.Context) {
	projectID := c.Param("id")
	proj, found := h.store.GetProject(projectID)
	if !found {
		edlErr(c, http.StatusNotFound, errCodeProjectMissing, "项目不存在", gin.H{"id": projectID})
		return
	}

	in, ok := h.bindRenderSubmit(c)
	if !ok {
		return
	}

	// 闸门 1：presetKey 必须命中服务端枚举表（03 §5.2 / 05 §5.1）
	spec, ok := edlRenderPresetTable[in.PresetKey]
	if !ok {
		edlErr(c, http.StatusBadRequest, errCodeProfileInvalid, "presetKey 不在服务端枚举表内",
			gin.H{"field": "presetKey", "presetKey": in.PresetKey})
		return
	}
	if spec.ProxyOnly {
		edlErr(c, http.StatusBadRequest, errCodeProfileInvalid, "proxy_* 方案用于生成代理，不可作为成片导出方案",
			gin.H{"field": "presetKey", "presetKey": in.PresetKey})
		return
	}

	// 闸门 2：项目内容契约（复用 B-03 校验；同时回填 timeline 缺省值）
	tl := proj.Timeline
	if v := validateProjectInput(proj.Name, &tl, proj.Clips); v != nil {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, v.Msg, v.Detail)
		return
	}
	if len(proj.Clips) == 0 {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, "时间线为空，无法渲染",
			gin.H{"field": "clips"})
		return
	}
	proj.Timeline = tl

	// 闸门 3：采样率必须落在 FVCS 白名单内（03 §5.1 第三道闸门）
	if !fvcsSampleRates[tl.SampleRate] {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid,
			"timeline.sampleRate 必须为 32000/44100/48000/96000（FVCS 白名单）",
			gin.H{"field": "timeline.sampleRate", "sampleRate": tl.SampleRate})
		return
	}

	// 闸门 4：解析素材根 / 成品根（03 §2.3、09 §4.2）
	sourceRoot, destRoot, localSource, localDest, rootMapped := h.resolveEDLRoots()
	if sourceRoot == "" || destRoot == "" {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid,
			"未配置素材根/成品根，请在设置中配置 videoRoot/exportRoot",
			gin.H{"field": "sourceRoot", "sourceRoot": sourceRoot, "destRoot": destRoot})
		return
	}

	// 闸门 5：输出名规范化（03 §4.4 / 07 §3.5）
	base, nerr := normalizeOutputBase(in.OutputName)
	if nerr != nil {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, nerr.Error(), gin.H{"field": "outputName"})
		return
	}

	// 闸门 6：素材存在性预检（03 §5.2；本地看不见素材根时不拦截，留给 FVCS 侧 stat → E_ASSET_MISSING）
	if code, status, v := h.precheckAssets(localSource, proj.Clips); v != nil {
		edlErr(c, status, code, v.Msg, v.Detail)
		return
	}

	// 幂等键：先按「无重名后缀」的输出名构造探针载荷（06 §4.4 幂等键不含 output，故与最终载荷同键）
	probe := buildRenderPayload(proj, sourceRoot, destRoot, base+renderOutputExt, in.PresetKey, spec)
	probe.Profile.FastCopyAllowed = computeFastCopyAllowed(&probe)
	probe.Checksum = computeRenderChecksum(&probe)

	if !in.Force {
		if t, hit := h.store.FindActiveTaskByChecksum(probe.Checksum); hit {
			h.store.AppendAudit(AuditEntry{
				Actor: security.GetEDLActor(c), Action: "render.reuse_active", Target: t.ID, Result: "ok",
				Detail: "checksum=" + probe.Checksum,
			})
			c.JSON(http.StatusOK, renderSubmitBody(t, true))
			return
		}
		if t, hit := h.store.FindSuccessTaskByChecksum(probe.Checksum); hit && h.outputArtifactExists(t, localDest) {
			h.store.AppendAudit(AuditEntry{
				Actor: security.GetEDLActor(c), Action: "render.reuse_success", Target: t.ID, Result: "ok",
				Detail: "checksum=" + probe.Checksum,
			})
			c.JSON(http.StatusOK, renderSubmitBody(t, true))
			return
		}
	}

	// 输出重名追加：避开成品目录中已存在的物理文件与已被占用的任务输出名（07 §3.5）
	output, oerr := h.uniqueOutputName(base, localDest)
	if oerr != nil {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, oerr.Error(), gin.H{"field": "outputName"})
		return
	}

	// 闸门 7：最终输出名严格白名单（07 §3.5，D-02 唯一实现）+ 成品落点 L4
	if e := validateOutputNameStrict(output); e != nil {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, "outputName 非法: "+e.Msg,
			gin.H{"field": "outputName"})
		return
	}
	if h.pv != nil && localDest != "" && h.pv.HasAuthorizedRootFor(localDest) {
		if _, e := h.pv.ValidateRel(localDest, output, nil); e != nil {
			edlErrFromValidation(c, "destRoot", e)
			return
		}
	}

	// 指定节点校验（06 §5.3 手工优先；缺省留空由 B-05 选机）
	var serverID, serverName string
	if in.ServerID != "" {
		srv, has := h.store.GetServer(in.ServerID)
		if !has {
			edlErr(c, http.StatusConflict, errCodeNodeOffline, "指定渲染节点不存在",
				gin.H{"serverId": in.ServerID})
			return
		}
		if !strings.EqualFold(strings.TrimSpace(srv.Status), "online") {
			edlErr(c, http.StatusConflict, errCodeNodeOffline, "指定渲染节点离线",
				gin.H{"serverId": in.ServerID})
			return
		}
		serverID, serverName = srv.ID, srv.Name
	}

	// 落地最终载荷（含去重后的 output）
	payload := buildRenderPayload(proj, sourceRoot, destRoot, output, in.PresetKey, spec)
	payload.Profile.FastCopyAllowed = computeFastCopyAllowed(&payload)
	payload.Checksum = computeRenderChecksum(&payload)
	payloadJSON, merr := json.Marshal(payload)
	if merr != nil {
		edlErr(c, http.StatusInternalServerError, errCodeEDLInvalid, "载荷序列化失败: "+merr.Error(), nil)
		return
	}

	// 闸门 8：落地载荷自检（D-02 最后一关）——确保入队载荷必然通过 FVCS 侧白名单
	if e := validatePayloadLimits(payloadJSON); e != nil {
		logger.Error("edl", "载荷自检失败(体积/深度) project=%v task-probe output=%v err=%v", proj.ID, output, e)
		edlErr(c, http.StatusInternalServerError, e.Code, "内部载荷校验失败: "+e.Msg, nil)
		return
	}
	if decoded, e := decodeRenderPayloadStrict(json.RawMessage(payloadJSON)); e != nil {
		logger.Error("edl", "载荷自检失败(严格解码) project=%v output=%v err=%v", proj.ID, output, e)
		edlErr(c, http.StatusInternalServerError, e.Code, "内部载荷校验失败: "+e.Msg, nil)
		return
	} else if e := validateRenderEDLPayload(decoded); e != nil {
		// 根未映射为共享相对根（未配置 smbSharePath / 根不在共享内，本地绝对写法过渡态）：
		// 根写法不合规属既有跨端契约空隙（10 §2E 决策：映射层随 D-02 落地），
		// 此时不阻断提交（避免把可用提交变成 500），仅告警并交 FVCS 侧兜底；
		// 其余字段（clips/timeline/profile/output）不合规仍一律拦截。
		if !rootMapped && isRootPathStyleErr(e) {
			logger.Warn("edl", "载荷自检：根未映射为共享相对根，跳过根写法校验 project=%v sourceRoot=%v destRoot=%v err=%v",
				proj.ID, sourceRoot, destRoot, e)
		} else {
			logger.Error("edl", "载荷自检失败(白名单) project=%v output=%v err=%v", proj.ID, output, e)
			edlErr(c, http.StatusInternalServerError, e.Code, "内部载荷校验失败: "+e.Msg, nil)
			return
		}
	}

	now := time.Now()
	task := Task{
		ID:              newRenderTaskID(now),
		OrderID:         h.store.NextOrderID(),
		FileName:        proj.Name,
		OutputName:      output,
		OutputFile:      destOutputPath(destRoot, localDest, output),
		ProfileID:       in.PresetKey,
		ProfileName:     in.PresetKey,
		Status:          StatusQueue,
		Progress:        0,
		RetryType:       Retryable,
		TaskType:        TaskTypeRenderEDL,
		ProjectID:       proj.ID,
		ProjectRev:      proj.Rev, // 冻结 rev（03 §4.3）
		PayloadJSON:     string(payloadJSON),
		Checksum:        payload.Checksum,
		TotalMs:         payload.TotalMs,
		SegTotal:        len(payload.Clips),
		FastCopyAllowed: payload.Profile.FastCopyAllowed,
		ServerID:        serverID,
		ServerName:      serverName,
		TraceID:         newTraceID(now),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	h.store.UpsertTask(task)
	h.store.SetProjectLastRenderTask(proj.ID, task.ID)
	h.store.AppendAudit(AuditEntry{
		Actor: security.GetEDLActor(c), Action: "render.submit", Target: task.ID, Result: "ok",
		Detail: fmt.Sprintf("project=%s rev=%d output=%s totalMs=%d fastCopy=%t checksum=%s",
			proj.ID, proj.Rev, output, payload.TotalMs, payload.Profile.FastCopyAllowed, payload.Checksum),
	})
	if h.hub != nil {
		h.hub.BroadcastTaskUpdate(task.ID, task.Status.WireName(), 0, "任务已入队")
	}
	logger.Info("edl", "渲染提交 project=%v task=%v output=%v totalMs=%v fastCopy=%v checksum=%v",
		proj.ID, task.ID, output, payload.TotalMs, payload.Profile.FastCopyAllowed, payload.Checksum)

	c.JSON(http.StatusOK, renderSubmitBody(task, false))
}

// ===== 内部：请求解析与输出名 =====

// bindRenderSubmit 限制体积并解析请求体；失败时已写入响应，返回 ok=false。
func (h *Handlers) bindRenderSubmit(c *gin.Context) (renderSubmitInput, bool) {
	var in renderSubmitInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, edlMaxBodyBytes)
	if err := c.ShouldBindJSON(&in); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			edlErr(c, http.StatusRequestEntityTooLarge, errCodeEDLTooLarge,
				"渲染请求体超过 256 KB 上限", gin.H{"maxBytes": edlMaxBodyBytes})
			return in, false
		}
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, "请求体解析失败: "+err.Error(), nil)
		return in, false
	}
	in.PresetKey = strings.TrimSpace(in.PresetKey)
	in.OutputName = strings.TrimSpace(in.OutputName)
	in.ServerID = strings.TrimSpace(in.ServerID)
	if in.PresetKey == "" {
		edlErr(c, http.StatusBadRequest, errCodeProfileInvalid, "presetKey 不能为空", gin.H{"field": "presetKey"})
		return in, false
	}
	if in.OutputName == "" {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, "outputName 不能为空", gin.H{"field": "outputName"})
		return in, false
	}
	return in, true
}

// normalizeOutputBase 规范化输出名基本名（03 §4.4：不含扩展名，服务端补 .mp4）。
func normalizeOutputBase(raw string) (string, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		return "", errors.New("outputName 不能为空")
	}
	// 前端/用户可能误带扩展名：仅接受 .mp4，其余显式报错（不静默改写）
	if strings.Contains(base, ".") {
		ext := strings.ToLower(base[strings.LastIndexByte(base, '.'):])
		if ext == renderOutputExt {
			base = base[:len(base)-len(renderOutputExt)]
		} else if ext == ".mov" || ext == ".mkv" || ext == ".m4v" || ext == ".avi" || ext == ".mxf" {
			return "", errors.New("outputName 不含扩展名（服务端固定补 .mp4），当前为 " + ext)
		}
	}
	if base == "" {
		return "", errors.New("outputName 去掉扩展名后为空")
	}
	if strings.ContainsAny(base, `/\:*?"<>|`) {
		return "", errors.New(`outputName 含非法字符（\ / : * ? " < > |）`)
	}
	if utf8.RuneCountInString(base) > renderOutputBaseMax {
		return "", fmt.Errorf("outputName 过长（最多 %d 个字符）", renderOutputBaseMax)
	}
	if !reOutputBase.MatchString(base) {
		return "", errors.New("outputName 含不受支持的字符（仅允许中英文、数字、_ - .）")
	}
	if isReservedOutputBase(base) {
		return "", errors.New("outputName 命中系统保留名，请更换")
	}
	return base, nil
}

// isReservedOutputBase 判定 Windows 保留设备名（忽略大小写与扩展名）。
func isReservedOutputBase(base string) bool {
	stem := base
	if dot := strings.IndexByte(stem, '.'); dot > 0 {
		stem = stem[:dot]
	}
	return reservedOutputBase[strings.ToLower(stem)]
}

// uniqueOutputName 追加 _1/_2… 直到不与物理文件或已占用任务输出名冲突（07 §3.5）。
func (h *Handlers) uniqueOutputName(base, localDest string) (string, error) {
	claimed := make(map[string]bool)
	for _, t := range h.store.GetTasks() {
		if t.OutputName != "" {
			claimed[strings.ToLower(t.OutputName)] = true
		}
	}
	for _, t := range h.store.GetHistory() {
		if t.OutputName != "" {
			claimed[strings.ToLower(t.OutputName)] = true
		}
	}
	for i := 0; i <= renderOutputMaxTries; i++ {
		name := base + renderOutputExt
		if i > 0 {
			name = fmt.Sprintf("%s_%d%s", base, i, renderOutputExt)
		}
		if !claimed[strings.ToLower(name)] && !fileExistsIn(localDest, name) {
			return name, nil
		}
	}
	return "", fmt.Errorf("同名成品过多（已尝试 _1.._%d），请更换输出名", renderOutputMaxTries)
}

// ===== 内部：根路径与素材预检 =====

// resolveEDLRoots 解析素材根/成品根：优先 Settings.videoRoot/exportRoot，
// 缺省回落授权目录首个（sourceRoot）与 SourceRoot/_exports（destRoot，09 §4.2）。
// 同时返回本地可见路径（用于存在性预检），NAS 上本地不可见时返回空串。
func (h *Handlers) resolveEDLRoots() (sourceRoot, destRoot, localSource, localDest string, rootMapped bool) {
	var cfg Settings
	if h.store != nil {
		cfg = h.store.GetSettings()
	}
	localSourceRoot := strings.TrimSpace(cfg.VideoRoot)
	localDestRoot := strings.TrimSpace(cfg.ExportRoot)

	if localSourceRoot == "" && h.pv != nil {
		if paths := h.pv.AccessPaths(); len(paths) > 0 {
			localSourceRoot = paths[0]
		}
	}
	if localDestRoot == "" && localSourceRoot != "" {
		localDestRoot = filepath.Join(localSourceRoot, "_exports")
	}
	if localSourceRoot != "" {
		localSource = filepath.FromSlash(localSourceRoot)
		if !isDir(localSource) {
			localSource = ""
		}
	}
	if localDestRoot != "" {
		localDest = filepath.FromSlash(localDestRoot)
		if !isDir(localDest) {
			localDest = ""
		}
	}

	// 协议根写法：优先映射为「相对共享根的 POSIX 相对根」（03 §2.3：协议内不出现磁盘绝对路径）；
	// 未配置共享根或根不在共享内时保持本地 POSIX 写法，由 rootMapped=false 通知调用方降级处置。
	sourceRoot = toPOSIXRoot(localSourceRoot)
	destRoot = toPOSIXRoot(localDestRoot)
	if shareBase := strings.TrimSpace(cfg.SMBSharePath); shareBase != "" {
		if relS, okS := toShareRelRoot(localSourceRoot, shareBase); okS {
			if relD, okD := toShareRelRoot(localDestRoot, shareBase); okD {
				sourceRoot, destRoot, rootMapped = relS, relD, true
			}
		}
	}
	return sourceRoot, destRoot, localSource, localDest, rootMapped
}

// toPOSIXRoot 把授权目录（本地绝对路径）转成契约要求的 POSIX 相对根写法。
func toPOSIXRoot(p string) string {
	return strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
}

// toShareRelRoot 把本地素材根/成品根映射为「相对共享根的 POSIX 相对根」（03 §2.3、09 §2）：
// 协议内禁止出现磁盘绝对路径，下发时再由共享根 UNC 前缀拼接（remote.go::toSMBUNC 第 3 步）。
// shareBase 为共享根本地路径或 UNC（settings.smbSharePath），localRoot 必须严格位于其下；
// 违反时返回 ok=false（保持本地写法并告警，绝不猜测映射）。
func toShareRelRoot(localRoot, shareBase string) (string, bool) {
	root := normalizeRootForCompare(localRoot)
	base := normalizeRootForCompare(shareBase)
	if root == "" || base == "" || root == base {
		return "", false
	}
	lowerRoot, lowerBase := strings.ToLower(root), strings.ToLower(base)
	if !strings.HasPrefix(lowerRoot, lowerBase+"/") {
		return "", false
	}
	rel := root[len(base)+1:]
	if rel == "" || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// isRootPathStyleErr 判断校验错误是否仅源于根路径写法（sourceRoot/destRoot）。
func isRootPathStyleErr(e *edlValidationError) bool {
	return e != nil && (e.Field == "sourceRoot" || e.Field == "destRoot")
}

// precheckAssets 逐 clip 预检素材（03 §5.2 末行 + 07 §3.2 L1~L4）。
//
// 素材根本地可见且已纳入授权目录时，走 D-02 的 PathValidator.ValidateRel：
// 相对路径 L1~L3 + 落地 EvalSymlinks 前缀校验（拦截软链接越权），失败按
// E_EDL_INVALID→400 / E_ASSET_NOT_IN_ROOT→403 / E_ASSET_MISSING→404 契约化返回；
// 素材根本地不可见（SMB 专用卷）或未纳入授权目录时回退轻量前缀判定，
// 最终由 FVCS 侧 stat 兜底（03 §5.2）。
// 返回（错误码, HTTP 状态, 违规）；无违规时 v == nil。
func (h *Handlers) precheckAssets(localSource string, clips []EDLClip) (string, int, *edlViolation) {
	if localSource == "" {
		return "", 0, nil
	}
	strict := h.pv != nil && h.pv.HasAuthorizedRootFor(localSource)
	for i := range clips {
		rel := clips[i].File
		if rel == "" {
			continue
		}
		if strict {
			// ValidateRel 只做路径合规与越权拦截（对不存在的文件是宽松的，输出文件场景需要），
			// 素材存在性必须另行判定，否则缺失素材会漏过预检（03 §5.2）。
			resolved, e := h.pv.ValidateRel(localSource, rel, edlAllowedSourceExt)
			if e != nil {
				return assetFailureFromValidation(e, i, rel)
			}
			if !fileExists(resolved) {
				return errCodeAssetMissing, http.StatusNotFound, &edlViolation{
					Msg:    "素材不存在",
					Detail: gin.H{"field": "clips.file", "index": i, "file": rel},
				}
			}
			continue
		}
		full := filepath.Join(localSource, filepath.FromSlash(rel))
		if !pathWithin(localSource, full) {
			return errCodeAssetNotInRoot, http.StatusForbidden, &edlViolation{
				Msg:    "素材不在授权目录内",
				Detail: gin.H{"field": "clips.file", "index": i, "file": rel},
			}
		}
		if !fileExists(full) {
			return errCodeAssetMissing, http.StatusNotFound, &edlViolation{
				Msg:    "素材不存在",
				Detail: gin.H{"field": "clips.file", "index": i, "file": rel},
			}
		}
	}
	return "", 0, nil
}

// assetFailureFromValidation 把 D-02 结构化路径错误映射为渲染提交的契约响应（03 §5.1）。
// 文案沿用 B-04 既有口径（前端与测试已对齐），原始 reason 放入 detail 便于定位。
func assetFailureFromValidation(e *edlValidationError, idx int, rel string) (string, int, *edlViolation) {
	msg := "素材相对路径非法"
	switch e.Code {
	case errCodeAssetNotInRoot:
		msg = "素材不在授权目录内"
	case errCodeAssetMissing:
		msg = "素材不存在"
	}
	return e.Code, edlValidateStatus(e.Code), &edlViolation{
		Msg:    msg,
		Detail: gin.H{"field": "clips.file", "index": idx, "file": rel, "reason": e.Msg},
	}
}

// pathWithin 判断 target 是否落在 root 目录内（含自身）。
func pathWithin(root, target string) bool {
	r, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	t, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	if r == t {
		return true
	}
	if !strings.HasSuffix(r, string(filepath.Separator)) {
		r += string(filepath.Separator)
	}
	return strings.HasPrefix(t, r)
}

// fileExists 判定路径存在且为普通文件。
func fileExists(p string) bool {
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// fileExistsIn 判定 dir 下是否存在名为 name 的文件；dir 为空（本地不可见）时返回 false。
func fileExistsIn(dir, name string) bool {
	if dir == "" || name == "" {
		return false
	}
	return fileExists(filepath.Join(dir, name))
}

// isDir 判定路径存在且为目录。
func isDir(p string) bool {
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// destOutputPath 记录成品落点：本地可见时写本地绝对路径，否则写 POSIX 相对路径（destRoot/output）。
func destOutputPath(destRoot, localDest, output string) string {
	if localDest != "" {
		return filepath.Join(localDest, output)
	}
	return strings.TrimRight(destRoot, "/") + "/" + output
}

// outputArtifactExists 判定既有成功任务的成品是否仍在（06 §4.4 幂等复用条件）。
func (h *Handlers) outputArtifactExists(t Task, localDest string) bool {
	if t.OutputName != "" && fileExistsIn(localDest, t.OutputName) {
		return true
	}
	if t.OutputFile != "" && fileExists(t.OutputFile) {
		return true
	}
	return false
}

// ===== 内部：载荷构造与幂等键 =====

// buildRenderPayload 构造 RenderTaskPayload（03 §2.2 / §3.2；与 FVCS/pkg/protocol 同形）。
func buildRenderPayload(proj Project, sourceRoot, destRoot, output, presetKey string, spec edlPresetSpec) RenderTaskPayload {
	clips := make([]WireClip, 0, len(proj.Clips))
	var total int64
	for i := range proj.Clips {
		cl := proj.Clips[i]
		clips = append(clips, WireClip{
			File:  cl.File,
			In:    msToTimecode(cl.InMs),
			Out:   msToTimecode(cl.OutMs),
			Speed: 1.0, // P0 恒 1.0（03 §2.5）
		})
		total += cl.OutMs - cl.InMs
	}
	return RenderTaskPayload{
		Type:       renderPayloadType,
		ProjectID:  proj.ID,
		ProjectRev: proj.Rev,
		SourceRoot: strings.TrimRight(sourceRoot, "/"),
		DestRoot:   strings.TrimRight(destRoot, "/"),
		Output:     output,
		Timeline:   proj.Timeline,
		Clips:      clips,
		Profile: RenderProfile{
			PresetKey: presetKey,
			Container: renderContainerMP4,
			Video: RenderVideoProfile{
				Codec:  spec.Codec,
				CRF:    spec.CRF,
				Preset: spec.Encoder,
				PixFmt: renderVideoPixFmt,
			},
			Audio: RenderAudioProfile{
				Codec:      renderAudioCodecAAC,
				Bitrate:    renderAudioKbps,
				Channels:   renderAudioChannels,
				SampleRate: proj.Timeline.SampleRate,
			},
		},
		TotalMs: total,
	}
}

// computeFastCopyAllowed 同源直通预判（05 §3.1）：presetKey=copy_same_source
// 且全部片段指向同一素材文件、首尾相接（首段 in=0，后续 in=前段 out）、speed 恒为 1。
// 与 FVCS/pkg/task.previewFastCopy 判定口径一致。
func computeFastCopyAllowed(p *RenderTaskPayload) bool {
	if p == nil || p.Profile.PresetKey != "copy_same_source" || len(p.Clips) == 0 {
		return false
	}
	first := p.Clips[0]
	if first.Speed != 0 && math.Abs(first.Speed-1.0) >= 1e-6 {
		return false
	}
	in0, err := parseTimecode(first.In)
	if err != nil || in0 != 0 {
		return false
	}
	prev := first.File
	prevOut, err := parseTimecode(first.Out)
	if err != nil {
		return false
	}
	for i := 1; i < len(p.Clips); i++ {
		c := p.Clips[i]
		if c.File != prev {
			return false
		}
		if c.Speed != 0 && math.Abs(c.Speed-1.0) >= 1e-6 {
			return false
		}
		inMs, err := parseTimecode(c.In)
		if err != nil || inMs != prevOut {
			return false
		}
		outMs, err := parseTimecode(c.Out)
		if err != nil {
			return false
		}
		prevOut = outMs
	}
	return true
}

// renderChecksumInput 幂等键输入（与 FVCS/pkg/protocol.checksumInput 同形，保证两侧同值）。
type renderChecksumInput struct {
	Clips   []WireClip    `json:"clips"`
	Profile RenderProfile `json:"profile"`
}

// computeRenderChecksum 幂等键：clips+profile 规范化 sha256 前 16 位（06 §4.4）。
// FastCopyAllowed 为服务端回填字段，计算前统一置 false。
func computeRenderChecksum(p *RenderTaskPayload) string {
	if p == nil {
		return ""
	}
	prof := p.Profile
	prof.FastCopyAllowed = false
	b, err := json.Marshal(renderChecksumInput{Clips: p.Clips, Profile: prof})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// msToTimecode 毫秒 → "HH:MM:SS.mmm"（03 §2.4：全整数运算）。
func msToTimecode(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3600000
	m := (ms % 3600000) / 60000
	s := (ms % 60000) / 1000
	rem := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, rem)
}

// newRenderTaskID 生成渲染任务 ID：t_<unix秒>_<6 位十六进制>（03 §4.4 示例形态）。
func newRenderTaskID(now time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", now.UnixNano(), os.Getpid())))
	return fmt.Sprintf("t_%d_%s", now.Unix(), hex.EncodeToString(sum[:])[:6])
}

// renderSubmitBody 渲染提交响应体（03 §4.4；reused 为 true 表示命中幂等复用）。
func renderSubmitBody(t Task, reused bool) gin.H {
	body := gin.H{
		"taskId":          t.ID,
		"status":          t.Status.WireName(),
		"serverId":        t.ServerID,
		"output":          t.OutputName,
		"totalMs":         t.TotalMs,
		"fastCopyAllowed": t.FastCopyAllowed,
	}
	if reused {
		body["reused"] = true
		body["msg"] = "已存在相同内容的渲染任务"
	}
	return body
}
