package protocol

// EDL / RenderEDL / GenProxy 载荷契约（对应设计文档 03 §2、§3、§5 与 07 §3.3）
//
// 说明：
//   - 时间表示三态统一（03 §2.4）：内存态/持久化/REST 用整数毫秒；
//     线协议 WireClip.in/out 用 "HH:MM:SS.mmm" 字符串，与 ffmpeg -ss/-t 文本一致。
//   - 本文件是 FVCC 侧 edl_validate.go 的同源规则集（03 §5.2），两侧必须逐条对齐。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ============================================================
// 命令与任务类型（03 §3.1）
// ============================================================

const (
	CmdCreateRenderEDL CmdType = "CreateRenderEDL"
	CmdCreateGenProxy  CmdType = "CreateGenProxy" // 见 04 §3
)

// TaskType 字段取值
const (
	TaskTypeTranscode = "TRANSCODE" // 默认（既有行为）
	TaskTypeRenderEDL = "RENDER_EDL"
	TaskTypeGenProxy  = "GEN_PROXY"
)

// 载荷 type 字段取值
const (
	PayloadTypeRenderEDL = "RenderEDL"
	PayloadTypeGenProxy  = "GenProxy"
)

// ============================================================
// 错误码表（03 §5.3）
// ============================================================

const (
	ErrCodeEDLInvalid         = "E_EDL_INVALID"
	ErrCodeEDLTooLarge        = "E_EDL_TOO_LARGE"
	ErrCodePayloadInvalid     = "E_PAYLOAD_INVALID"
	ErrCodeProfileInvalid     = "E_PROFILE_INVALID"
	ErrCodeProtoFieldConflict = "E_PROTO_FIELD_CONFLICT"
	ErrCodeAssetMissing       = "E_ASSET_MISSING"
	ErrCodeAssetNotInRoot     = "E_ASSET_NOT_IN_ROOT"
	ErrCodeRevConflict        = "E_REV_CONFLICT"
	ErrCodeNodeOffline        = "E_NODE_OFFLINE"
	ErrCodeSMBMountFailed     = "E_SMB_MOUNT_FAILED"
	ErrCodeRenderFailed       = "E_RENDER_FAILED"
	ErrCodeFFmpegMissing      = "E_FFMPEG_MISSING"
	ErrCodeDiskFull           = "E_DISK_FULL"
	ErrCodeTimeoutStall       = "E_TIMEOUT_STALL"

	// ---- 凭据档案（07 §5.3 / §5.4）----
	ErrCodeCredentialNotFound    = "E_CREDENTIAL_NOT_FOUND"
	ErrCodeCredentialScopeDenied = "E_CREDENTIAL_SCOPE_DENIED"
	ErrCodeCredentialInvalid     = "E_CREDENTIAL_INVALID"
	ErrCodeCredentialInUse       = "E_CREDENTIAL_IN_USE"
	// ErrCodeCredentialMissing 挂载前的缺凭据前置判定（代理 E_RENDER_FAILED 闭环）：
	// credentialId 与明文账号口令全空时直接失败，不进 net use 交互式认证态。
	ErrCodeCredentialMissing = "E_CREDENTIAL_MISSING"
)

// PayloadError 结构化错误：必须包含可定位的 field / index（03 §5.1）
type PayloadError struct {
	Code  string `json:"code"`
	Msg   string `json:"msg"`
	Field string `json:"field,omitempty"`
	Index int    `json:"index"`
}

func (e *PayloadError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (field=%s, index=%d)", e.Code, e.Msg, e.Field, e.Index)
	}
	return fmt.Sprintf("%s: %s (index=%d)", e.Code, e.Msg, e.Index)
}

func newPayloadErr(code, field, msg string, index int) *PayloadError {
	return &PayloadError{Code: code, Msg: msg, Field: field, Index: index}
}

// ============================================================
// 载荷类型（03 §2.2）
// ============================================================

// Timeline 输出时间轴参数
type Timeline struct {
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	FPS        int  `json:"fps"`
	SampleRate int  `json:"sampleRate"`
	Audio      bool `json:"audio"`
}

// WireClip 线协议片段：in/out 为 "HH:MM:SS.mmm"
type WireClip struct {
	File  string  `json:"file"`
	In    string  `json:"in"`
	Out   string  `json:"out"`
	Speed float64 `json:"speed"`
}

// RenderVideoProfile 视频编码参数
type RenderVideoProfile struct {
	Codec   string `json:"codec"`
	CRF     int    `json:"crf"`
	Preset  string `json:"preset"`
	PixFmt  string `json:"pixFmt"`
	Bitrate string `json:"bitrate,omitempty"` // [P1+] 预留，P0 存在即拒绝
}

// RenderAudioProfile 音频编码参数
type RenderAudioProfile struct {
	Codec      string `json:"codec"` // P0 恒为 aac
	Bitrate    string `json:"bitrate"`
	Channels   int    `json:"channels"`
	SampleRate int    `json:"sampleRate"`
}

// RenderProfile 渲染方案
type RenderProfile struct {
	PresetKey       string             `json:"presetKey"`
	Container       string             `json:"container"` // P0 恒为 mp4
	Video           RenderVideoProfile `json:"video"`
	Audio           RenderAudioProfile `json:"audio"`
	FastCopyAllowed bool               `json:"fastCopyAllowed"` // 服务端计算后回填，前端仅展示
}

// RenderTaskPayload 渲染任务载荷（FVCC → FVCS）
type RenderTaskPayload struct {
	Type       string        `json:"type"` // 恒为 "RenderEDL"
	ProjectID  string        `json:"projectId"`
	ProjectRev int           `json:"projectRev"`
	SourceRoot string        `json:"sourceRoot"`
	DestRoot   string        `json:"destRoot"`
	Output     string        `json:"output"` // 相对 DestRoot，P0 强制 .mp4
	Timeline   Timeline      `json:"timeline"`
	Clips      []WireClip    `json:"clips"`
	Profile    RenderProfile `json:"profile"`
	TotalMs    int64         `json:"totalMs"`
	Checksum   string        `json:"checksum"`
}

// ProxyTemplate 代理参数模板（04 §3.3）
type ProxyTemplate struct {
	PresetKey    string `json:"presetKey"`
	Height       int    `json:"height"`
	FPSFollowSrc bool   `json:"fpsFollowSource"`
	CRF          int    `json:"crf"`
	Preset       string `json:"preset"`
	AudioBitrate string `json:"audioBitrate"`
	GOPSec       int    `json:"gopSeconds"`
	KeyframeOnly bool   `json:"keyframeOnly,omitempty"` // [P1+] 存在即拒绝
}

// GenProxyPayload 代理生成载荷（04 §3.2）
type GenProxyPayload struct {
	Type      string        `json:"type"` // 恒为 "GenProxy"
	AssetID   string        `json:"assetId"`
	SrcFile   string        `json:"srcFile"`
	ProxyFile string        `json:"proxyFile"`
	Template  ProxyTemplate `json:"template"`
	// SourceRoot 本次任务实际使用的素材根（与 FVCC 侧 GenProxyPayload 同名同 tag）。
	// 为空表示沿用缺省素材根；非空时仅用于放行该扩展字段，避免 Strict 解码因未知字段拒绝。
	SourceRoot string `json:"sourceRoot,omitempty"`
}

// ============================================================
// Profile 枚举表（05 §5.1，服务端权威）
// ============================================================

const (
	PresetCopySameSource  = "copy_same_source"
	PresetH264NVENCP5     = "h264_nvenc_p5"
	PresetH264NVENCP7     = "h264_nvenc_p7"
	PresetHEVCNVENCP5     = "hevc_nvenc_p5"
	PresetH264QSVBalanced = "h264_qsv_balanced"
	PresetHEVCQSVBalanced = "hevc_qsv_balanced"
	PresetH264AMFBalanced = "h264_amf_balanced"
	PresetLibx264Medium   = "libx264_medium"
	PresetLibx264Slow     = "libx264_slow"
	PresetLibx265Medium   = "libx265_medium"
	PresetProxy720pH264   = "proxy_720p_h264"
	PresetProxy720pNVENC  = "proxy_720p_nvenc"
)

// ValidPresetKeys 枚举白名单（顺序与 05 §5.1 表一致）
var ValidPresetKeys = []string{
	PresetCopySameSource,
	PresetH264NVENCP5,
	PresetH264NVENCP7,
	PresetHEVCNVENCP5,
	PresetH264QSVBalanced,
	PresetHEVCQSVBalanced,
	PresetH264AMFBalanced,
	PresetLibx264Medium,
	PresetLibx264Slow,
	PresetLibx265Medium,
	PresetProxy720pH264,
	PresetProxy720pNVENC,
}

// IsValidPresetKey 判断 presetKey 是否命中服务端枚举表
func IsValidPresetKey(key string) bool {
	for _, k := range ValidPresetKeys {
		if k == key {
			return true
		}
	}
	return false
}

// ============================================================
// 时间换算（03 §2.4，与前端逐位一致）
// ============================================================

var timecodeRe = regexp.MustCompile(`^\d{2,}:[0-5]\d:[0-5]\d\.\d{3}$`)

// FormatMs 整数毫秒 → "HH:MM:SS.mmm"（全整数运算，禁止浮点秒）
func FormatMs(ms int64) string {
	sign := ""
	if ms < 0 {
		sign = "-"
		ms = -ms
	}
	h := ms / 3600000
	ms %= 3600000
	m := ms / 60000
	ms %= 60000
	s := ms / 1000
	ms %= 1000
	return fmt.Sprintf("%s%02d:%02d:%02d.%03d", sign, h, m, s, ms)
}

// ParseTimecode "HH:MM:SS.mmm" → 整数毫秒
func ParseTimecode(tc string) (int64, error) {
	if !timecodeRe.MatchString(tc) {
		return 0, fmt.Errorf("时间码格式非法: %q", tc)
	}
	dot := strings.IndexByte(tc, '.')
	ms, err := parseUint(tc[dot+1:])
	if err != nil {
		return 0, err
	}
	parts := strings.Split(tc[:dot], ":")
	h, err := parseUint(parts[0])
	if err != nil {
		return 0, err
	}
	m, _ := parseUint(parts[1])
	s, _ := parseUint(parts[2])
	return ((h*3600+m*60+s)*1000 + ms), nil
}

func parseUint(s string) (int64, error) {
	var v int64
	if s == "" {
		return 0, fmt.Errorf("空数值")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("非法数字 %q", s)
		}
		v = v*10 + int64(s[i]-'0')
	}
	return v, nil
}

// ClipDurationMs 片段时长（开区间语义：out - in）
func ClipDurationMs(c WireClip) (int64, error) {
	in, err := ParseTimecode(c.In)
	if err != nil {
		return 0, err
	}
	out, err := ParseTimecode(c.Out)
	if err != nil {
		return 0, err
	}
	return out - in, nil
}

// ============================================================
// 校验规则（03 §2.5 / §5.2）
// ============================================================

const (
	MaxClips         = 200
	MinClipMs        = 100
	MaxTotalMs       = 6 * 3600 * 1000
	MaxEDLPayloadLen = 256 * 1024
)

// 允许的素材扩展名（大小写不敏感）
var allowedSourceExt = map[string]bool{
	"mp4": true, "mov": true, "mkv": true, "m4v": true, "avi": true, "mxf": true,
}

// SplitErrorCode 拆分 "E_XXX: msg" 形式的错误文本（无错误码前缀时回落为 E_PAYLOAD_INVALID）
func SplitErrorCode(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	text := err.Error()
	if strings.HasPrefix(text, "E_") {
		if i := strings.Index(text, ": "); i > 0 {
			return text[:i], strings.TrimSpace(text[i+2:])
		}
		return text, ""
	}
	return ErrCodePayloadInvalid, text
}

// ValidateSourceFile 校验相对 SourceRoot 的素材路径（03 §5.2 的 file 行）。
// 规则单一来源：edl_validate.go 的 ValidateRelPath（07 §3.2 L1~L3），此处仅做字段名映射。
func ValidateSourceFile(name string) *PayloadError {
	if e := ValidateRelPath(name, AllowedSourceExt); e != nil {
		e.Field = "file"
		return e
	}
	return nil
}

// ValidateOutputName 校验输出文件名（相对 DestRoot，P0 不允许子目录）。
// 规则单一来源：edl_validate.go 的 validateOutputNameStrict（07 §3.5：单段 + .mp4 +
// 禁多重扩展名 + 拒 Windows 保留设备名）。
func ValidateOutputName(name string) *PayloadError {
	return validateOutputNameStrict(name)
}

func validateTimeline(t *Timeline) *PayloadError {
	if t.FPS < 1 || t.FPS > 120 {
		return newPayloadErr(ErrCodeEDLInvalid, "timeline.fps", "帧率必须在 1..120", -1)
	}
	if t.Width < 16 || t.Width > 7680 || t.Height < 16 || t.Height > 7680 {
		return newPayloadErr(ErrCodeEDLInvalid, "timeline.width/height", "分辨率必须在 16..7680", -1)
	}
	if t.Width%2 != 0 || t.Height%2 != 0 {
		return newPayloadErr(ErrCodeEDLInvalid, "timeline.width/height", "宽高必须为偶数", -1)
	}
	if t.Audio && t.SampleRate != 32000 && t.SampleRate != 44100 && t.SampleRate != 48000 && t.SampleRate != 96000 {
		return newPayloadErr(ErrCodeEDLInvalid, "timeline.sampleRate", "采样率不在允许集合", -1)
	}
	return nil
}

// validateProfile 校验渲染方案（03 §5.2 的 presetKey 行 + P0 预留字段拒绝）
func ValidateProfile(p *RenderProfile) *PayloadError {
	if !IsValidPresetKey(p.PresetKey) {
		return newPayloadErr(ErrCodeProfileInvalid, "profile.presetKey", "presetKey 不在服务端枚举表", -1)
	}
	if p.Container != "mp4" {
		return newPayloadErr(ErrCodeProfileInvalid, "profile.container", "P0 容器固定 mp4", -1)
	}
	if p.Video.Bitrate != "" {
		return newPayloadErr(ErrCodeProfileInvalid, "profile.video.bitrate", "P1+ 预留字段，P0 拒绝", -1)
	}
	if p.Audio.Codec != "aac" {
		return newPayloadErr(ErrCodeProfileInvalid, "profile.audio.codec", "P0 音频编码固定 aac", -1)
	}
	if p.Audio.Channels < 1 || p.Audio.Channels > 8 {
		return newPayloadErr(ErrCodeProfileInvalid, "profile.audio.channels", "声道数必须在 1..8", -1)
	}
	if p.Audio.SampleRate != 32000 && p.Audio.SampleRate != 44100 &&
		p.Audio.SampleRate != 48000 && p.Audio.SampleRate != 96000 {
		return newPayloadErr(ErrCodeProfileInvalid, "profile.audio.sampleRate", "采样率不在允许集合", -1)
	}
	return nil
}

// ValidateRenderEDLPayload 载荷白名单校验（FVCS 侧闸门，03 §5.2 / 07 §3.3）
// 校验通过时会就地补全 TotalMs（若为 0）。
func ValidateRenderEDLPayload(p *RenderTaskPayload) *PayloadError {
	if p == nil {
		return newPayloadErr(ErrCodePayloadInvalid, "", "载荷为空", -1)
	}
	if p.Type != PayloadTypeRenderEDL {
		return newPayloadErr(ErrCodePayloadInvalid, "type", "type 必须为 RenderEDL", -1)
	}
	// 三根相对路径白名单（07 §3.2）：空值表示沿用服务端默认根
	if e := ValidateRootRelPath(p.SourceRoot, "sourceRoot"); e != nil {
		return e
	}
	if e := ValidateRootRelPath(p.DestRoot, "destRoot"); e != nil {
		return e
	}
	if err := validateTimeline(&p.Timeline); err != nil {
		return err
	}
	if err := ValidateOutputName(p.Output); err != nil {
		return err
	}
	if err := ValidateProfile(&p.Profile); err != nil {
		return err
	}

	n := len(p.Clips)
	if n < 1 || n > MaxClips {
		return newPayloadErr(ErrCodeEDLInvalid, "clips", fmt.Sprintf("片段数必须在 1..%d，实际 %d", MaxClips, n), -1)
	}

	var total int64
	for i := range p.Clips {
		c := &p.Clips[i]
		if err := ValidateSourceFile(c.File); err != nil {
			err.Index = i
			return err
		}
		inMs, err := ParseTimecode(c.In)
		if err != nil {
			return newPayloadErr(ErrCodeEDLInvalid, "in", err.Error(), i)
		}
		outMs, err := ParseTimecode(c.Out)
		if err != nil {
			return newPayloadErr(ErrCodeEDLInvalid, "out", err.Error(), i)
		}
		if outMs <= inMs {
			return newPayloadErr(ErrCodeEDLInvalid, "out", "out 必须晚于 in", i)
		}
		if outMs-inMs < MinClipMs {
			return newPayloadErr(ErrCodeEDLInvalid, "out", fmt.Sprintf("单片段时长不得小于 %dms", MinClipMs), i)
		}
		if c.Speed == 0 {
			// 字段缺省视为 1.0（P0 不支持变速）
			c.Speed = 1.0
		}
		if c.Speed != 1.0 {
			return newPayloadErr(ErrCodeEDLInvalid, "speed", "P0 不支持变速（speed 必须为 1.0）", i)
		}
		total += outMs - inMs
	}
	if total > MaxTotalMs {
		return newPayloadErr(ErrCodeEDLInvalid, "clips", "片段总时长超过 6 小时", -1)
	}
	if p.TotalMs == 0 {
		p.TotalMs = total
	} else if p.TotalMs != total {
		return newPayloadErr(ErrCodeEDLInvalid, "totalMs",
			fmt.Sprintf("totalMs=%d 与 Σ(out-in)=%d 不一致", p.TotalMs, total), -1)
	}
	return nil
}

// DecodeRenderPayloadStrict 严格解码：出现未声明字段即拒绝（03 §5.2 payload 额外字段行）
func DecodeRenderPayloadStrict(raw json.RawMessage) (*RenderTaskPayload, *PayloadError) {
	if len(raw) == 0 {
		return nil, newPayloadErr(ErrCodePayloadInvalid, "", "payload 缺失", -1)
	}
	// 体积 / 嵌套深度 / 单字符串长度约束（07 §3.6）
	if e := ValidatePayloadLimits(raw); e != nil {
		return nil, e
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var p RenderTaskPayload
	if err := dec.Decode(&p); err != nil {
		return nil, newPayloadErr(ErrCodePayloadInvalid, "", "载荷解析失败: "+err.Error(), -1)
	}
	return &p, nil
}

// ============================================================
// GenProxy 载荷校验（04 §3.2 / §3.3）
// ============================================================

// IsValidProxyPresetKey 判断代理模板 presetKey 是否命中白名单
// （取值见 §5.1 枚举表：PresetProxy720pH264 / PresetProxy720pNVENC）
func IsValidProxyPresetKey(key string) bool {
	return key == PresetProxy720pH264 || key == PresetProxy720pNVENC
}

// DecodeGenProxyPayloadStrict 严格解码代理载荷（未声明字段即拒绝）
func DecodeGenProxyPayloadStrict(raw json.RawMessage) (*GenProxyPayload, *PayloadError) {
	if len(raw) == 0 {
		return nil, newPayloadErr(ErrCodePayloadInvalid, "", "payload 缺失", -1)
	}
	// 体积 / 嵌套深度 / 单字符串长度约束（07 §3.6）
	if e := ValidatePayloadLimits(raw); e != nil {
		return nil, e
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var p GenProxyPayload
	if err := dec.Decode(&p); err != nil {
		return nil, newPayloadErr(ErrCodePayloadInvalid, "", "载荷解析失败: "+err.Error(), -1)
	}
	return &p, nil
}

// ValidateGenProxyPayload 代理载荷白名单校验（04 §3.2 / 07 §3.3）
func ValidateGenProxyPayload(p *GenProxyPayload) *PayloadError {
	if p == nil {
		return newPayloadErr(ErrCodePayloadInvalid, "", "payload 为空", -1)
	}
	if p.Type != PayloadTypeGenProxy {
		return newPayloadErr(ErrCodePayloadInvalid, "type", "type 必须为 "+PayloadTypeGenProxy, -1)
	}
	if e := ValidateSourceFile(p.SrcFile); e != nil {
		return newPayloadErr(e.Code, "srcFile", e.Msg, -1)
	}
	// proxyFile 相对 smbOutputPath，允许子目录（04 §3.2 示例 demo/a_01.proxy.mp4）
	if e := ValidateSourceFile(p.ProxyFile); e != nil {
		return newPayloadErr(e.Code, "proxyFile", e.Msg, -1)
	}
	if e := validateProxyTemplate(&p.Template); e != nil {
		return e
	}
	return nil
}

// validateProxyTemplate 校验代理参数模板（04 §3.3 取值表）
func validateProxyTemplate(t *ProxyTemplate) *PayloadError {
	if !IsValidProxyPresetKey(t.PresetKey) {
		return newPayloadErr(ErrCodeProfileInvalid, "template.presetKey",
			"未知代理模板: "+t.PresetKey, -1)
	}
	// P1+ 预留字段：出现即拒绝（P0 不做关键帧抽取）
	if t.KeyframeOnly {
		return newPayloadErr(ErrCodeProfileInvalid, "template.keyframeOnly",
			"keyframeOnly 为 P1+ 保留字段，P0 不支持", -1)
	}
	if t.Height <= 0 || t.Height > 2160 || t.Height%2 != 0 {
		return newPayloadErr(ErrCodeProfileInvalid, "template.height",
			fmt.Sprintf("height=%d 非法（1~2160 且为偶数）", t.Height), -1)
	}
	if t.CRF <= 0 || t.CRF > 51 {
		return newPayloadErr(ErrCodeProfileInvalid, "template.crf",
			fmt.Sprintf("crf=%d 非法（1~51）", t.CRF), -1)
	}
	if strings.TrimSpace(t.Preset) == "" {
		return newPayloadErr(ErrCodeProfileInvalid, "template.preset", "preset 为空", -1)
	}
	if strings.TrimSpace(t.AudioBitrate) == "" {
		return newPayloadErr(ErrCodeProfileInvalid, "template.audioBitrate", "audioBitrate 为空", -1)
	}
	if t.GOPSec <= 0 || t.GOPSec > 10 {
		return newPayloadErr(ErrCodeProfileInvalid, "template.gopSeconds",
			fmt.Sprintf("gopSeconds=%d 非法（1~10）", t.GOPSec), -1)
	}
	return nil
}

// ============================================================
// 幂等键（06 §4.4）：clips + profile 规范化后的 sha256 前 16 位
// ============================================================

type checksumInput struct {
	Clips   []WireClip    `json:"clips"`
	Profile RenderProfile `json:"profile"`
}

// ComputeRenderChecksum 规范化计算幂等键。
// 注意：FastCopyAllowed 为服务端回填字段，计算前统一置 false，保证 FVCC/FVCS 两侧同值。
func ComputeRenderChecksum(p *RenderTaskPayload) string {
	prof := p.Profile
	prof.FastCopyAllowed = false
	payload := checksumInput{Clips: p.Clips, Profile: prof}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// VerifyRenderChecksum 校验载荷携带的 checksum 是否与重算值一致（不一致仅供告警，不致命）
func VerifyRenderChecksum(p *RenderTaskPayload) bool {
	if p.Checksum == "" {
		return false
	}
	return strings.EqualFold(p.Checksum, ComputeRenderChecksum(p))
}
