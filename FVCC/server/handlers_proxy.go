package main

// M4：代理工作流提交侧（04 §3.2 / §3.5）。
//
// 契约（03 §4.5 / 04 §3.2）：
//
//	POST /api/proxy  { "assetId": "a_0000000a", "file": "demo/a_01.mp4", "root": "src"? }
//	200 OK           { "taskId": "t_...", "status": "QUEUE", "proxyFile": "demo/a_01.proxy.mp4" }
//
// 决策④：入参字段以 ui-src/src/api.ts 的 ProxyRequest 为准（assetId/file/root?）；
// proxyFile 沿用 FVCS 侧口径——**相对 ProxyRoot** 的 POSIX 路径（04 §3.2 下发 smbOutputPath=<videoRoot>/_proxy）。
// 决策⑤（04 §3.5）：同一 file 已有 QUEUE/RUNNING 代理任务时返回既有 taskId（HTTP 200），不重复建任务。
//
// 职责边界：本文件只负责「提交侧」——路径闸门、素材存在性预检、同素材去重、
// 任务入队（QUEUE）与下发载荷构造；下发由调度器（B-06 dispatchRenderLike）完成，
// 完成后的 ffprobe 校验 / asset_proxies 登记 / proxy_ready 广播见 proxy_flow.go。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/logger"
)

// 代理提交侧常量与错误码（03 §5.3 / 04 §6）。
const (
	// proxyPayloadType 代理载荷 type 取值（与 FVCS protocol.PayloadTypeGenProxy 同值）。
	proxyPayloadType = "GenProxy"
	// errCodeProxyVerifyFailed 代理时长/帧率校验不通过（04 §6；由 proxy_flow.go 使用）。
	errCodeProxyVerifyFailed = "E_PROXY_VERIFY_FAILED"
	// errCodeProxyQuotaExceeded 代理配额超限且无法清理（04 §6；§3.6 兜底，P0 仅登记不阻塞）。
	errCodeProxyQuotaExceeded = "E_PROXY_QUOTA_EXCEEDED"
	// errCodeAssetIDInvalid 03 §5.3 未定义，实现补充（见 10 号台账）。
	errCodeAssetIDInvalid = "E_ASSET_ID_INVALID"
)

// proxyRequest 代理生成请求体（api.ts ProxyRequest；04 §3.2）。
type proxyRequest struct {
	AssetID string `json:"assetId"`
	File    string `json:"file"`
	Root    string `json:"root"`
}

// proxyRelOf 源素材相对路径 → 代理相对路径（04 §3.2：demo/a_01.mp4 → demo/a_01.proxy.mp4）。
func proxyRelOf(file string) string {
	dir, base := path.Split(strings.TrimSpace(file))
	ext := path.Ext(base)
	if ext == "" {
		ext = ".mp4"
	}
	return dir + strings.TrimSuffix(base, ext) + ".proxy" + ext
}

// handleProxyRequest POST /api/proxy（04 §3.2 / §3.5）。
func (h *Handlers) handleProxyRequest(c *gin.Context) {
	var in proxyRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, edlMaxBodyBytes)
	if err := c.ShouldBindJSON(&in); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			edlErr(c, http.StatusRequestEntityTooLarge, errCodeEDLTooLarge,
				"代理请求体超过 256 KB 上限", gin.H{"maxBytes": edlMaxBodyBytes})
			return
		}
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, "请求体解析失败: "+err.Error(), nil)
		return
	}

	file := strings.TrimSpace(in.File)
	// 相对路径闸门复用 clip 文件的 L1/L2 结构校验（07 §3.2）：1..255、POSIX 相对、无 .. 段。
	if v := validateClipFile(file); v != nil {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, v.Msg, v.Detail)
		return
	}
	assetID := strings.TrimSpace(in.AssetID)
	if assetID == "" || len(assetID) > 64 {
		edlErr(c, http.StatusBadRequest, errCodeAssetIDInvalid, "assetId 非法（1~64）",
			gin.H{"field": "assetId"})
		return
	}
	// 代理只针对源素材生成（04 §3.1）；root 缺省即 src。
	// 修复①：root 亦接受「授权素材根本地绝对路径」，剪辑页据此对各授权目录下的素材生成代理。
	root := normalizeMediaRoot(in.Root)
	if root == "proxy" || root == "dest" {
		edlErr(c, http.StatusBadRequest, errCodeRootUnknown, "代理仅支持对源素材生成（root=src）",
			gin.H{"field": "root", "root": root})
		return
	}
	srcLocal, rootCode := h.ResolveRoot(root)
	if rootCode != "" || !h.IsAuthorizedSrcRoot(srcLocal) {
		edlErr(c, http.StatusBadRequest, errCodeRootUnknown, "root 非法：不在授权的素材根范围内",
			gin.H{"field": "root", "root": root})
		return
	}
	proxyLocal := h.ProxyLocalForSrcRoot(srcLocal)

	// 决策⑤ / 04 §3.5：同 file 已有 QUEUE/RUNNING 代理任务 → 返回既有 taskId（HTTP 200，非错误）。
	// 修复①：多根场景下相对路径可能同名，故仅在「素材根一致」时才算重复。
	if exist, ok := h.store.FindActiveProxyTask(file); ok && h.sameProxyTaskSrcRoot(exist, srcLocal) {
		c.JSON(http.StatusOK, gin.H{
			"taskId":       exist.ID,
			"status":       exist.Status.WireName(),
			"proxyFile":    proxyRelOf(file),
			"deduplicated": true,
		})
		return
	}

	if srcLocal == "" {
		edlErr(c, http.StatusBadRequest, errCodeRootUnknown, "未配置素材根（videoRoot），无法生成代理",
			gin.H{"field": "videoRoot"})
		return
	}
	if proxyLocal == "" {
		edlErr(c, http.StatusBadRequest, errCodeRootUnknown, "未配置代理根（proxyRoot），无法生成代理",
			gin.H{"field": "proxyRoot"})
		return
	}
	// 素材存在性预检（03 §5.2：源文件存在性 → E_ASSET_MISSING）。
	srcPath := filepath.Join(srcLocal, filepath.FromSlash(file))
	if !fileExists(srcPath) {
		edlErr(c, http.StatusNotFound, errCodeAssetMissing, "素材不存在或不可访问", gin.H{"file": file})
		return
	}

	tmpl := defaultProxyTemplate()
	proxyFile := proxyRelOf(file)
	payload := GenProxyPayload{
		Type:       proxyPayloadType,
		AssetID:    assetID,
		SrcFile:    file,
		ProxyFile:  proxyFile,
		Template:   tmpl,
		SourceRoot: toPOSIXRoot(srcLocal),
	}
	payloadJSON, merr := json.Marshal(payload)
	if merr != nil {
		edlErr(c, http.StatusInternalServerError, errCodeEDLInvalid, "载荷序列化失败: "+merr.Error(), nil)
		return
	}

	now := time.Now()
	task := Task{
		ID:          newRenderTaskID(now),
		OrderID:     h.store.NextOrderID(),
		SourceFile:  file,
		OutputFile:  filepath.Join(proxyLocal, filepath.FromSlash(proxyFile)),
		FileName:    path.Base(file),
		OutputName:  path.Base(proxyFile),
		ProfileID:   tmpl.PresetKey,
		ProfileName: tmpl.PresetKey,
		Status:      StatusQueue,
		Progress:    0,
		RetryType:   Retryable,
		TaskType:    TaskTypeGenProxy,
		PayloadJSON: string(payloadJSON),
		TraceID:     newTraceID(now),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	h.store.UpsertTask(task)
	h.store.AppendAudit(AuditEntry{
		Actor: edlActor(c), Action: "proxy.submit", Target: task.ID, Result: "ok",
		Detail: fmt.Sprintf("asset=%s file=%s proxy=%s preset=%s", assetID, file, proxyFile, tmpl.PresetKey),
	})
	if h.hub != nil {
		h.hub.BroadcastTaskUpdate(task.ID, task.Status.WireName(), 0, "代理任务已入队")
	}
	logger.Info("proxy", "代理提交 task=%v asset=%v file=%v proxy=%v preset=%v",
		task.ID, assetID, file, proxyFile, tmpl.PresetKey)

	c.JSON(http.StatusOK, gin.H{
		"taskId":    task.ID,
		"status":    StatusQueue.WireName(),
		"proxyFile": proxyFile,
	})
}

// sameProxyTaskSrcRoot 判定既有代理任务与本次请求是否使用同一素材根（修复①）：
// 多根场景下不同授权目录可能出现相同的相对路径，不能仅凭 SrcFile 去重。
func (h *Handlers) sameProxyTaskSrcRoot(t Task, srcLocal string) bool {
	var p GenProxyPayload
	if err := json.Unmarshal([]byte(t.PayloadJSON), &p); err != nil {
		return true // 载荷不可解析：沿用旧口径，避免误判为非重复而重复排队
	}
	base := strings.TrimSpace(p.SourceRoot)
	if base == "" {
		// 旧任务（无 SourceRoot 字段）按缺省素材根处理。
		return samePath(h.resolveMediaRoots().SourceLocal, srcLocal)
	}
	return samePath(filepath.FromSlash(base), srcLocal)
}
