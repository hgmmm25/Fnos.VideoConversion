package edl

// P2-1 B轮：EDL 校验域收敛（原定义于根包 handlers_edl.go / handlers_render.go）。
// 本文件承载 EDL 域共享常量与辅助；根包通过 edl_shim.go 转发，引用点零改动。

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

// ===== 错误码（03 §5.3）=====
const (
	errCodeEDLInvalid     = "E_EDL_INVALID"
	errCodeEDLTooLarge    = "E_EDL_TOO_LARGE"
	errCodeProfileInvalid = "E_PROFILE_INVALID"
	errCodeNodeOffline    = "E_NODE_OFFLINE"
	errCodeAssetMissing   = "E_ASSET_MISSING"
	// E_ASSET_NOT_IN_ROOT（403）：下发前二次校验使用。
	errCodeAssetNotInRoot = "E_ASSET_NOT_IN_ROOT"
)

// ===== 约束（03 §2.5 / 07 §3.6）=====
const (
	edlMaxClips     = 200
	edlMinClipMs    = 100
	edlMaxTotalMs   = 6 * 60 * 60 * 1000 // 6 小时
	edlMaxBodyBytes = 256 * 1024
)

// ===== 渲染常量（03 §4.4 / 07 §3.5 / 05 §5.1）=====
const (
	renderPayloadType   = "RenderEDL"
	renderOutputExt     = ".mp4"
	renderContainerMP4  = "mp4"
	renderAudioCodecAAC = "aac"
)

// fvcsSampleRates FVCS ValidateRenderEDLPayload 允许的采样率集合（03 §5.1 第三道闸门）。
var fvcsSampleRates = map[int]bool{32000: true, 44100: true, 48000: true, 96000: true}

// edlPresetSpec 05 §5.1 presetKey 枚举表（成片导出用；proxy_* 仅用于 04 §3 代理生成）。
type edlPresetSpec struct {
	Codec     string
	CRF       int
	Encoder   string // 编码器 preset（h264_nvenc 的 p5 / libx264 的 medium 等）
	ProxyOnly bool
}

// edlRenderPresetTable 与 FVCS/pkg/protocol.ValidPresetKeys 同集（12 项）。
var edlRenderPresetTable = map[string]edlPresetSpec{
	"copy_same_source":  {Codec: "copy"},
	"h264_nvenc_p5":     {Codec: "h264_nvenc", CRF: 18, Encoder: "p5"},
	"h264_nvenc_p7":     {Codec: "h264_nvenc", CRF: 16, Encoder: "p7"},
	"hevc_nvenc_p5":     {Codec: "hevc_nvenc", CRF: 20, Encoder: "p5"},
	"h264_qsv_balanced": {Codec: "h264_qsv", CRF: 20, Encoder: "medium"},
	"hevc_qsv_balanced": {Codec: "hevc_qsv", CRF: 22, Encoder: "medium"},
	"h264_amf_balanced": {Codec: "h264_amf", CRF: 20, Encoder: "balanced"},
	"libx264_medium":    {Codec: "libx264", CRF: 18, Encoder: "medium"},
	"libx264_slow":      {Codec: "libx264", CRF: 16, Encoder: "slow"},
	"libx265_medium":    {Codec: "libx265", CRF: 20, Encoder: "medium"},
	"proxy_720p_h264":   {Codec: "libx264", CRF: 23, Encoder: "veryfast", ProxyOnly: true},
	"proxy_720p_nvenc":  {Codec: "h264_nvenc", CRF: 23, Encoder: "p5", ProxyOnly: true},
}

// parseTimecode "HH:MM:SS.mmm" → 毫秒（03 §2.4，容忍 "H:MM:SS.mmm" 的少位小时）。
func parseTimecode(tc string) (int64, error) {
	dot := strings.IndexByte(tc, '.')
	if dot < 0 {
		return 0, errors.New("时间码缺少毫秒部分")
	}
	hhmmss := strings.Split(tc[:dot], ":")
	if len(hhmmss) != 3 {
		return 0, errors.New("时间码格式非法")
	}
	vals := make([]int64, 3)
	for i, part := range hhmmss {
		if part == "" {
			return 0, errors.New("时间码字段为空")
		}
		var n int64
		for j := 0; j < len(part); j++ {
			ch := part[j]
			if ch < '0' || ch > '9' {
				return 0, errors.New("时间码含非数字字符")
			}
			n = n*10 + int64(ch-'0')
		}
		vals[i] = n
	}
	msPart := tc[dot+1:]
	if len(msPart) != 3 {
		return 0, errors.New("毫秒必须为 3 位")
	}
	var millis int64
	for i := 0; i < len(msPart); i++ {
		ch := msPart[i]
		if ch < '0' || ch > '9' {
			return 0, errors.New("毫秒含非数字字符")
		}
		millis = millis*10 + int64(ch-'0')
	}
	return ((vals[0]*60+vals[1])*60+vals[2])*1000 + millis, nil
}

// edlErr 统一失败响应。
func edlErr(c *gin.Context, status int, code, msg string, detail gin.H) {
	// D-04：拒绝类错误码集中记账（07 §7）——经根包注入的审计钩子转发
	auditRejection(c, code)
	body := gin.H{"ok": false, "code": code, "msg": msg}
	if detail != nil {
		body["detail"] = detail
	}
	c.JSON(status, body)
}
