package protocol

// EDL 路径/输出名/载荷体积 白名单校验（对应设计文档 07 §3.2、§3.5、§3.6，D-01）
//
// 与 FVCC 侧 FVCC/server/edl_validate.go 为同规则双实现（07 §3.1 纵深防御：两侧不共享代码，
// 规则以 07 文档为唯一来源，靠同一组测试向量 testdata/edl_vectors.json 保证一致）。
//
// 分层（07 §3.2）：
//
//	L1 语法：非空、长度 ≤255、无 NUL/控制字符、无 `\`、不以 `/` 开头
//	L2 结构：按 `/` 切分后每段非空且非 `.`/`..`、段数 ≤8、单段 rune ≤100
//	L3 语义：path.Clean 后与原文相等、扩展名 ∈ 白名单
//	L4 落地：拼根后 Clean → EvalSymlinks → 结果以根为前缀（带分隔符边界）

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// MaxRelPathLen 相对路径总长度上限（07 §3.2 L1）
	MaxRelPathLen = 255
	// MaxRelSegments 相对路径层级上限（07 §3.2 L2）
	MaxRelSegments = 8
	// MaxRelSegRunes 单段 rune 上限（07 §3.2 L2）
	MaxRelSegRunes = 100
	// MaxJSONDepth JSON 嵌套深度上限（07 §3.6）
	MaxJSONDepth = 8
	// MaxJSONStrRunes JSON 单字符串 rune 上限（07 §3.6）
	MaxJSONStrRunes = 255
)

// AllowedSourceExt 素材扩展名白名单（带点、小写；单一数据源派生自 allowedSourceExt，
// 避免 FVCS 内部两套扩展名表漂移）
var AllowedSourceExt = func() map[string]bool {
	m := make(map[string]bool, len(allowedSourceExt))
	for k := range allowedSourceExt {
		m["."+k] = true
	}
	return m
}()

// reOutputBase 输出主名允许字符集（07 §3.5）
var reOutputBase = regexp.MustCompile(`^[\p{Han}\p{L}\p{N} ._\-（）()\[\]]{1,80}$`)

// reReservedDeviceName Windows 保留设备名（大小写不敏感，07 §3.5）
var reReservedDeviceName = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`)

// ============================================================
// L1~L3：相对路径校验（07 §3.2）
// ============================================================

// ValidateRelPath 校验相对路径（协议统一 `/` 分隔）。
// allowExt 为 nil 时不做扩展名要求（如 sourceRoot/destRoot 这类根路径）。
func ValidateRelPath(rel string, allowExt map[string]bool) *PayloadError {
	// ---- L1 语法 ----
	if rel == "" {
		return newPayloadErr(ErrCodeEDLInvalid, "path", "路径不得为空", -1)
	}
	if len(rel) > MaxRelPathLen {
		return newPayloadErr(ErrCodeEDLInvalid, "path", "路径长度超过 255", -1)
	}
	if strings.HasPrefix(rel, "/") {
		return newPayloadErr(ErrCodeEDLInvalid, "path", "禁止绝对路径（不得以 / 开头）", -1)
	}
	if strings.Contains(rel, `\`) {
		return newPayloadErr(ErrCodeEDLInvalid, "path", "协议统一使用 / 分隔，禁止反斜杠", -1)
	}
	for i := 0; i < len(rel); i++ {
		if rel[i] < 0x20 || rel[i] == 0x7f {
			return newPayloadErr(ErrCodeEDLInvalid, "path", "含控制字符", -1)
		}
	}

	// ---- L2 结构 ----
	segs := strings.Split(rel, "/")
	if len(segs) > MaxRelSegments {
		return newPayloadErr(ErrCodeAssetNotInRoot, "path",
			"路径层级超过 8 段，疑似越权扫描", -1)
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return newPayloadErr(ErrCodeAssetNotInRoot, "path", "含非法路径段 "+s, -1)
		}
		if utf8.RuneCountInString(s) > MaxRelSegRunes {
			return newPayloadErr(ErrCodeEDLInvalid, "path", "单段长度超过 100", -1)
		}
		for _, r := range s {
			if r < 0x20 || r == 0x7f || strings.ContainsRune(`:*?"<>|`, r) {
				return newPayloadErr(ErrCodeEDLInvalid, "path", "含非法字符 "+string(r), -1)
			}
		}
	}

	// ---- L3 语义 ----
	if path.Clean(rel) != rel {
		return newPayloadErr(ErrCodeEDLInvalid, "path", "路径未规范化（含 // 或 ./）", -1)
	}
	if allowExt != nil {
		ext := strings.ToLower(path.Ext(rel))
		if !allowExt[ext] {
			return newPayloadErr(ErrCodeEDLInvalid, "path", "不支持的扩展名 "+ext, -1)
		}
	}
	return nil
}

// ValidateRelPathDecoded 先 URL 解码再校验（07 §8 S3：纵深防御，防 %2e%2e 类编码穿越）。
// 正常链路上 gin/前端已完成一次解码，此处为二次兜底，重复解码不影响已解码的合法路径。
func ValidateRelPathDecoded(rel string, allowExt map[string]bool) *PayloadError {
	decoded, err := url.PathUnescape(rel)
	if err != nil {
		return newPayloadErr(ErrCodeEDLInvalid, "path", "URL 解码失败", -1)
	}
	return ValidateRelPath(decoded, allowExt)
}

// ============================================================
// L4：落地路径校验（07 §3.2）
// ============================================================

// withinRoot 前缀校验必须带分隔符边界（防 /media/videos2 被误判为 /media/videos 子路径）
func withinRoot(root, abs string) bool {
	r := filepath.Clean(root)
	return abs == r || strings.HasPrefix(abs, r+string(os.PathSeparator))
}

// ResolveRelPath 把相对路径解析为根下的绝对物理路径，并完成 L4 校验。
// 文件不存在时对父目录求 EvalSymlinks（输出/代理目录场景）。
func ResolveRelPath(root, rel string) (string, *PayloadError) {
	if strings.TrimSpace(root) == "" {
		return "", newPayloadErr(ErrCodeEDLInvalid, "root", "根目录未配置", -1)
	}
	if e := ValidateRelPath(rel, nil); e != nil {
		return "", e
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		rootAbs = filepath.Clean(root)
	}
	joined := filepath.Clean(filepath.Join(rootAbs, filepath.FromSlash(rel)))

	real := joined
	if r, err := filepath.EvalSymlinks(joined); err == nil {
		real = r
	} else {
		parent := filepath.Dir(joined)
		rp, perr := filepath.EvalSymlinks(parent)
		if perr != nil {
			return "", newPayloadErr(ErrCodeAssetMissing, "path", "路径解析失败: "+perr.Error(), -1)
		}
		real = filepath.Join(rp, filepath.Base(joined))
	}

	realRoot := rootAbs
	if rr, err := filepath.EvalSymlinks(rootAbs); err == nil {
		realRoot = rr
	}
	if !withinRoot(realRoot, real) {
		return "", newPayloadErr(ErrCodeAssetNotInRoot, "path", "解析后的物理路径落在根目录之外", -1)
	}
	return real, nil
}

// ============================================================
// 输出文件名校验（07 §3.5）
// ============================================================

// validateOutputNameStrict 输出文件名白名单：单段 + 强制 .mp4 + 禁多重扩展名 + 拒保留设备名。
func validateOutputNameStrict(name string) *PayloadError {
	const field = "output"
	if name == "" {
		return newPayloadErr(ErrCodeEDLInvalid, field, "输出名不得为空", -1)
	}
	if strings.ContainsAny(name, `/\`) {
		return newPayloadErr(ErrCodeEDLInvalid, field, "不得含路径分隔符（P0 不允许子目录）", -1)
	}
	if len(name) > MaxRelPathLen {
		return newPayloadErr(ErrCodeEDLInvalid, field, "输出名长度超过 255", -1)
	}
	for i := 0; i < len(name); i++ {
		if name[i] < 0x20 || name[i] == 0x7f {
			return newPayloadErr(ErrCodeEDLInvalid, field, "含控制字符", -1)
		}
	}
	if !strings.HasSuffix(strings.ToLower(name), ".mp4") {
		return newPayloadErr(ErrCodeEDLInvalid, field, "P0 输出扩展名强制 .mp4", -1)
	}
	if strings.Count(name, ".") != 1 {
		return newPayloadErr(ErrCodeEDLInvalid, field, "禁止多重扩展名（如 a.b.mp4）", -1)
	}
	base := name[:len(name)-len(".mp4")]
	if strings.TrimSpace(base) == "" {
		return newPayloadErr(ErrCodeEDLInvalid, field, "输出主名不得为空白", -1)
	}
	if !reOutputBase.MatchString(base) {
		return newPayloadErr(ErrCodeEDLInvalid, field, "输出主名含不允许的字符或超长（≤80）", -1)
	}
	if reReservedDeviceName.MatchString(strings.TrimSpace(base)) {
		return newPayloadErr(ErrCodeEDLInvalid, field, "命中 Windows 保留设备名", -1)
	}
	return nil
}

// ValidateRootRelPath 根路径（sourceRoot / destRoot / proxyRoot）校验（07 §3.2）：
// 空值放行（表示沿用服务端默认根），非空按同一套 L1~L3 规则校验、不做扩展名要求。
func ValidateRootRelPath(root, field string) *PayloadError {
	if root == "" {
		return nil
	}
	if e := ValidateRelPath(root, nil); e != nil {
		e.Field = field
		return e
	}
	return nil
}

// ============================================================
// JSON 体积 / 深度 / 字符串长度约束（07 §3.6）
// ============================================================

// ValidatePayloadLimits 载荷解析前的轻量扫描（不引入新依赖）：
// 体积 ≤256KB、嵌套深度 ≤8、单字符串 ≤255 字符。
func ValidatePayloadLimits(raw []byte) *PayloadError {
	if len(raw) > MaxEDLPayloadLen {
		return newPayloadErr(ErrCodeEDLTooLarge, "", "payload 超过 256KB", -1)
	}

	depth := 0
	inStr := false
	esc := false
	strRunes := 0

	for i := 0; i < len(raw); i++ {
		b := raw[i]

		if inStr {
			if esc {
				esc = false
				strRunes++
			} else {
				switch {
				case b == '\\':
					esc = true
				case b == '"':
					inStr = false
					strRunes = 0
				default:
					_, size := utf8.DecodeRune(raw[i:])
					if size > 1 {
						i += size - 1
					}
					strRunes++
				}
			}
			if strRunes > MaxJSONStrRunes {
				return newPayloadErr(ErrCodePayloadInvalid, "", "JSON 字符串长度超过 255", -1)
			}
			continue
		}

		switch b {
		case '"':
			inStr = true
			strRunes = 0
		case '{', '[':
			depth++
			if depth > MaxJSONDepth {
				return newPayloadErr(ErrCodePayloadInvalid, "", "JSON 嵌套深度超过 8", -1)
			}
		case '}', ']':
			depth--
		}
	}

	if inStr || depth != 0 {
		return newPayloadErr(ErrCodePayloadInvalid, "", "JSON 结构不完整", -1)
	}
	return nil
}

// ============================================================
// 时间码 / 数值辅助（07 §3.3 红线 4）
// ============================================================

// TimecodeToMs "HH:MM:SS.mmm" → 毫秒；格式非法返回错误（等价于 07 §3.3 的 timecodeToMs + 正则前置）
func TimecodeToMs(tc string) (int64, *PayloadError) {
	if !timecodeRe.MatchString(tc) {
		return 0, newPayloadErr(ErrCodeEDLInvalid, "timecode", "时间码格式非法", -1)
	}
	ms, err := ParseTimecode(tc)
	if err != nil {
		return 0, newPayloadErr(ErrCodeEDLInvalid, "timecode", "时间码解析失败", -1)
	}
	return ms, nil
}
