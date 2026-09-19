package media

// roots.go — P2-1 B轮：M4 预览网关三根解析（04 §2.4、§2.6）随 media 域收口。
// 原实现位于根包 security.go（resolveMediaRootsFor / proxyLocalForSrcRoot /
// localPathOf / m4ProxyRootDefault / m4CacheRootDefault / mediaRoots /
// errCodeRootUnknown），迁入本包后由根包 security.go 薄转发，保持根包调用点
// 零改动；阶段 C 根包收口后删除转发。

import (
	"path/filepath"
	"runtime"
	"strings"

	"fvcc/internal/security"
	"fvcc/internal/store/model"
)

// ErrCodeRootUnknown 非法/未配置的 root 参数（04 §6；属编码缺陷，服务端按 ERROR 记录）。
const ErrCodeRootUnknown = "E_ROOT_UNKNOWN"

// MediaRoots 预览网关三根（src/proxy/dest）的 POSIX 契约形态与本地物理路径（04 §2.6）。
type MediaRoots struct {
	SourcePOSIX string
	ProxyPOSIX  string
	DestPOSIX   string
	SourceLocal string
	ProxyLocal  string
	DestLocal   string
}

// SettingsProvider 三根解析所需的设置读取能力（*store.Store 满足）。
type SettingsProvider interface {
	GetSettings() model.Settings
}

// M4ProxyRootDefault / M4CacheRootDefault 代理根与缩略图缓存根的默认推导（04 §2.4、§2.6）。
// 决策②：Settings 未显式配置时按 videoRoot 推导，不引入额外持久化。
func M4ProxyRootDefault(videoRoot string) string {
	return strings.TrimRight(strings.TrimSpace(videoRoot), "/") + "/_proxy"
}

func M4CacheRootDefault(videoRoot string) string {
	return strings.TrimRight(strings.TrimSpace(videoRoot), "/") + "/_wve_cache"
}

// LocalPathOf POSIX 契约路径 → 本地物理路径；空串原样返回（未配置）。
func LocalPathOf(posixPath string) string {
	p := strings.TrimSpace(posixPath)
	if p == "" {
		return ""
	}
	return filepath.FromSlash(p)
}

// ResolveMediaRoots 包级三根解析（04 §2.6）：与根包 (h *Handlers) resolveMediaRoots
// 同规则，供无 Handlers 上下文的调用点（如调度器侧的代理路径推导）复用：
//   - src/dest 复用既有缺省回落（授权目录首个 / <videoRoot>/_exports）；
//   - proxy 取 Settings.proxyRoot，为空时回落 <videoRoot>/_proxy。
func ResolveMediaRoots(store SettingsProvider, pv *security.PathValidator) MediaRoots {
	var cfg model.Settings
	if store != nil {
		cfg = store.GetSettings()
	}
	src := strings.TrimSpace(cfg.VideoRoot)
	if src == "" && pv != nil {
		if paths := pv.AccessPaths(); len(paths) > 0 {
			src = toPOSIXRoot(paths[0])
		}
	}
	dst := strings.TrimSpace(cfg.ExportRoot)
	if dst == "" && src != "" {
		dst = strings.TrimRight(src, "/") + "/_exports"
	}
	proxy := strings.TrimSpace(cfg.ProxyRoot)
	if proxy == "" && src != "" {
		proxy = M4ProxyRootDefault(src)
	}
	return MediaRoots{
		SourcePOSIX: strings.TrimSpace(src),
		ProxyPOSIX:  proxy,
		DestPOSIX:   strings.TrimSpace(dst),
		SourceLocal: LocalPathOf(src),
		ProxyLocal:  LocalPathOf(proxy),
		DestLocal:   LocalPathOf(dst),
	}
}

// ProxyLocalForSrcRoot 返回素材根 srcLocal 对应的本地代理根（修复①：代理与素材同根）：
// 缺省素材根 → 沿用 Settings.proxyRoot；其他授权根 → <该根>/_proxy。
// 提交侧（handlers_proxy.go）与校验侧（调度器代理路径推导）共用同一口径，避免
// 「提交时按根 B 写、校验时按 videoRoot 读」的错位。
func ProxyLocalForSrcRoot(store SettingsProvider, pv *security.PathValidator, srcLocal string) string {
	roots := ResolveMediaRoots(store, pv)
	if strings.TrimSpace(srcLocal) == "" {
		return roots.ProxyLocal
	}
	if samePath(srcLocal, roots.SourceLocal) {
		return roots.ProxyLocal
	}
	return filepath.Join(filepath.FromSlash(srcLocal), "_proxy")
}

// toPOSIXRoot 把本地绝对路径转成契约要求的 POSIX 相对根写法（04 §2.6）。
func toPOSIXRoot(p string) string {
	return strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
}

// samePath 路径等价比较：Clean 后按平台大小写规则比较（Windows 不区分大小写）。
func samePath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}
