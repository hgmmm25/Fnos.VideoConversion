package media

// P2-1 B轮：M4 代理工作流收尾迁入 internal/media（原根包 proxy_flow.go）。
//
// 职责：GEN_PROXY 任务在 FVCS 侧成功后，FVCC 侧完成
//
//	① 轮询获知节点终态（04 §3.4：FVCC 轮询/回调获知结果）；
//	② ffprobe 校验代理一致性：时长差 ≤ 1 帧、帧率相等、高度 ≤ 720、音轨存在性一致（04 §4.1）；
//	③ 写 asset_proxies 登记（state=ready）/ 校验失败标 invalid 并删除代理文件；
//	④ 广播 WS proxy_ready，前端据此切换预览源（04 §3.4、03 §6）。
//
// 依赖收敛：Store / Hub / Remote / 调度失败收口均以最小接口注入，
// 源/代理本地根推导（根包 security 域 resolveMediaRootsFor）经 ProxyPathResolver 注入，
// 本包不反向依赖根包。

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"fvcc/internal/remote"
	"fvcc/internal/store/model"
	"fvcc/logger"
)

// 代理收尾常量。
const (
	// ProxyPollInterval GEN_PROXY 远端终态轮询间隔（04 §3.4；避免每 tick 打 QueryTask）。
	ProxyPollInterval = 5 * time.Second

	// ErrCodeProxyVerifyFailed 代理一致性校验失败错误码（与根包 handlers_proxy.go 提交侧同口径）。
	ErrCodeProxyVerifyFailed = "E_PROXY_VERIFY_FAILED"
)

// ProxyProber 代理校验所需的元数据探测能力：*FFprobe 满足；单测注入桩以避免 ffprobe 在环。
type ProxyProber interface {
	Probe(path string) (model.VideoInfo, error)
}

// ProxyStore 代理收尾所需的存储能力（*store.Store 满足）。
type ProxyStore interface {
	GetServer(id string) (model.Server, bool)
	UpsertAssetProxy(p model.AssetProxy)
	MarkAssetProxyState(assetKey, state, taskID string) bool
	SetTaskFinishedAt(id string, ts time.Time)
	UpdateTaskStatus(id string, status model.TaskStatus, progress float64, errMsg string)
}

// ProxyHub 代理收尾所需的 WS 广播能力（*ws.Hub 满足）。
type ProxyHub interface {
	BroadcastTaskUpdate(taskID, status string, progress float64, message string)
	BroadcastProxyReady(assetID string, data interface{}) bool
}

// ProxyRemote 远端任务查询能力（*remote.RemoteClient 满足）。
type ProxyRemote interface {
	QueryTaskDetail(server model.Server, remoteTaskID string) (remote.RemoteTaskStatus, error)
}

// ProxyFinalizer 渲染任务不可重试失败收口（调度域注入：置 ERROR / 冷却 / 归档）。
type ProxyFinalizer interface {
	FailPermanent(taskID string, code, msg string)
}

// ProxyPathResolver 解析源/代理本地绝对路径（M4 根推导与提交侧同源，由根包 security 域注入）。
type ProxyPathResolver interface {
	ResolveProxyPaths(payload model.GenProxyPayload) (srcLocal, proxyLocal, code, msg string)
}

// ProxyPathResolverFunc 函数适配器，便于根包注入闭包。
type ProxyPathResolverFunc func(payload model.GenProxyPayload) (srcLocal, proxyLocal, code, msg string)

// ResolveProxyPaths 实现 ProxyPathResolver。
func (f ProxyPathResolverFunc) ResolveProxyPaths(payload model.GenProxyPayload) (string, string, string, string) {
	return f(payload)
}

// ProxyWorkflow 代理收尾工作流（原 Scheduler 上 proxy_flow 逻辑的独立载体）。
type ProxyWorkflow struct {
	store      ProxyStore
	hub        ProxyHub
	remote     ProxyRemote
	finisher   ProxyFinalizer
	resolver   ProxyPathResolver
	probe      ProxyProber
	probeLocal ProxyProber
	pollAt     sync.Map // taskID → 上次远端终态轮询时刻
}

// NewProxyWorkflow 构造代理收尾工作流。
func NewProxyWorkflow(store ProxyStore, hub ProxyHub, remote ProxyRemote, finisher ProxyFinalizer, resolver ProxyPathResolver) *ProxyWorkflow {
	return &ProxyWorkflow{store: store, hub: hub, remote: remote, finisher: finisher, resolver: resolver}
}

// SetProbe 注入代理校验探测通道（装配 / 单测入口；为 nil 时运行时惰性构造 ffprobe）。
func (w *ProxyWorkflow) SetProbe(p ProxyProber) { w.probe = p }

// prober 返回校验用探测通道（惰性构造，仅在调度 tick 内串行调用）。
func (w *ProxyWorkflow) prober() ProxyProber {
	if w.probe != nil {
		return w.probe
	}
	if w.probeLocal == nil {
		w.probeLocal = NewFFprobe()
	}
	return w.probeLocal
}

// Poll 轮询 GEN_PROXY 任务在 FVCS 侧的终态（04 §3.4）：
// SUCCESS → 校验 + 登记 + proxy_ready；FAILED/CANCELLED → 任务收口为 ERROR。
// 非终态（RUNNING/QUEUE）直接返回，等下一个轮询窗口。
func (w *ProxyWorkflow) Poll(t model.Task) {
	if w.remote == nil || strings.TrimSpace(t.RemoteTaskID) == "" || strings.TrimSpace(t.ServerID) == "" {
		return
	}
	now := time.Now()
	if v, ok := w.pollAt.Load(t.ID); ok {
		if last, ok2 := v.(time.Time); ok2 && now.Sub(last) < ProxyPollInterval {
			return
		}
	}
	w.pollAt.Store(t.ID, now)

	server, ok := w.store.GetServer(t.ServerID)
	if !ok {
		w.finisher.FailPermanent(t.ID, "E_NODE_OFFLINE", "代理任务的渲染节点已不存在")
		return
	}
	rs, err := w.remote.QueryTaskDetail(server, t.RemoteTaskID)
	if err != nil {
		// 轮询失败不改任务状态：保持 RUNNING，等下个窗口重试（避免网络抖动误杀代理任务）。
		logger.Warn("proxy", "代理终态轮询失败 task=%s: %v", t.ID, err)
		return
	}
	switch strings.ToUpper(strings.TrimSpace(rs.Status)) {
	case "SUCCESS", "COMPLETED":
		w.Finish(t)
	case "FAILED", "ERROR":
		// 失败节点细化上报：优先采用 FVCS 上报的 E_* 码与失败原因（含阶段），
		// 仅在节点未携带时回退泛化码/文案。
		w.finisher.FailPermanent(t.ID, RemoteFailureCode(rs), RemoteFailureMessage("代理生成失败", rs))
	case "CANCELED", "CANCELLED", "STOPPED":
		w.finisher.FailPermanent(t.ID, "E_RENDER_FAILED", "代理任务已取消")
	}
}

// RemoteFailureCode 远端失败错误码：优先取远端上报的 E_* 码，缺失时回退 E_RENDER_FAILED。
func RemoteFailureCode(st remote.RemoteTaskStatus) string {
	if code := strings.TrimSpace(st.ErrorCode); code != "" {
		return code
	}
	return remote.ErrCodeRenderFailed
}

// RemoteFailureMessage 组合远端失败明细（节点状态 + 渲染阶段 + 错误码 + 节点原文），
// 供任务面板/日志直接定位失败节点，避免只显示"节点状态 Failed"。
func RemoteFailureMessage(prefix string, st remote.RemoteTaskStatus) string {
	msg := fmt.Sprintf("%s（节点状态 %s", prefix, strings.TrimSpace(st.Status))
	if stage := strings.TrimSpace(st.Stage); stage != "" {
		msg += "，阶段 " + stage
	}
	if code := strings.TrimSpace(st.ErrorCode); code != "" {
		msg += "，" + code
	}
	msg += "）"
	if detail := strings.TrimSpace(st.ErrorMessage); detail != "" {
		msg += ": " + detail
	}
	return msg
}

// Finish GEN_PROXY 成功后的校验 / 登记 / 广播（04 §3.4）。
func (w *ProxyWorkflow) Finish(t model.Task) {
	var payload model.GenProxyPayload
	if err := json.Unmarshal([]byte(t.PayloadJSON), &payload); err != nil ||
		strings.TrimSpace(payload.SrcFile) == "" || strings.TrimSpace(payload.ProxyFile) == "" {
		w.finisher.FailPermanent(t.ID, "E_PAYLOAD_INVALID", "代理任务载荷缺失或非法，无法登记代理")
		return
	}
	srcLocal, proxyLocal, code, msg := w.resolver.ResolveProxyPaths(payload)
	if code != "" {
		w.finisher.FailPermanent(t.ID, code, msg)
		return
	}

	srcInfo, serr := w.prober().Probe(srcLocal)
	if serr != nil {
		w.finisher.FailPermanent(t.ID, "E_ASSET_MISSING", "源素材探测失败: "+serr.Error())
		return
	}
	proxyInfo, perr := w.prober().Probe(proxyLocal)
	if perr != nil {
		w.markProxyInvalid(payload, t.ID)
		w.removeInvalidProxyFile(proxyLocal)
		w.finisher.FailPermanent(t.ID, ErrCodeProxyVerifyFailed, "代理文件探测失败: "+perr.Error())
		return
	}
	if verr := VerifyProxyMeta(srcInfo, proxyInfo); verr != nil {
		w.markProxyInvalid(payload, t.ID)
		w.removeInvalidProxyFile(proxyLocal)
		w.finisher.FailPermanent(t.ID, ErrCodeProxyVerifyFailed, verr.Error())
		return
	}

	size := int64(0)
	if fi, err := os.Stat(proxyLocal); err == nil {
		size = fi.Size()
	}
	at := time.Now().Format(time.RFC3339)
	w.store.UpsertAssetProxy(model.AssetProxy{
		AssetKey:        payload.SrcFile,
		ProxyRel:        payload.ProxyFile,
		SrcDurationMs:   DurationMs(srcInfo.Duration),
		ProxyDurationMs: DurationMs(proxyInfo.Duration),
		SrcFPS:          srcInfo.Fps,
		ProxyFPS:        proxyInfo.Fps,
		Mode:            model.ProxyModeFull,
		State:           model.ProxyStateReady,
		TaskID:          t.ID,
		GeneratedAt:     at,
		LastAccessAt:    at,
		SizeBytes:       size,
	})
	w.store.SetTaskFinishedAt(t.ID, time.Now())
	w.store.UpdateTaskStatus(t.ID, model.StatusCompleted, 100, "")
	w.hub.BroadcastTaskUpdate(t.ID, string(model.StatusCompleted), 100, "代理生成完成")
	logger.InfoT("proxy", t.TraceID, "代理就绪 asset=%v src=%v proxy=%v durationMs=%d fps=%v",
		payload.AssetID, payload.SrcFile, payload.ProxyFile, DurationMs(proxyInfo.Duration), proxyInfo.Fps)
	// 04 §3.4：WS proxy_ready 广播（Hub 内含 10s 同 assetId 去重）。
	w.hub.BroadcastProxyReady(payload.AssetID, map[string]interface{}{
		"assetId":    payload.AssetID,
		"proxyFile":  payload.ProxyFile,
		"durationMs": DurationMs(proxyInfo.Duration),
	})
	w.pollAt.Delete(t.ID)
}

// markProxyInvalid 登记/标记代理为 invalid（04 §3.4：校验失败不自动重试，标记后提示用户）。
func (w *ProxyWorkflow) markProxyInvalid(payload model.GenProxyPayload, taskID string) {
	if w.store.MarkAssetProxyState(payload.SrcFile, model.ProxyStateInvalid, taskID) {
		return
	}
	w.store.UpsertAssetProxy(model.AssetProxy{
		AssetKey:    payload.SrcFile,
		ProxyRel:    payload.ProxyFile,
		Mode:        model.ProxyModeFull,
		State:       model.ProxyStateInvalid,
		TaskID:      taskID,
		GeneratedAt: time.Now().Format(time.RFC3339),
	})
}

// removeInvalidProxyFile 删除校验未通过的代理文件（仅该次生成的产物，不触碰其他文件）。
func (w *ProxyWorkflow) removeInvalidProxyFile(p string) {
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		logger.Warn("proxy", "删除失效代理文件失败: %v", err)
	}
}

// VerifyProxyMeta 代理一致性校验（04 §3.4 校验项 + §4.1 P0 硬约束）。
//
// 探测信息缺失（ffprobe 不可用 → Fps=="unknown"）时无法执行严格校验，放行登记：
// 宁可让前端在预览失败时兜底，也不把可用代理误标 invalid。
func VerifyProxyMeta(src, px model.VideoInfo) error {
	if !FpsKnown(src.Fps) || !FpsKnown(px.Fps) {
		return nil
	}
	// 同帧率：分数串相等（如 30000/1001）。
	if strings.TrimSpace(src.Fps) != strings.TrimSpace(px.Fps) {
		return fmt.Errorf("帧率不一致（源 %s / 代理 %s）", src.Fps, px.Fps)
	}
	// 同时长：|dur_proxy - dur_src| ≤ 1 帧。
	if src.Duration > 0 && px.Duration > 0 {
		tolMs := 1000.0 / 30.0
		if f, ok := ParseFPS(src.Fps); ok && f > 0 {
			tolMs = 1000.0 / f
		}
		if diff := math.Abs(float64(DurationMs(src.Duration) - DurationMs(px.Duration))); diff > tolMs {
			return fmt.Errorf("时长不一致（源 %dms / 代理 %dms，容差 %.0fms）",
				DurationMs(src.Duration), DurationMs(px.Duration), tolMs)
		}
	}
	// 分辨率：代理高度 ≤ 720（04 §3.3）。
	if px.Height > 0 && px.Height > 720 {
		return fmt.Errorf("代理高度 %d 超出 720", px.Height)
	}
	// 同音轨存在性。
	if AudioKnown(src.AudioCodec) && AudioKnown(px.AudioCodec) {
		if (src.AudioCodec != "") != (px.AudioCodec != "") {
			return fmt.Errorf("音轨存在性不一致（源 %q / 代理 %q）", src.AudioCodec, px.AudioCodec)
		}
	}
	return nil
}

// FpsKnown 帧率串是否可用（ffprobe 缺失时 stubProbe 置 "unknown"）。
func FpsKnown(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && !strings.EqualFold(s, "unknown")
}

// AudioKnown 音轨编码是否可用（"unknown" 视为探测不可用）。
func AudioKnown(s string) bool {
	s = strings.TrimSpace(s)
	return !strings.EqualFold(s, "unknown")
}

// ParseFPS 解析 ffprobe 帧率串（"30000/1001" 或 "25"）。
func ParseFPS(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "unknown") {
		return 0, false
	}
	if i := strings.IndexByte(s, '/'); i > 0 {
		num, err1 := strconv.ParseFloat(s[:i], 64)
		den, err2 := strconv.ParseFloat(s[i+1:], 64)
		if err1 != nil || err2 != nil || den == 0 {
			return 0, false
		}
		return num / den, true
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// DurationMs 秒 → 毫秒（向下取整到毫秒后四舍五入，避免浮点尾差）。
func DurationMs(sec float64) int64 {
	if sec <= 0 {
		return 0
	}
	return int64(math.Round(sec * 1000))
}
