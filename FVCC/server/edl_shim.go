package main

// P2-1 B轮：EDL 校验域收敛到 internal/edl 后的根包兼容层。
// 本文件转发 internal/edl 导出符号为原小写名，根包引用点零改动；
// 阶段 C 迁入 api 层后内联删除本文件。

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"fvcc/internal/edl"
	"fvcc/internal/security"
)

// init 注入拒绝类错误码审计钩子（D-04，07 §7）：edl 校验域经此回调完成审计记账，
// 避免校验域反向依赖安全审计域形成 import cycle。
func init() {
	edl.SetAuditRejectionHook(security.AuditRejection)
}

// 错误码转发（03 §5.3）
const (
	errCodeEDLInvalid     = edl.ErrCodeEDLInvalid
	errCodeEDLTooLarge    = edl.ErrCodeEDLTooLarge
	errCodeProfileInvalid = edl.ErrCodeProfileInvalid
	errCodeNodeOffline    = edl.ErrCodeNodeOffline
	errCodeAssetMissing   = edl.ErrCodeAssetMissing
	errCodeAssetNotInRoot = edl.ErrCodeAssetNotInRoot
	errCodePayloadInvalid = edl.ErrCodePayloadInvalid
)

// EDL 约束转发（03 §2.5 / 07 §3.6）
const (
	edlMaxClips     = edl.EDLMaxClips
	edlMinClipMs    = edl.EDLMinClipMs
	edlMaxTotalMs   = edl.EDLMaxTotalMs
	edlMaxBodyBytes = edl.EDLMaxBodyBytes
)

// 渲染载荷约束转发（03 §4.4 / 05 §5.1）
const (
	renderPayloadType   = edl.RenderPayloadType
	renderOutputExt     = edl.RenderOutputExt
	renderContainerMP4  = edl.RenderContainerMP4
	renderAudioCodecAAC = edl.RenderAudioCodecAAC
)

// 变量转发
var (
	edlAllowedSourceExt = edl.EDLAllowedSourceExt
	fvcsSampleRates     = edl.FVCSampleRates
	edlRenderPresetTable = edl.EDLRenderPresetTable
)

// 类型别名转发
type (
	edlValidationError = edl.EDLValidationError
	edlPresetSpec      = edl.EDLPresetSpec
)

func validateRelPath(rel string, allowExt map[string]bool) *edlValidationError {
	return edl.ValidateRelPath(rel, allowExt)
}

func validateRelPathDecoded(rel string, allowExt map[string]bool) *edlValidationError {
	return edl.ValidateRelPathDecoded(rel, allowExt)
}

func edlWithinRoot(root, abs string) bool {
	return edl.EDLWithinRoot(root, abs)
}

func edlResolveRelPath(root, rel string) (string, *edlValidationError) {
	return edl.EDLResolveRelPath(root, rel)
}

func validateOutputNameStrict(name string) *edlValidationError {
	return edl.ValidateOutputNameStrict(name)
}

func validateRootRelPath(root, field string) *edlValidationError {
	return edl.ValidateRootRelPath(root, field)
}

func validatePayloadLimits(raw []byte) *edlValidationError {
	return edl.ValidatePayloadLimits(raw)
}

func decodeRenderPayloadStrict(raw json.RawMessage) (*RenderTaskPayload, *edlValidationError) {
	p, e := edl.DecodeRenderPayloadStrict(raw)
	return p, e
}

func validateRenderTimeline(t *Timeline) *edlValidationError {
	return edl.ValidateRenderTimeline(t)
}

func validateRenderProfile(p *RenderProfile) *edlValidationError {
	return edl.ValidateRenderProfile(p)
}

func validateRenderEDLPayload(p *RenderTaskPayload) *edlValidationError {
	return edl.ValidateRenderEDLPayload(p)
}

func edlValidateStatus(code string) int {
	return edl.EDLValidateStatus(code)
}

func edlErrFromValidation(c *gin.Context, scope string, e *edlValidationError) {
	edl.EDLErrFromValidation(c, scope, e)
}

func newEDLValidationErr(code, field, msg string, index int) *edlValidationError {
	return edl.NewEDLValidationErr(code, field, msg, index)
}

func parseTimecode(tc string) (int64, error) {
	return edl.ParseTimecode(tc)
}

func edlErr(c *gin.Context, status int, code, msg string, detail gin.H) {
	edl.EDLErr(c, status, code, msg, detail)
}
