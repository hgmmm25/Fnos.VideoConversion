package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// PathValidator 路径安全校验器，按 FS.md 8.2 四层校验逻辑实现。
type PathValidator struct {
	accessPaths []string // 授权目录列表 (TRIM_DATA_ACCESSIBLE_PATHS)
	extraPaths  []string // 用户手动配置的额外授权目录（通过设置页配置）
	envFile     string   // config_callback 写入的授权目录文件路径
	devMode     bool     // 开发模式标志
}

// NewPathValidator 创建路径校验器。
// devMode 为 true 时（本地开发），若环境变量未设置则放行当前工作目录。
// 生产模式下若环境变量未设置，返回空列表，前端将提示未授权。
// envFile 为 config_callback 脚本写入的授权目录文件路径，用于运行时
// 动态读取 fnOS 最新授权目录（进程启动后环境变量不会变化，需通过文件传递）。
func NewPathValidator(devMode bool, envFile string) *PathValidator {
	pv := &PathValidator{devMode: devMode, envFile: envFile}
	pv.reload()
	return pv
}

// SetExtraPaths 设置用户手动配置的额外授权目录。
func (pv *PathValidator) SetExtraPaths(paths []string) {
	pv.extraPaths = pv.extraPaths[:0]
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				pv.extraPaths = append(pv.extraPaths, abs)
			} else {
				pv.extraPaths = append(pv.extraPaths, p)
			}
		}
	}
}

// allPaths 返回环境变量授权目录与手动配置目录的合并列表。
func (pv *PathValidator) allPaths() []string {
	result := make([]string, 0, len(pv.accessPaths)+len(pv.extraPaths))
	seen := map[string]bool{}
	for _, p := range pv.accessPaths {
		if !seen[p] {
			result = append(result, p)
			seen[p] = true
		}
	}
	for _, p := range pv.extraPaths {
		if !seen[p] {
			result = append(result, p)
			seen[p] = true
		}
	}
	return result
}

// reload 重新加载授权目录。
// 优先从 envFile（由 config_callback 写入）读取，以获取 fnOS 运行时
// 动态授权的最新目录；若文件不存在或为空，则回退到进程环境变量。
func (pv *PathValidator) reload() {
	pv.accessPaths = pv.accessPaths[:0]

	raw := ""
	// 优先从 config_callback 写入的文件读取（支持运行时动态授权）
	if pv.envFile != "" {
		if data, err := os.ReadFile(pv.envFile); err == nil {
			raw = strings.TrimSpace(string(data))
		}
	}
	// 回退到进程环境变量（启动时快照，不会动态变化）
	if raw == "" {
		raw = os.Getenv("TRIM_DATA_ACCESSIBLE_PATHS")
	}

	if raw == "" {
		if pv.devMode {
			// 开发模式下未设置，放行当前目录
			if cwd, err := os.Getwd(); err == nil {
				pv.accessPaths = []string{cwd}
			}
		}
		return
	}
	for _, p := range strings.FieldsFunc(raw, accessPathSplitter(raw)) {
		p = strings.TrimSpace(p)
		if p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				pv.accessPaths = append(pv.accessPaths, abs)
			} else {
				pv.accessPaths = append(pv.accessPaths, p)
			}
		}
	}
}

// windowsDriveRe 检测 Windows 盘符路径（如 C:\media、D:/videos）。
var windowsDriveRe = regexp.MustCompile(`(?i)(?:^|[\s,;])[a-z]:[\\/]`)

// accessPathSplitter 依据输入形态选择分割符（D-02 修复）：
// 授权目录在 POSIX（fnOS）下以 `:` 分隔；Windows 盘符路径本身含 `:`，
// 若仍按 `:` 切分会把 `C:\media` 拆成 `C` 与 `\media` 导致授权失效，
// 此时改用 , ; 与换行分隔。
func accessPathSplitter(raw string) func(rune) bool {
	if windowsDriveRe.MatchString(raw) {
		return func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' }
	}
	return func(r rune) bool { return r == ',' || r == ':' || r == '\n' || r == '\r' }
}

// Validate 完整四层路径安全校验：
//  1. Clean 标准化路径（URL 解码由 gin 框架完成，避免双重解码）
//  2. 拦截显性 .. 路径
//  3. EvalSymlinks 解析软链接获取真实路径
//  4. 强制校验真实路径前缀属于授权目录
func (pv *PathValidator) Validate(path string) error {
	// 每次校验前重新读取环境变量，支持 fnOS 运行时动态授权
	pv.reload()

	// 1. 标准化路径（gin c.Query 已完成 URL 解码，此处不再重复解码）
	clean := filepath.Clean(path)

	// 2. 拦截显性 .. 段（段级判定，避免误杀 a..b.mp4 这类合法文件名）
	if hasDotDotSegment(clean) {
		return fmt.Errorf("非法路径: 包含 .. 穿越字符")
	}

	// 3. 解析软链接获取真实物理路径
	realPath, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// 文件可能尚不存在（如输出路径），尝试对父目录解析
		parent := filepath.Dir(clean)
		realParent, perr := filepath.EvalSymlinks(parent)
		if perr != nil {
			return fmt.Errorf("路径解析失败: %w", err)
		}
		realPath = filepath.Join(realParent, filepath.Base(clean))
	}

	// 4. 校验真实路径前缀属于授权目录
	allPaths := pv.allPaths()
	if len(allPaths) == 0 {
		// 生产模式下未配置授权目录，拒绝访问（防止越权）
		// 开发模式下 NewPathValidator 已回退到当前工作目录，不会走到这里
		return fmt.Errorf("未配置授权目录，请在 fnOS 应用设置中为此应用授权共享文件夹")
	}
	for _, allow := range allPaths {
		// 前缀必须带分隔符边界，避免 /media/videos2 被误判为 /media/videos 的子路径；
		// 授权目录同样解析真实路径，保证与 realPath 在同一物理视图下比较。
		ra := allow
		if resolved, err := filepath.EvalSymlinks(allow); err == nil {
			ra = resolved
		}
		if edlWithinRoot(ra, realPath) {
			return nil
		}
	}
	return fmt.Errorf("路径超出授权访问范围: %s", realPath)
}

// hasDotDotSegment 判定路径是否含显性 ".." 段（D-02：段级判定，替代裸 strings.Contains，
// 避免把 a..b.mp4 这类合法文件名误判为穿越）。
func hasDotDotSegment(p string) bool {
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

// hasAuthorizedRootFor 判定 root 是否落在某个授权目录内（本地物理视图，含符号链接解析）。
// 渲染提交侧据此决定是否启用 D-02 的严格 L4 落地校验：素材根未纳入授权目录时
// （如 NAS 侧尚未下发 config_callback）回退轻量前缀判定，避免把整个素材根误判为越权。
func (pv *PathValidator) hasAuthorizedRootFor(root string) bool {
	if pv == nil {
		return false
	}
	abs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return false
	}
	realRoot := abs
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		realRoot = r
	}
	for _, allow := range pv.allPaths() {
		ra := filepath.Clean(allow)
		if r, err := filepath.EvalSymlinks(allow); err == nil {
			ra = r
		}
		if edlWithinRoot(ra, realRoot) {
			return true
		}
	}
	return false
}

// ValidateRel 相对路径四层校验（07 §3.2，D-02）：授权根下的素材/输出相对路径。
//
//	L1~L3 复用 edl_validate.go 的 validateRelPath（与 FVCS 侧同规则）；
//	L4   校验根目录落在授权目录内，落地后 EvalSymlinks 并做前缀（带边界）校验。
//
// allowExt 为 nil 时不校验扩展名。返回解析后的绝对物理路径，失败返回结构化错误。
func (pv *PathValidator) ValidateRel(root, rel string, allowExt map[string]bool) (string, *edlValidationError) {
	pv.reload()

	// L1~L3
	if e := validateRelPath(rel, allowExt); e != nil {
		return "", e
	}

	rootAbs := strings.TrimSpace(root)
	if rootAbs == "" {
		return "", newEDLValidationErr(errCodeEDLInvalid, "root", "根目录未配置", -1)
	}
	if abs, err := filepath.Abs(rootAbs); err == nil {
		rootAbs = abs
	}

	allPaths := pv.allPaths()
	if len(allPaths) == 0 {
		return "", newEDLValidationErr(errCodeAssetNotInRoot, "root",
			"未配置授权目录，请在 fnOS 应用设置中为此应用授权共享文件夹", -1)
	}

	// 根目录本身必须在授权范围内（防止把 rel 校验当成越权跳板）
	realRoot, rerr := filepath.EvalSymlinks(rootAbs)
	if rerr != nil {
		return "", newEDLValidationErr(errCodeAssetMissing, "root", "根目录不可访问: "+rerr.Error(), -1)
	}
	authorized := false
	for _, allow := range allPaths {
		ra := filepath.Clean(allow)
		if resolved, err := filepath.EvalSymlinks(allow); err == nil {
			ra = resolved
		}
		if edlWithinRoot(ra, realRoot) {
			authorized = true
			break
		}
	}
	if !authorized {
		return "", newEDLValidationErr(errCodeAssetNotInRoot, "root", "根目录不在授权访问范围内", -1)
	}

	// L4
	return edlResolveRelPath(realRoot, rel)
}

// AccessPaths 返回授权目录列表（供前端展示）。
// 每次调用时重新读取环境变量，支持 fnOS 运行时动态授权。
func (pv *PathValidator) AccessPaths() []string {
	pv.reload()
	return pv.allPaths()
}

// ===== M4：预览网关三根解析（04 §2.4、§2.6）=====

// errCodeRootUnknown 非法/未配置的 root 参数（04 §6；属编码缺陷，服务端按 ERROR 记录）。
const errCodeRootUnknown = "E_ROOT_UNKNOWN"

// mediaRoots 预览网关三根（src/proxy/dest）的 POSIX 契约形态与本地物理路径（04 §2.6）。
type mediaRoots struct {
	SourcePOSIX string
	ProxyPOSIX  string
	DestPOSIX   string
	SourceLocal string
	ProxyLocal  string
	DestLocal   string
}

// m4ProxyRootDefault / m4CacheRootDefault 代理根与缩略图缓存根的默认推导（04 §2.4、§2.6）。
// 决策②：Settings 未显式配置时按 videoRoot 推导，不引入额外持久化。
func m4ProxyRootDefault(videoRoot string) string {
	return strings.TrimRight(strings.TrimSpace(videoRoot), "/") + "/_proxy"
}

func m4CacheRootDefault(videoRoot string) string {
	return strings.TrimRight(strings.TrimSpace(videoRoot), "/") + "/_wve_cache"
}

// localPathOf POSIX 契约路径 → 本地物理路径；空串原样返回（未配置）。
func localPathOf(posixPath string) string {
	p := strings.TrimSpace(posixPath)
	if p == "" {
		return ""
	}
	return filepath.FromSlash(p)
}

// resolveMediaRoots 解析 src/proxy/dest 三根（04 §2.6）：
//   - src/dest 复用 resolveEDLRoots 的既有缺省回落（授权目录首个 / <videoRoot>/_exports）；
//   - proxy 取 Settings.proxyRoot，为空时回落 <videoRoot>/_proxy。
func (h *Handlers) resolveMediaRoots() mediaRoots {
	return resolveMediaRootsFor(h.store, h.pv)
}

// resolveMediaRootsFor 包级三根解析：与 (h *Handlers) resolveMediaRoots 同规则，
// 供无 Handlers 上下文的调用点（如调度器侧的代理路径推导，proxy_flow.go）复用。
func resolveMediaRootsFor(store *Store, pv *PathValidator) mediaRoots {
	var cfg Settings
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
		proxy = m4ProxyRootDefault(src)
	}
	return mediaRoots{
		SourcePOSIX: strings.TrimSpace(src),
		ProxyPOSIX:  proxy,
		DestPOSIX:   strings.TrimSpace(dst),
		SourceLocal: localPathOf(src),
		ProxyLocal:  localPathOf(proxy),
		DestLocal:   localPathOf(dst),
	}
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

// proxyLocalForSrcRoot 包级实现：提交侧（handlers_proxy.go）与校验侧（proxy_flow.go）共用同一口径，
// 避免「提交时按根 B 写、校验时按 videoRoot 读」的错位。
func proxyLocalForSrcRoot(store *Store, pv *PathValidator, srcLocal string) string {
	roots := resolveMediaRootsFor(store, pv)
	if strings.TrimSpace(srcLocal) == "" {
		return roots.ProxyLocal
	}
	if samePath(srcLocal, roots.SourceLocal) {
		return roots.ProxyLocal
	}
	return filepath.Join(filepath.FromSlash(srcLocal), "_proxy")
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
