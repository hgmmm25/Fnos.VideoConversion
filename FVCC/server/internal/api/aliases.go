package api

// P2-1 C轮：main 根包 handlers/router/stream/security_roots/hashutil 迁入 internal/api 时，
// 将阶段 A/B 遗留的根包 shim 转发符号（models_shim/edl_shim/media_shim/store_shim/
// remote_shim/ws_shim/node_shim/scheduler 薄壳）内联到本包，使迁移代码零改动引用。
// 阶段 C 收口后根包 shim 文件整体删除。

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"fvcc/internal/edl"
	"fvcc/internal/media"
	"fvcc/internal/node"
	"fvcc/internal/remote"
	"fvcc/internal/scheduler"
	"fvcc/internal/security"
	"fvcc/internal/store"
	"fvcc/internal/store/model"
	"fvcc/internal/ws"
)

// init 注入拒绝类错误码审计钩子（D-04，07 §7）：edl 校验域经此回调完成审计记账，
// 避免校验域反向依赖安全审计域形成 import cycle。
func init() {
	edl.SetAuditRejectionHook(security.AuditRejection)
}

// ===== model 别名（原 models_shim.go）=====
type (
	TaskStatus         = model.TaskStatus
	TaskType           = model.TaskType
	RetryType          = model.RetryType
	Task               = model.Task
	Server             = model.Server
	Profile            = model.Profile
	Lock               = model.Lock
	LockEntry          = model.LockEntry
	StreamInfo         = model.StreamInfo
	VideoInfo          = model.VideoInfo
	TasksFile          = model.TasksFile
	HistoryFile        = model.HistoryFile
	ServersFile        = model.ServersFile
	ProfilesFile       = model.ProfilesFile
	LocksFile          = model.LocksFile
	VideoInfoCache     = model.VideoInfoCache
	VideoCacheFile     = model.VideoCacheFile
	Settings           = model.Settings
	SettingsFile       = model.SettingsFile
	Timeline           = model.Timeline
	Transition         = model.Transition
	EDLClip            = model.EDLClip
	Project            = model.Project
	ProjectSummary     = model.ProjectSummary
	WireClip           = model.WireClip
	RenderVideoProfile = model.RenderVideoProfile
	RenderAudioProfile = model.RenderAudioProfile
	RenderProfile      = model.RenderProfile
	RenderTaskPayload  = model.RenderTaskPayload
	GPUInfo            = model.GPUInfo
	NodeCaps           = model.NodeCaps
	NodeHealthSample   = model.NodeHealthSample
	AuditEntry         = model.AuditEntry
	ProjectsFile       = model.ProjectsFile
	NodeCapsFile       = model.NodeCapsFile
	NodeHealthFile     = model.NodeHealthFile
	AuditLogFile       = model.AuditLogFile
	AssetProxy         = model.AssetProxy
	AssetProxiesFile   = model.AssetProxiesFile
	ProxyTemplate      = model.ProxyTemplate
	GenProxyPayload    = model.GenProxyPayload
)

// ===== model 常量转发 =====
const (
	StatusQueue        = model.StatusQueue
	StatusUploading    = model.StatusUploading
	StatusWaitingTrans = model.StatusWaitingTrans
	StatusTranscoding  = model.StatusTranscoding
	StatusWaitingDown  = model.StatusWaitingDown
	StatusDownloading  = model.StatusDownloading
	StatusCompleted    = model.StatusCompleted
	StatusError        = model.StatusError
	StatusPaused       = model.StatusPaused
	StatusCancelled    = model.StatusCancelled
	StatusCooldown     = model.StatusCooldown

	TaskTypeTranscode = model.TaskTypeTranscode
	TaskTypeRenderEDL = model.TaskTypeRenderEDL
	TaskTypeGenProxy  = model.TaskTypeGenProxy

	StagePrepare  = model.StagePrepare
	StageSegment  = model.StageSegment
	StageConcat   = model.StageConcat
	StageMux      = model.StageMux
	StageFinalize = model.StageFinalize

	Retryable    = model.Retryable
	NonRetryable = model.NonRetryable

	ProxyModeFull         = model.ProxyModeFull
	ProxyModeKeyframeOnly = model.ProxyModeKeyframeOnly
	ProxyStateReady       = model.ProxyStateReady
	ProxyStateInvalid     = model.ProxyStateInvalid
	ProxyStateStale       = model.ProxyStateStale

	DefaultSchedulerIntervalSec = model.DefaultSchedulerIntervalSec
	DefaultChunkSizeMB          = model.DefaultChunkSizeMB
	DefaultHistoryLimit         = model.DefaultHistoryLimit
)

// ===== model 函数转发 =====
func DefaultSettings() Settings { return model.DefaultSettings() }

func DefaultProxyTemplate() ProxyTemplate { return model.DefaultProxyTemplate() }

func boolPtr(b bool) *bool { return model.BoolPtr(b) }

// ===== edl 别名（原 edl_shim.go）=====

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
	edlAllowedSourceExt  = edl.EDLAllowedSourceExt
	fvcsSampleRates      = edl.FVCSampleRates
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

// ===== store 别名（原 store_shim.go）=====
type Store = store.Store

var (
	NewStore           = store.NewStore
	ErrProjectNameUsed = store.ErrProjectNameUsed
	ErrRevConflict     = store.ErrRevConflict
	ErrProjectNotFound = store.ErrProjectNotFound
	TrashDirName       = store.TrashDirName
	MoveToTrash        = store.MoveToTrash
)

// ===== remote 别名（原 remote_shim.go）=====

// 错误码转发（06 §4.3 白名单；E_EDL_INVALID 见 edl 别名）
const (
	errCodeSMBMountFailed = remote.ErrCodeSMBMountFailed
	errCodeRenderFailed   = remote.ErrCodeRenderFailed
	errCodePayloadMissing = remote.ErrCodePayloadMissing
)

// 类型别名转发
type (
	RemoteClient     = remote.RemoteClient
	RemoteProgress   = remote.RemoteProgress
	RemoteTaskStatus = remote.RemoteTaskStatus
	RenderDispatcher = remote.RenderDispatcher
)

// 变量转发
var NewRemoteClient = remote.NewRemoteClient

// 函数转发
func normalizeRootForCompare(p string) string {
	return remote.NormalizeRootForCompare(p)
}

func parseHelloCaps(serverID string, data json.RawMessage) (model.NodeCaps, bool) {
	return remote.ParseHelloCaps(serverID, data)
}

// ===== ws 别名（原 ws_shim.go）=====
type (
	Hub              = ws.Hub
	SegInfo          = ws.SegInfo
	TaskUpdateFullMsg = ws.TaskUpdateFullMsg
	ProxyReadyMsg    = ws.ProxyReadyMsg
	NodeStatusMsg    = ws.NodeStatusMsg
	TaskUpdateMsg    = ws.TaskUpdateMsg
	InfoMsg          = ws.InfoMsg
)

// 常量转发：B-07 聚合窗口 / B-08 健康分档 / D-04 WS 连接上限
const (
	taskUpdateAggInterval  = ws.TaskUpdateAggInterval
	proxyReadyDedupeWindow = ws.ProxyReadyDedupeWindow
	nodeHealthBandSize     = ws.NodeHealthBandSize
	healthScoreUnknown     = ws.HealthScoreUnknown
	wsBrowserMaxConns      = ws.WSBrowserMaxConns
)

// NewHub 创建 Hub（转发 internal/ws.NewHub）。
func NewHub() *Hub { return ws.NewHub() }

// ===== node 别名（原 node_shim.go）=====
var errNoSelectableNode = node.ErrNoSelectableNode

// ===== media 别名（原 media_shim.go 中 api 需要的部分）=====

// NewFFprobe 创建 ffprobe 封装（转发至 internal/media）。
func NewFFprobe() *media.FFprobe { return media.NewFFprobe() }

// findFFmpeg 查找 ffmpeg 可执行文件（转发至 internal/media）。
func findFFmpeg() string { return media.FindFFmpeg() }

// RunningCount 返回本地转码运行计数（转发至 internal/media）。
func RunningCount() int32 { return media.RunningCount() }

// AddRunningCount 原子增减本地转码运行计数（转发至 internal/media）。
func AddRunningCount(delta int32) int32 { return media.AddRunningCount(delta) }

// StopLocalTranscode 停止本地转码（转发至 internal/media）。
func StopLocalTranscode(taskID string) { media.StopLocalTranscode(taskID) }

// GetLocalTranscodeProgress 返回本地转码进度（转发至 internal/media）。
func GetLocalTranscodeProgress(taskID string) float64 { return media.GetLocalTranscodeProgress(taskID) }

// ===== scheduler 别名（原根包 scheduler.go 薄壳）=====
type Scheduler = scheduler.Scheduler

func NewScheduler(store *Store, remote *RemoteClient, hub *Hub, pv *security.PathValidator) *Scheduler {
	return scheduler.NewScheduler(store, remote, hub, pv)
}
