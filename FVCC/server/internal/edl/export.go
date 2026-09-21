package edl

// P2-1 B轮：EDL 校验域导出包装。
// 根包 edl_shim.go 经本文件转发原小写符号，保证根包引用点零改动；
// 阶段 C 迁入 api 层后，根包应直接 import 本包并删除 edl_shim.go。

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"fvcc/internal/store/model"
)

// 错误码导出（03 §5.3）
const (
	ErrCodeEDLInvalid     = errCodeEDLInvalid
	ErrCodeEDLTooLarge    = errCodeEDLTooLarge
	ErrCodeProfileInvalid = errCodeProfileInvalid
	ErrCodeNodeOffline    = errCodeNodeOffline
	ErrCodeAssetMissing   = errCodeAssetMissing
	ErrCodeAssetNotInRoot = errCodeAssetNotInRoot
	ErrCodePayloadInvalid = errCodePayloadInvalid
)

// EDL 约束导出（03 §2.5 / 07 §3.6）
const (
	EDLMaxClips     = edlMaxClips
	EDLMinClipMs    = edlMinClipMs
	EDLMaxTotalMs   = edlMaxTotalMs
	EDLMaxBodyBytes = edlMaxBodyBytes
)

// 渲染载荷约束导出（03 §4.4 / 05 §5.1）
const (
	RenderPayloadType   = renderPayloadType
	RenderOutputExt     = renderOutputExt
	RenderContainerMP4  = renderContainerMP4
	RenderAudioCodecAAC = renderAudioCodecAAC
)

// 变量导出
var (
	EDLAllowedSourceExt = edlAllowedSourceExt
	FVCSampleRates      = fvcsSampleRates
	EDLRenderPresetTable = edlRenderPresetTable
)

// 类型别名（保留字段与方法集）
type (
	EDLValidationError = edlValidationError
	EDLPresetSpec      = edlPresetSpec
)

// 函数导出包装
func ValidateRelPath(rel string, allowExt map[string]bool) *EDLValidationError {
	return validateRelPath(rel, allowExt)
}

func ValidateRelPathDecoded(rel string, allowExt map[string]bool) *EDLValidationError {
	return validateRelPathDecoded(rel, allowExt)
}

func EDLWithinRoot(root, abs string) bool {
	return edlWithinRoot(root, abs)
}

func EDLResolveRelPath(root, rel string) (string, *EDLValidationError) {
	return edlResolveRelPath(root, rel)
}

func ValidateOutputNameStrict(name string) *EDLValidationError {
	return validateOutputNameStrict(name)
}

func ValidateRootRelPath(root, field string) *EDLValidationError {
	return validateRootRelPath(root, field)
}

func ValidatePayloadLimits(raw []byte) *EDLValidationError {
	return validatePayloadLimits(raw)
}

func DecodeRenderPayloadStrict(raw json.RawMessage) (*model.RenderTaskPayload, *EDLValidationError) {
	return decodeRenderPayloadStrict(raw)
}

func ValidateRenderTimeline(t *model.Timeline) *EDLValidationError {
	return validateRenderTimeline(t)
}

func ValidateRenderProfile(p *model.RenderProfile) *EDLValidationError {
	return validateRenderProfile(p)
}

func ValidateRenderEDLPayload(p *model.RenderTaskPayload) *EDLValidationError {
	return validateRenderEDLPayload(p)
}

func EDLValidateStatus(code string) int {
	return edlValidateStatus(code)
}

func EDLErrFromValidation(c *gin.Context, scope string, e *EDLValidationError) {
	edlErrFromValidation(c, scope, e)
}

func NewEDLValidationErr(code, field, msg string, index int) *EDLValidationError {
	return newEDLValidationErr(code, field, msg, index)
}

func ParseTimecode(tc string) (int64, error) {
	return parseTimecode(tc)
}

func EDLErr(c *gin.Context, status int, code, msg string, detail gin.H) {
	edlErr(c, status, code, msg, detail)
}
