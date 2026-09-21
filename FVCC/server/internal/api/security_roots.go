package api

// P2-1 B轮：本文件保留 M4 预览网关三根解析（04 §2.4、§2.6）中与 Handlers
// 组装逻辑耦合的根解析方法；三根解析实现已随 media 域迁入
// internal/media/roots.go，本文件以薄转发保持根包调用点零改动。
// PathValidator 四层路径校验已迁入 internal/security/pathvalidator.go。
// 阶段 C 迁入 internal/api/media 后本文件删除。

import (
	"path/filepath"
	"runtime"
	"strings"

	"fvcc/internal/media"
	"fvcc/internal/security"
)

// ===== M4：预览网关三根解析（04 §2.4、§2.6）=====

// 转发：三根解析实现（internal/media/roots.go），根包调用点零改动。
const errCodeRootUnknown = media.ErrCodeRootUnknown

type mediaRoots = media.MediaRoots

func m4ProxyRootDefault(videoRoot string) string { return media.M4ProxyRootDefault(videoRoot) }

func m4CacheRootDefault(videoRoot string) string { return media.M4CacheRootDefault(videoRoot) }

func localPathOf(posixPath string) string { return media.LocalPathOf(posixPath) }

func resolveMediaRootsFor(store *Store, pv *security.PathValidator) mediaRoots {
	return media.ResolveMediaRoots(store, pv)
}

func proxyLocalForSrcRoot(store *Store, pv *security.PathValidator, srcLocal string) string {
	return media.ProxyLocalForSrcRoot(store, pv, srcLocal)
}

// resolveMediaRoots 解析 src/proxy/dest 三根（04 §2.6）：
//   - src/dest 复用 resolveEDLRoots 的既有缺省回落（授权目录首个 / <videoRoot>/_exports）；
//   - proxy 取 Settings.proxyRoot，为空时回落 <videoRoot>/_proxy。
func (h *Handlers) resolveMediaRoots() mediaRoots {
	return resolveMediaRootsFor(h.store, h.pv)
}

// resolveCacheRoot 解析缩略图缓存根（04 §2.4）：Settings.cacheRoot，为空回落 <videoRoot>/_wve_cache。
func (h *Handlers) resolveCacheRoot() (posixRoot, localRoot string) {
	cr := strings.TrimSpace(h.store.GetSettings().CacheRoot)
	if cr == "" {
		// 用本地素材根推导（D-02 后 resolveEDLRoots 的根可能是共享相对根，不适用于本地缓存目录）
		src := toPOSIXRoot(strings.TrimSpace(resolveMediaRootsFor(h.store, h.pv).SourcePOSIX))
		if src == "" {
			return "", ""
		}
		cr = m4CacheRootDefault(src)
	}
	return cr, localPathOf(cr)
}

// ResolveRoot 把 root 参数（src/proxy/dest）解析为本地绝对根路径（04 §2.6、§7 改动点索引）。
// 返回 (本地根, 错误码)；错误码非空表示 root 非法或对应根未配置。
//
// 2026-09-16 修复①：root 放宽支持「素材根本地绝对路径」写法——剪辑页可在**全部授权目录**间切换，
// 前端把当前素材根的绝对路径作为 root 下发，此处仅接受与授权目录/videoRoot 完全一致的路径
// （不接受任意路径、不接受子目录），从而在不改相对路径协议的前提下支援多根。
func (h *Handlers) ResolveRoot(root string) (string, string) {
	r := strings.TrimSpace(root)
	roots := h.resolveMediaRoots()
	switch r {
	case "", "src":
		if roots.SourceLocal == "" {
			return "", errCodeRootUnknown
		}
		return roots.SourceLocal, ""
	case "proxy":
		if roots.ProxyLocal == "" {
			return "", errCodeRootUnknown
		}
		return roots.ProxyLocal, ""
	case "dest":
		if roots.DestLocal == "" {
			return "", errCodeRootUnknown
		}
		return roots.DestLocal, ""
	}
	// 非枚举写法：按「素材根绝对路径」解析（必须命中授权集合）。
	if local, ok := h.matchAuthorizedSrcRoot(r); ok {
		return local, ""
	}
	// 修复①：亦接受「某授权素材根下的 _proxy 目录」，用于多根场景下代理产物的缩略图/预览。
	if local, ok := h.matchAuthorizedProxyRoot(r); ok {
		return local, ""
	}
	return "", errCodeRootUnknown
}

// matchAuthorizedProxyRoot 判断 p 是否为「某授权素材根下的 _proxy 目录」（修复①）。
// 多根场景下代理与素材同根（<素材根>/_proxy），前端据此直接以绝对路径请求代理缩略图。
func (h *Handlers) matchAuthorizedProxyRoot(p string) (string, bool) {
	want := strings.TrimSpace(p)
	if want == "" {
		return "", false
	}
	want = filepath.Clean(filepath.FromSlash(want))
	for _, cand := range h.srcRootCandidates() {
		dir := filepath.Join(filepath.Clean(filepath.FromSlash(cand)), "_proxy")
		if samePath(dir, want) {
			return dir, true
		}
	}
	return "", false
}

// IsAuthorizedSrcRoot 判断给定本地路径是否为授权素材根（供代理提交侧区分 src/proxy/dest）。
func (h *Handlers) IsAuthorizedSrcRoot(p string) bool {
	_, ok := h.matchAuthorizedSrcRoot(p)
	return ok
}

// ProxyLocalForSrcRoot 返回素材根 srcLocal 对应的本地代理根（修复①：代理与素材同根）：
// 缺省素材根 → 沿用 Settings.proxyRoot；其他授权根 → <该根>/_proxy。
func (h *Handlers) ProxyLocalForSrcRoot(srcLocal string) string {
	return proxyLocalForSrcRoot(h.store, h.pv, srcLocal)
}

// matchAuthorizedSrcRoot 判断 root 是否为本机授权的素材根（修复①），命中返回其本地绝对根。
// 仅接受与候选根**完全一致**的路径（Clean + 大小写归一后比较），禁止用前缀匹配放行子目录。
func (h *Handlers) matchAuthorizedSrcRoot(root string) (string, bool) {
	want := strings.TrimSpace(root)
	if want == "" {
		return "", false
	}
	want = filepath.Clean(filepath.FromSlash(want))
	wantReal := want
	if r, err := filepath.EvalSymlinks(want); err == nil {
		wantReal = r
	}
	for _, cand := range h.srcRootCandidates() {
		c := filepath.Clean(filepath.FromSlash(cand))
		if samePath(c, want) || samePath(c, wantReal) {
			return c, true
		}
		if cr, err := filepath.EvalSymlinks(c); err == nil && (samePath(cr, want) || samePath(cr, wantReal)) {
			return c, true
		}
	}
	return "", false
}

// srcRootCandidates 素材根候选集合（修复①）：Settings.videoRoot → 设置内授权目录 → 进程授权目录，
// 去重保序；用于剪辑页多根切换时的白名单校验（首个候选即缺省 src 根）。
func (h *Handlers) srcRootCandidates() []string {
	out := make([]string, 0, 8)
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		for _, e := range out {
			if samePath(e, p) {
				return
			}
		}
		out = append(out, p)
	}
	if h.store != nil {
		cfg := h.store.GetSettings()
		add(cfg.VideoRoot)
		for _, p := range cfg.AccessiblePaths {
			add(p)
		}
	}
	if h.pv != nil {
		for _, p := range h.pv.AccessPaths() {
			add(p)
		}
	}
	return out
}

// samePath 路径等价比较：Clean 后按平台大小写规则比较（Windows 不区分大小写）。
func samePath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}
