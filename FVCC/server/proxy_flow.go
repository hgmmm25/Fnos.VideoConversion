package main

// M4：代理工作流收尾（04 §3.4 / §3.5 / §4.1）。
//
// 职责：GEN_PROXY 任务在 FVCS 侧成功后，FVCC 侧完成
//
//	① 轮询获知节点终态（04 §3.4：FVCC 轮询/回调获知结果）；
//	② ffprobe 校验代理一致性：时长差 ≤ 1 帧、帧率相等、高度 ≤ 720、音轨存在性一致（04 §4.1）；
//	③ 写 asset_proxies 登记（state=ready）/ 校验失败标 invalid 并删除代理文件；
//	④ 广播 WS proxy_ready，前端据此切换预览源（04 §3.4、03 §6）。
//
// 设计取舍（对齐本任务决策）：本仓库 Store 为 JSON 持久化，asset_proxies 落
// dataDir/asset_proxies.json（store_proxy.go），不引 SQLite；代理根/素材根由
// resolveMediaRootsFor（security.go）按 Settings 与缺省推导解析，与提交侧同源。

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fvcc/logger"
)

// 代理收尾常量。
const (
	// proxyPollInterval GEN_PROXY 远端终态轮询间隔（04 §3.4；避免每 tick 打 QueryTask）。
	proxyPollInterval = 5 * time.Second
)

// ProxyProber 代理校验所需的元数据探测能力：*FFprobe 满足；单测注入桩以避免 ffprobe 在环。
type ProxyProber interface {
	Probe(path string) (VideoInfo, error)
}

// SetProxyProbe 注入代理校验探测通道（装配 / 单测入口；为 nil 时运行时惰性构造 ffprobe）。
func (s *Scheduler) SetProxyProbe(p ProxyProber) { s.probe = p }

// prober 返回校验用探测通道（惰性构造，仅在调度 tick 内串行调用）。
func (s *Scheduler) prober() ProxyProber {
	if s.probe != nil {
		return s.probe
	}
	if s.probeLocal == nil {
		s.probeLocal = NewFFprobe()
	}
	return s.probeLocal
}

// pollGenProxy 轮询 GEN_PROXY 任务在 FVCS 侧的终态（04 §3.4）：
// SUCCESS → 校验 + 登记 + proxy_ready；FAILED/CANCELLED → 任务收口为 ERROR。
// 非终态（RUNNING/QUEUE）直接返回，等下一个轮询窗口。
func (s *Scheduler) pollGenProxy(t Task) {
	if s.remote == nil || strings.TrimSpace(t.RemoteTaskID) == "" || strings.TrimSpace(t.ServerID) == "" {
		return
	}
	now := time.Now()
	if v, ok := s.proxyPollAt.Load(t.ID); ok {
		if last, ok2 := v.(time.Time); ok2 && now.Sub(last) < proxyPollInterval {
			return
		}
	}
	s.proxyPollAt.Store(t.ID, now)

	server, ok := s.store.GetServer(t.ServerID)
	if !ok {
		s.failRenderTaskPermanent(t, "E_NODE_OFFLINE", "代理任务的渲染节点已不存在")
		return
	}
	rs, err := s.remote.QueryTaskDetail(server, t.RemoteTaskID)
	if err != nil {
		// 轮询失败不改任务状态：保持 RUNNING，等下个窗口重试（避免网络抖动误杀代理任务）。
		logger.Warn("proxy", "代理终态轮询失败 task=%s: %v", t.ID, err)
		return
	}
	switch strings.ToUpper(strings.TrimSpace(rs.Status)) {
	case "SUCCESS", "COMPLETED":
		s.finishGenProxy(t)
	case "FAILED", "ERROR":
		// 失败节点细化上报：优先采用 FVCS 上报的 E_* 码与失败原因（含阶段），
		// 仅在节点未携带时回退泛化码/文案。
		s.failRenderTaskPermanent(t, remoteFailureCode(rs), remoteFailureMessage("代理生成失败", rs))
	case "CANCELED", "CANCELLED", "STOPPED":
		s.failRenderTaskPermanent(t, "E_RENDER_FAILED", "代理任务已取消")
	}
}

// remoteFailureCode 远端失败错误码：优先取远端上报的 E_* 码，缺失时回退 E_RENDER_FAILED。
func remoteFailureCode(st RemoteTaskStatus) string {
	if code := strings.TrimSpace(st.ErrorCode); code != "" {
		return code
	}
	return errCodeRenderFailed
}

// remoteFailureMessage 组合远端失败明细（节点状态 + 渲染阶段 + 错误码 + 节点原文），
// 供任务面板/日志直接定位失败节点，避免只显示"节点状态 Failed"。
func remoteFailureMessage(prefix string, st RemoteTaskStatus) string {
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

// finishGenProxy GEN_PROXY 成功后的校验 / 登记 / 广播（04 §3.4）。
func (s *Scheduler) finishGenProxy(t Task) {
	var payload GenProxyPayload
	if err := json.Unmarshal([]byte(t.PayloadJSON), &payload); err != nil ||
		strings.TrimSpace(payload.SrcFile) == "" || strings.TrimSpace(payload.ProxyFile) == "" {
		s.failRenderTaskPermanent(t, "E_PAYLOAD_INVALID", "代理任务载荷缺失或非法，无法登记代理")
		return
	}
	srcLocal, proxyLocal, code, msg := s.proxyLocalPaths(payload)
	if code != "" {
		s.failRenderTaskPermanent(t, code, msg)
		return
	}

	srcInfo, serr := s.prober().Probe(srcLocal)
	if serr != nil {
		s.failRenderTaskPermanent(t, "E_ASSET_MISSING", "源素材探测失败: "+serr.Error())
		return
	}
	proxyInfo, perr := s.prober().Probe(proxyLocal)
	if perr != nil {
		s.markProxyInvalid(payload, t.ID)
		s.removeInvalidProxyFile(proxyLocal)
		s.failRenderTaskPermanent(t, errCodeProxyVerifyFailed, "代理文件探测失败: "+perr.Error())
		return
	}
	if verr := verifyProxyMeta(srcInfo, proxyInfo); verr != nil {
		s.markProxyInvalid(payload, t.ID)
		s.removeInvalidProxyFile(proxyLocal)
		s.failRenderTaskPermanent(t, errCodeProxyVerifyFailed, verr.Error())
		return
	}

	size := int64(0)
	if fi, err := os.Stat(proxyLocal); err == nil {
		size = fi.Size()
	}
	at := time.Now().Format(time.RFC3339)
	s.store.UpsertAssetProxy(AssetProxy{
		AssetKey:        payload.SrcFile,
		ProxyRel:        payload.ProxyFile,
		SrcDurationMs:   durationMs(srcInfo.Duration),
		ProxyDurationMs: durationMs(proxyInfo.Duration),
		SrcFPS:          srcInfo.Fps,
		ProxyFPS:        proxyInfo.Fps,
		Mode:            ProxyModeFull,
		State:           ProxyStateReady,
		TaskID:          t.ID,
		GeneratedAt:     at,
		LastAccessAt:    at,
		SizeBytes:       size,
	})
	s.store.SetTaskFinishedAt(t.ID, time.Now())
	s.store.UpdateTaskStatus(t.ID, StatusCompleted, 100, "")
	s.hub.BroadcastTaskUpdate(t.ID, string(StatusCompleted), 100, "代理生成完成")
	logger.InfoT("proxy", t.TraceID, "代理就绪 asset=%v src=%v proxy=%v durationMs=%d fps=%v",
		payload.AssetID, payload.SrcFile, payload.ProxyFile, durationMs(proxyInfo.Duration), proxyInfo.Fps)
	// 04 §3.4：WS proxy_ready 广播（Hub 内含 10s 同 assetId 去重）。
	s.hub.BroadcastProxyReady(payload.AssetID, map[string]interface{}{
		"assetId":    payload.AssetID,
		"proxyFile":  payload.ProxyFile,
		"durationMs": durationMs(proxyInfo.Duration),
	})
	s.proxyPollAt.Delete(t.ID)
}

// proxyLocalPaths 由载荷解析源/代理本地绝对路径（根推导与提交侧同源）。
// 修复①：载荷携带 SourceRoot（非缺省授权根）时，源根取该根、代理根取 <该根>/_proxy，
// 与 handlers_proxy.go 提交侧、remote.go 下发侧保持同一口径。
func (s *Scheduler) proxyLocalPaths(payload GenProxyPayload) (srcLocal, proxyLocal, code, msg string) {
	roots := resolveMediaRootsFor(s.store, s.pv)
	if roots.SourceLocal == "" {
		return "", "", errCodeRootUnknown, "未配置素材根（videoRoot），无法校验代理"
	}
	srcLocal = roots.SourceLocal
	if sr := strings.TrimSpace(payload.SourceRoot); sr != "" {
		srcLocal = localPathOf(sr)
	}
	proxyLocal = proxyLocalForSrcRoot(s.store, s.pv, srcLocal)
	if proxyLocal == "" {
		return "", "", errCodeRootUnknown, "未配置代理根（proxyRoot），无法校验代理"
	}
	return filepath.Join(srcLocal, filepath.FromSlash(payload.SrcFile)),
		filepath.Join(proxyLocal, filepath.FromSlash(payload.ProxyFile)), "", ""
}

// markProxyInvalid 登记/标记代理为 invalid（04 §3.4：校验失败不自动重试，标记后提示用户）。
func (s *Scheduler) markProxyInvalid(payload GenProxyPayload, taskID string) {
	if s.store.MarkAssetProxyState(payload.SrcFile, ProxyStateInvalid, taskID) {
		return
	}
	s.store.UpsertAssetProxy(AssetProxy{
		AssetKey:    payload.SrcFile,
		ProxyRel:    payload.ProxyFile,
		Mode:        ProxyModeFull,
		State:       ProxyStateInvalid,
		TaskID:      taskID,
		GeneratedAt: time.Now().Format(time.RFC3339),
	})
}

// removeInvalidProxyFile 删除校验未通过的代理文件（仅该次生成的产物，不触碰其他文件）。
func (s *Scheduler) removeInvalidProxyFile(p string) {
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		logger.Warn("proxy", "删除失效代理文件失败: %v", err)
	}
}

// verifyProxyMeta 代理一致性校验（04 §3.4 校验项 + §4.1 P0 硬约束）。
//
// 探测信息缺失（ffprobe 不可用 → Fps=="unknown"）时无法执行严格校验，放行登记：
// 宁可让前端在预览失败时兜底，也不把可用代理误标 invalid。
func verifyProxyMeta(src, px VideoInfo) error {
	if !fpsKnown(src.Fps) || !fpsKnown(px.Fps) {
		return nil
	}
	// 同帧率：分数串相等（如 30000/1001）。
	if strings.TrimSpace(src.Fps) != strings.TrimSpace(px.Fps) {
		return fmt.Errorf("帧率不一致（源 %s / 代理 %s）", src.Fps, px.Fps)
	}
	// 同时长：|dur_proxy - dur_src| ≤ 1 帧。
	if src.Duration > 0 && px.Duration > 0 {
		tolMs := 1000.0 / 30.0
		if f, ok := parseFPS(src.Fps); ok && f > 0 {
			tolMs = 1000.0 / f
		}
		if diff := math.Abs(float64(durationMs(src.Duration) - durationMs(px.Duration))); diff > tolMs {
			return fmt.Errorf("时长不一致（源 %dms / 代理 %dms，容差 %.0fms）",
				durationMs(src.Duration), durationMs(px.Duration), tolMs)
		}
	}
	// 分辨率：代理高度 ≤ 720（04 §3.3）。
	if px.Height > 0 && px.Height > 720 {
		return fmt.Errorf("代理高度 %d 超出 720", px.Height)
	}
	// 同音轨存在性。
	if audioKnown(src.AudioCodec) && audioKnown(px.AudioCodec) {
		if (src.AudioCodec != "") != (px.AudioCodec != "") {
			return fmt.Errorf("音轨存在性不一致（源 %q / 代理 %q）", src.AudioCodec, px.AudioCodec)
		}
	}
	return nil
}

// fpsKnown 帧率串是否可用（ffprobe 缺失时 stubProbe 置 "unknown"）。
func fpsKnown(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && !strings.EqualFold(s, "unknown")
}

// audioKnown 音轨编码是否可用（"unknown" 视为探测不可用）。
func audioKnown(s string) bool {
	s = strings.TrimSpace(s)
	return !strings.EqualFold(s, "unknown")
}

// parseFPS 解析 ffprobe 帧率串（"30000/1001" 或 "25"）。
func parseFPS(s string) (float64, bool) {
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

// durationMs 秒 → 毫秒（向下取整到毫秒后四舍五入，避免浮点尾差）。
func durationMs(sec float64) int64 {
	if sec <= 0 {
		return 0
	}
	return int64(math.Round(sec * 1000))
}
