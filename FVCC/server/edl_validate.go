package main

// D-02：FVCC 侧 EDL 路径 / 输出名 / 载荷白名单校验（同规则双实现）
//
// 规则唯一来源：WebVideoEditor_Design/07-安全校验与凭据管理细则.md §3.2、§3.5、§3.6。
// 与 FVCS/pkg/protocol/edl_validate.go 为「同规则双实现」（07 §3.1 纵深防御：两侧不共享代码），
// 一致性由共用测试向量 FVCS/pkg/protocol/testdata/edl_vectors.json 保证；两侧均不得各自修改向量。
//
// 分层（07 §3.2）：
//
//	L1 语法：非空、长度 ≤255、无 NUL/控制字符、无 `\`、不以 `/` 开头
//	L2 结构：按 `/` 切分后每段非空且非 `.`/`..`、段数 ≤8、单段 rune ≤100、段内无 `:*?"<>|`
//	L3 语义：path.Clean 后与原文相等、扩展名 ∈ 白名单
//	L4 落地：拼根后 Clean → EvalSymlinks → 结果以根为前缀（带分隔符边界）
//
// 与 handlers_edl.go 的契约级校验（validateProjectInput）分工：L1~L3/L4 与扩展名白名单
// 以本文件为唯一实现（handlers_edl.go 的 validateClipFile 已改为委托本文件），
// handlers_edl.go 只保留项目级语义（clipId/assetId/片段时长关系等）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// ===== 约束（07 §3.2 / §3.6）=====

const (
	// edlMaxRelPathLen 相对路径总长度上限（07 §3.2 L1）
	edlMaxRelPathLen = 255
	// edlMaxRelSegments 相对路径层级上限（07 §3.2 L2）
	edlMaxRelSegments = 8
	// edlMaxRelSegRunes 单段 rune 上限（07 §3.2 L2）
	edlMaxRelSegRunes = 100
	// edlMaxJSONDepth JSON 嵌套深度上限（07 §3.6）
	edlMaxJSONDepth = 8
	// edlMaxJSONStrRunes JSON 单字符串 rune 上限（07 §3.6）
	edlMaxJSONStrRunes = 255

	// errCodePayloadInvalid 载荷结构非法（03 §5.3；此前仅以字面量出现，此处收敛为常量）
	errCodePayloadInvalid = "E_PAYLOAD_INVALID"
)

// edlAllowedSourceExt 素材扩展名白名单（带点、小写；07 §3.2 L3）
var edlAllowedSourceExt = map[string]bool{
	".mp4": true, ".mov": true, ".mkv": true, ".m4v": true, ".avi": true, ".mxf": true,
}

// edlReOutputBase 输出主名允许字符集（07 §3.5，与 FVCS 侧逐字符一致）
var edlReOutputBase = regexp.MustCompile(`^[\p{Han}\p{L}\p{N} ._\-（）()\[\]]{1,80}$`)

// edlReReservedDeviceName Windows 保留设备名（大小写不敏感，07 §3.5）
var edlReReservedDeviceName = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`)

// ===== 结构化错误 =====

// edlValidationError 结构化校验错误：必须可定位 field / index（03 §5.1）
type edlValidationError struct {
	Code  string
	Msg   string
	Field string
	Index int
}

func (e *edlValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (field=%s, index=%d)", e.Code, e.Msg, e.Field, e.Index)
	}
	return fmt.Sprintf("%s: %s (index=%d)", e.Code, e.Msg, e.Index)
}

func newEDLValidationErr(code, field, msg string, index int) *edlValidationError {
	return &edlValidationError{Code: code, Msg: msg, Field: field, Index: index}
}

// ============================================================
// L1~L3：相对路径校验（07 §3.2）
// ============================================================

// validateRelPath 校验相对路径（协议统一 `/` 分隔）。
// allowExt 为 nil 时不做扩展名要求（如 sourceRoot/destRoot 这类根路径）。
func validateRelPath(rel string, allowExt map[string]bool) *edlValidationError {
	// ---- L1 语法 ----
	if rel == "" {
		return newEDLValidationErr(errCodeEDLInvalid, "path", "路径不得为空", -1)
	}
	if len(rel) > edlMaxRelPathLen {
		return newEDLValidationErr(errCodeEDLInvalid, "path", "路径长度超过 255", -1)
	}
	if strings.HasPrefix(rel, "/") {
		return newEDLValidationErr(errCodeEDLInvalid, "path", "禁止绝对路径（不得以 / 开头）", -1)
	}
	if strings.Contains(rel, `\`) {
		return newEDLValidationErr(errCodeEDLInvalid, "path", "协议统一使用 / 分隔，禁止反斜杠", -1)
	}
	for i := 0; i < len(rel); i++ {
		if rel[i] < 0x20 || rel[i] == 0x7f {
			return newEDLValidationErr(errCodeEDLInvalid, "path", "含控制字符", -1)
		}
	}

	// ---- L2 结构 ----
	segs := strings.Split(rel, "/")
	if len(segs) > edlMaxRelSegments {
		return newEDLValidationErr(errCodeAssetNotInRoot, "path",
			"路径层级超过 8 段，疑似越权扫描", -1)
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return newEDLValidationErr(errCodeAssetNotInRoot, "path", "含非法路径段 "+s, -1)
		}
		if utf8.RuneCountInString(s) > edlMaxRelSegRunes {
			return newEDLValidationErr(errCodeEDLInvalid, "path", "单段长度超过 100", -1)
		}
		for _, r := range s {
			if r < 0x20 || r == 0x7f || strings.ContainsRune(`:*?"<>|`, r) {
				return newEDLValidationErr(errCodeEDLInvalid, "path", "含非法字符 "+string(r), -1)
			}
		}
	}

	// ---- L3 语义 ----
	if path.Clean(rel) != rel {
		return newEDLValidationErr(errCodeEDLInvalid, "path", "路径未规范化（含 // 或 ./）", -1)
	}
	if allowExt != nil {
		ext := strings.ToLower(path.Ext(rel))
		if !allowExt[ext] {
			return newEDLValidationErr(errCodeEDLInvalid, "path", "不支持的扩展名 "+ext, -1)
		}
	}
	return nil
}

// validateRelPathDecoded 先 URL 解码再校验（07 §8 S3：纵深防御，防 %2e%2e 类编码穿越）。
func validateRelPathDecoded(rel string, allowExt map[string]bool) *edlValidationError {
	decoded, err := url.PathUnescape(rel)
	if err != nil {
		return newEDLValidationErr(errCodeEDLInvalid, "path", "URL 解码失败", -1)
	}
	return validateRelPath(decoded, allowExt)
}

// edlWithinRoot 前缀校验必须带分隔符边界（防 /media/videos2 被误判为 /media/videos 子路径）。
func edlWithinRoot(root, abs string) bool {
	r := filepath.Clean(root)
	return abs == r || strings.HasPrefix(abs, r+string(os.PathSeparator))
}

// edlResolveRelPath 把相对路径解析为根下的绝对物理路径，并完成 L4 校验（07 §3.2）。
// 文件不存在时对父目录求 EvalSymlinks（输出/代理目录场景）。
func edlResolveRelPath(root, rel string) (string, *edlValidationError) {
	if strings.TrimSpace(root) == "" {
		return "", newEDLValidationErr(errCodeEDLInvalid, "root", "根目录未配置", -1)
	}
	if e := validateRelPath(rel, nil); e != nil {
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
			return "", newEDLValidationErr(errCodeAssetMissing, "path", "路径解析失败: "+perr.Error(), -1)
		}
		real = filepath.Join(rp, filepath.Base(joined))
	}

	realRoot := rootAbs
	if rr, err := filepath.EvalSymlinks(rootAbs); err == nil {
		realRoot = rr
	}
	if !edlWithinRoot(realRoot, real) {
		return "", newEDLValidationErr(errCodeAssetNotInRoot, "path", "解析后的物理路径落在根目录之外", -1)
	}
	return real, nil
}

// ============================================================
// 输出文件名校验（07 §3.5）
// ============================================================

// validateOutputNameStrict 输出文件名白名单：单段 + 强制 .mp4 + 禁多重扩展名 + 拒 Windows 保留设备名。
func validateOutputNameStrict(name string) *edlValidationError {
	const field = "output"
	if name == "" {
		return newEDLValidationErr(errCodeEDLInvalid, field, "输出名不得为空", -1)
	}
	if strings.ContainsAny(name, `/\`) {
		return newEDLValidationErr(errCodeEDLInvalid, field, "不得含路径分隔符（P0 不允许子目录）", -1)
	}
	if len(name) > edlMaxRelPathLen {
		return newEDLValidationErr(errCodeEDLInvalid, field, "输出名长度超过 255", -1)
	}
	for i := 0; i < len(name); i++ {
		if name[i] < 0x20 || name[i] == 0x7f {
			return newEDLValidationErr(errCodeEDLInvalid, field, "含控制字符", -1)
		}
	}
	if !strings.HasSuffix(strings.ToLower(name), renderOutputExt) {
		return newEDLValidationErr(errCodeEDLInvalid, field, "P0 输出扩展名强制 .mp4", -1)
	}
	if strings.Count(name, ".") != 1 {
		return newEDLValidationErr(errCodeEDLInvalid, field, "禁止多重扩展名（如 a.b.mp4）", -1)
	}
	base := name[:len(name)-len(renderOutputExt)]
	if strings.TrimSpace(base) == "" {
		return newEDLValidationErr(errCodeEDLInvalid, field, "输出主名不得为空白", -1)
	}
	if !edlReOutputBase.MatchString(base) {
		return newEDLValidationErr(errCodeEDLInvalid, field, "输出主名含不允许的字符或超长（≤80）", -1)
	}
	if edlReReservedDeviceName.MatchString(strings.TrimSpace(base)) {
		return newEDLValidationErr(errCodeEDLInvalid, field, "命中 Windows 保留设备名", -1)
	}
	return nil
}

// validateRootRelPath 根路径（sourceRoot / destRoot / proxyRoot）校验（07 §3.2）：
// 空值放行（表示沿用服务端默认根），非空按同一套 L1~L3 规则校验、不做扩展名要求。
func validateRootRelPath(root, field string) *edlValidationError {
	if root == "" {
		return nil
	}
	if e := validateRelPath(root, nil); e != nil {
		e.Field = field
		return e
	}
	return nil
}

// ============================================================
// JSON 体积 / 深度 / 字符串长度约束（07 §3.6）
// ============================================================

// validatePayloadLimits 载荷解析前的轻量扫描（不引入新依赖）：
// 体积 ≤256KB、嵌套深度 ≤8、单字符串 ≤255 字符、结构完整。
func validatePayloadLimits(raw []byte) *edlValidationError {
	if len(raw) == 0 {
		return newEDLValidationErr(errCodePayloadInvalid, "", "载荷为空", -1)
	}
	if len(raw) > edlMaxBodyBytes {
		return newEDLValidationErr(errCodeEDLTooLarge, "", "payload 超过 256KB", -1)
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
			if strRunes > edlMaxJSONStrRunes {
				return newEDLValidationErr(errCodePayloadInvalid, "", "JSON 字符串长度超过 255", -1)
			}
			continue
		}

		switch b {
		case '"':
			inStr = true
			strRunes = 0
		case '{', '[':
			depth++
			if depth > edlMaxJSONDepth {
				return newEDLValidationErr(errCodePayloadInvalid, "", "JSON 嵌套深度超过 8", -1)
			}
		case '}', ']':
			depth--
		}
	}

	if inStr || depth != 0 {
		return newEDLValidationErr(errCodePayloadInvalid, "", "JSON 结构不完整", -1)
	}
	return nil
}

// ============================================================
// 渲染载荷：严格解码 + 白名单校验（03 §5.2 / 07 §3.3）
// ============================================================

// decodeRenderPayloadStrict 严格解码：未知字段即拒绝（S6 filters / S12 凭据字段均由
// DisallowUnknownFields 拦截，RenderTaskPayload 不声明任何 filters/secret 字段）。
func decodeRenderPayloadStrict(raw json.RawMessage) (*RenderTaskPayload, *edlValidationError) {
	if len(raw) == 0 {
		return nil, newEDLValidationErr(errCodePayloadInvalid, "", "payload 缺失", -1)
	}
	if e := validatePayloadLimits(raw); e != nil {
		return nil, e
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var p RenderTaskPayload
	if err := dec.Decode(&p); err != nil {
		return nil, newEDLValidationErr(errCodePayloadInvalid, "", "载荷解析失败: "+err.Error(), -1)
	}
	return &p, nil
}

// validateRenderTimeline 时间线数值校验（03 §5.2；不做缺省回填，与 FVCS 侧一致）
func validateRenderTimeline(t *Timeline) *edlValidationError {
	if t.FPS < 1 || t.FPS > 120 {
		return newEDLValidationErr(errCodeEDLInvalid, "timeline.fps", "帧率必须在 1..120", -1)
	}
	if t.Width < 16 || t.Width > 7680 || t.Height < 16 || t.Height > 7680 {
		return newEDLValidationErr(errCodeEDLInvalid, "timeline.width/height", "分辨率必须在 16..7680", -1)
	}
	if t.Width%2 != 0 || t.Height%2 != 0 {
		return newEDLValidationErr(errCodeEDLInvalid, "timeline.width/height", "宽高必须为偶数", -1)
	}
	if t.Audio && !fvcsSampleRates[t.SampleRate] {
		return newEDLValidationErr(errCodeEDLInvalid, "timeline.sampleRate", "采样率不在允许集合", -1)
	}
	return nil
}

// validateRenderProfile 渲染方案校验（03 §5.2 + P0 预留字段拒绝）。
// presetKey 命中 12 项枚举表即通过（proxy_* 的用途限制属 handler 闸门，见 handlers_render.go）。
func validateRenderProfile(p *RenderProfile) *edlValidationError {
	if _, ok := edlRenderPresetTable[p.PresetKey]; !ok {
		return newEDLValidationErr(errCodeProfileInvalid, "profile.presetKey", "presetKey 不在服务端枚举表", -1)
	}
	if p.Container != renderContainerMP4 {
		return newEDLValidationErr(errCodeProfileInvalid, "profile.container", "P0 容器固定 mp4", -1)
	}
	if p.Video.Bitrate != "" {
		return newEDLValidationErr(errCodeProfileInvalid, "profile.video.bitrate", "P1+ 预留字段，P0 拒绝", -1)
	}
	if p.Audio.Codec != renderAudioCodecAAC {
		return newEDLValidationErr(errCodeProfileInvalid, "profile.audio.codec", "P0 音频编码固定 aac", -1)
	}
	if p.Audio.Channels < 1 || p.Audio.Channels > 8 {
		return newEDLValidationErr(errCodeProfileInvalid, "profile.audio.channels", "声道数必须在 1..8", -1)
	}
	if !fvcsSampleRates[p.Audio.SampleRate] {
		return newEDLValidationErr(errCodeProfileInvalid, "profile.audio.sampleRate", "采样率不在允许集合", -1)
	}
	return nil
}

// validateRenderEDLPayload 载荷白名单校验（FVCC 侧闸门；与 FVCS ValidateRenderEDLPayload 同规则）。
// 校验通过时会就地补全 TotalMs（若为 0），并把缺省 speed（0）回填为 1.0。
func validateRenderEDLPayload(p *RenderTaskPayload) *edlValidationError {
	if p == nil {
		return newEDLValidationErr(errCodePayloadInvalid, "", "载荷为空", -1)
	}
	if p.Type != renderPayloadType {
		return newEDLValidationErr(errCodePayloadInvalid, "type", "type 必须为 RenderEDL", -1)
	}
	// 三根相对路径白名单（07 §3.2）：空值表示沿用服务端默认根
	if e := validateRootRelPath(p.SourceRoot, "sourceRoot"); e != nil {
		return e
	}
	if e := validateRootRelPath(p.DestRoot, "destRoot"); e != nil {
		return e
	}
	if e := validateRenderTimeline(&p.Timeline); e != nil {
		return e
	}
	if e := validateOutputNameStrict(p.Output); e != nil {
		return e
	}
	if e := validateRenderProfile(&p.Profile); e != nil {
		return e
	}

	n := len(p.Clips)
	if n < 1 || n > edlMaxClips {
		return newEDLValidationErr(errCodeEDLInvalid, "clips",
			fmt.Sprintf("片段数必须在 1..%d，实际 %d", edlMaxClips, n), -1)
	}

	var total int64
	for i := range p.Clips {
		c := &p.Clips[i]
		if e := validateRelPath(c.File, edlAllowedSourceExt); e != nil {
			e.Field = "file"
			e.Index = i
			return e
		}
		inMs, err := parseTimecode(c.In)
		if err != nil {
			return newEDLValidationErr(errCodeEDLInvalid, "in", err.Error(), i)
		}
		outMs, err := parseTimecode(c.Out)
		if err != nil {
			return newEDLValidationErr(errCodeEDLInvalid, "out", err.Error(), i)
		}
		if outMs <= inMs {
			return newEDLValidationErr(errCodeEDLInvalid, "out", "out 必须晚于 in", i)
		}
		if outMs-inMs < edlMinClipMs {
			return newEDLValidationErr(errCodeEDLInvalid, "out",
				fmt.Sprintf("单片段时长不得小于 %dms", edlMinClipMs), i)
		}
		if c.Speed == 0 {
			// 字段缺省视为 1.0（P0 不支持变速）
			c.Speed = 1.0
		}
		if c.Speed != 1.0 {
			return newEDLValidationErr(errCodeEDLInvalid, "speed", "P0 不支持变速（speed 必须为 1.0）", i)
		}
		total += outMs - inMs
	}
	if total > edlMaxTotalMs {
		return newEDLValidationErr(errCodeEDLInvalid, "clips", "片段总时长超过 6 小时", -1)
	}
	if p.TotalMs == 0 {
		p.TotalMs = total
	} else if p.TotalMs != total {
		return newEDLValidationErr(errCodeEDLInvalid, "totalMs",
			fmt.Sprintf("totalMs=%d 与 Σ(out-in)=%d 不一致", p.TotalMs, total), -1)
	}
	return nil
}

// ============================================================
// 错误码 → HTTP 状态 / 契约化响应（03 §4.1、§5.3）
// ============================================================

// edlValidateStatus 校验错误码 → HTTP 状态码。
func edlValidateStatus(code string) int {
	switch code {
	case errCodeAssetNotInRoot:
		return http.StatusForbidden
	case errCodeAssetMissing:
		return http.StatusNotFound
	case errCodeEDLTooLarge:
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusBadRequest
	}
}

// edlErrFromValidation 结构化校验失败 → 契约化失败响应 {ok:false, code, msg, detail}
// （03 §4.1；detail 至少含 field，index ≥ 0 时带上便于前端定位）。
func edlErrFromValidation(c *gin.Context, scope string, e *edlValidationError) {
	if e == nil {
		return
	}
	detail := gin.H{"field": e.Field}
	if e.Index >= 0 {
		detail["index"] = e.Index
	}
	if scope != "" {
		detail["scope"] = scope
	}
	edlErr(c, edlValidateStatus(e.Code), e.Code, e.Msg, detail)
}
