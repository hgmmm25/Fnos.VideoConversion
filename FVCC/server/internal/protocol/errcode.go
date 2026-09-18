package protocol

// errcode.go — P2-1 阶段 A：跨包共享的稳定业务错误码常量。
//
// 这些错误码原先散落在 main 包各文件（handlers_edl.go / edl_validate.go /
// handlers_render.go），审计模块（internal/security）迁移时需要引用，
// 故在协议层收敛导出。main 包内同名私有常量在阶段 B 迁入 internal/api 时统一收口。

const (
	// ErrCodeEDLInvalid EDL 载荷校验失败（03 §5.3）。
	ErrCodeEDLInvalid = "E_EDL_INVALID"
	// ErrCodePayloadInvalid 通用载荷校验失败（03 §5.3）。
	ErrCodePayloadInvalid = "E_PAYLOAD_INVALID"
	// ErrCodeAssetNotInRoot 素材越权（不在授权根内，07 §7）。
	ErrCodeAssetNotInRoot = "E_ASSET_NOT_IN_ROOT"
	// ErrCodeRateLimited 限流拒绝错误码（07 §4.5；03 §5.3 未定义，实现补充）。
	ErrCodeRateLimited = "E_RATE_LIMITED"
)
