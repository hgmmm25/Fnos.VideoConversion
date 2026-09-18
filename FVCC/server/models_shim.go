package main

// P2-1 阶段 A 迁移 shim（models.go → internal/store/model）：
// 模型符号以 type alias / const / func 转发，保证 main 包其余文件
// 继续以原名引用，避免一次性改写 30+ 文件的引用点；
// 阶段 C 模型按域拆分时，随各文件逐步内联清理本 shim。

import "fvcc/internal/store/model"

// ===== 类型别名 =====
type (
	TaskStatus        = model.TaskStatus
	TaskType          = model.TaskType
	RetryType         = model.RetryType
	Task              = model.Task
	Server            = model.Server
	Profile           = model.Profile
	Lock              = model.Lock
	LockEntry         = model.LockEntry
	StreamInfo        = model.StreamInfo
	VideoInfo         = model.VideoInfo
	TasksFile         = model.TasksFile
	HistoryFile       = model.HistoryFile
	ServersFile       = model.ServersFile
	ProfilesFile      = model.ProfilesFile
	LocksFile         = model.LocksFile
	VideoInfoCache    = model.VideoInfoCache
	VideoCacheFile    = model.VideoCacheFile
	Settings          = model.Settings
	SettingsFile      = model.SettingsFile
	Timeline          = model.Timeline
	Transition        = model.Transition
	EDLClip           = model.EDLClip
	Project           = model.Project
	ProjectSummary    = model.ProjectSummary
	WireClip          = model.WireClip
	RenderVideoProfile = model.RenderVideoProfile
	RenderAudioProfile = model.RenderAudioProfile
	RenderProfile     = model.RenderProfile
	RenderTaskPayload = model.RenderTaskPayload
	GPUInfo           = model.GPUInfo
	NodeCaps          = model.NodeCaps
	NodeHealthSample  = model.NodeHealthSample
	AuditEntry        = model.AuditEntry
	ProjectsFile      = model.ProjectsFile
	NodeCapsFile      = model.NodeCapsFile
	NodeHealthFile    = model.NodeHealthFile
	AuditLogFile      = model.AuditLogFile
	AssetProxy        = model.AssetProxy
	AssetProxiesFile  = model.AssetProxiesFile
	ProxyTemplate     = model.ProxyTemplate
	GenProxyPayload   = model.GenProxyPayload
)

// ===== 常量转发 =====
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
)

// ===== 函数转发 =====
func DefaultSettings() Settings { return model.DefaultSettings() }

func DefaultProxyTemplate() ProxyTemplate { return model.DefaultProxyTemplate() }

// boolPtr 迁移期 shim（model.BoolPtr 转发）：main 包仍以原名使用。
func boolPtr(b bool) *bool { return model.BoolPtr(b) }
