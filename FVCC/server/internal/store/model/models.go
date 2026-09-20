// Package model 是 FVCC 持久化契约模型（P2-1 阶段 A 前置下沉）。
// 归属规则（见混乱报告 §4.1.5）：凡落盘 / 跨进程传输的结构体归本包；
// 仅内存态的内部结构体留在所属业务包内。本文件为 models.go 整体迁入，
// 阶段 C 将按域拆分为 task.go / server.go / profile.go / settings.go /
// project.go / edl.go / asset.go / audit.go / nodecaps.go / common.go。
package model

import "time"

// ===== 任务状态枚举 =====

type TaskStatus string

const (
	StatusQueue        TaskStatus = "QUEUE"         // 队列等待
	StatusUploading    TaskStatus = "UPLOADING"     // 上传中
	StatusWaitingTrans TaskStatus = "WAITING_TRANS" // 待转码
	StatusTranscoding  TaskStatus = "TRANSCODING"   // 转码中
	StatusWaitingDown  TaskStatus = "WAITING_DOWN"  // 待下载
	StatusDownloading  TaskStatus = "DOWNLOADING"   // 下载中
	StatusCompleted    TaskStatus = "COMPLETED"     // 完成
	StatusError        TaskStatus = "ERROR"         // 错误
	StatusPaused       TaskStatus = "PAUSED"        // 暂停
	StatusCancelled    TaskStatus = "CANCELLED"     // 已取消
	// StatusCooldown 冷却等待中（06 §2.3 新增取值）：派发失败后按退避策略置为该状态，
	// 到期由调度器转回 StatusQueue（保留 OrderID，不改变用户可见顺序）。
	StatusCooldown TaskStatus = "COOLDOWN"
)

// IsTerminal 判断是否终态。
func (s TaskStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled
}

// IsActive 判断任务是否仍占用调度（未被终态收口）。
// 用于 06 §4.4 幂等检查：checksum 相同的进行中任务直接复用。
func (s TaskStatus) IsActive() bool {
	return !s.IsTerminal()
}

// ===== 任务类型与渲染阶段（06 §2.2）=====

// TaskType 任务类型，对应 tasks.task_type 列。
type TaskType string

const (
	TaskTypeTranscode TaskType = "TRANSCODE"  // 默认：既有文件转码
	TaskTypeRenderEDL TaskType = "RENDER_EDL" // EDL 时间线渲染（05）
	TaskTypeGenProxy  TaskType = "GEN_PROXY"  // 代理生成（04）
)

// 渲染阶段取值，对应 tasks.stage 列（05 §3）。
const (
	StagePrepare  = "prepare"
	StageSegment  = "segment"
	StageConcat   = "concat"
	StageMux      = "mux"
	StageFinalize = "finalize"
)

// 任务状态 → 06 §2.3 线协议取值的映射（仅用于对外展示/审计，不改写存储枚举）：
//
//	StatusQueue       ↔ QUEUE
//	StatusTranscoding ↔ RUNNING
//	StatusCompleted   ↔ SUCCESS
//	StatusError       ↔ FAILED
//	StatusCancelled   ↔ CANCELED
//	StatusCooldown    ↔ COOLDOWN
func (s TaskStatus) WireName() string {
	switch s {
	case StatusQueue:
		return "QUEUE"
	case StatusTranscoding:
		return "RUNNING"
	case StatusCompleted:
		return "SUCCESS"
	case StatusError:
		return "FAILED"
	case StatusCancelled:
		return "CANCELED"
	case StatusCooldown:
		return "COOLDOWN"
	default:
		return string(s)
	}
}

// RetryType 错误重试类型。
type RetryType string

const (
	Retryable    RetryType = "retryable"     // 可重试
	NonRetryable RetryType = "non_retryable" // 不可重试
)

// ===== 数据模型 =====

// Task 转码任务。
type Task struct {
	ID             string     `json:"id"`
	OrderID        int64      `json:"orderId"`     // 创建顺序号，用于任务排序
	SourceFile     string     `json:"sourceFile"`  // 本地源文件路径
	OutputFile     string     `json:"outputFile"`  // 本地输出文件路径
	FileName       string     `json:"fileName"`    // 源文件名
	OutputName     string     `json:"outputName"`  // 输出文件名
	ServerID       string     `json:"serverId"`    // 转码服务器 ID
	ServerName     string     `json:"serverName"`  // 转码服务器名称（前端显示用）
	ProfileID      string     `json:"profileId"`   // 转码方案 ID
	ProfileName    string     `json:"profileName"` // 转码方案名称（前端显示用）
	FFmpegArgs     string     `json:"ffmpegArgs"`  // FFmpeg 编码参数
	Status         TaskStatus `json:"status"`
	Progress       float64    `json:"progress"` // 0-100
	RetryCount     int        `json:"retryCount"`
	RetryType      RetryType  `json:"retryType"`
	CoolDownUntil  *time.Time `json:"coolDownUntil"`    // 冷却截止时间
	CoolDownSec    int        `json:"coolDownSec"`      // 当前冷却秒数
	UploadChunkIdx int        `json:"uploadChunkIndex"` // 断点续传：已上传分片索引
	RemoteTaskID   string     `json:"remoteTaskId"`     // 远端服务端返回的任务 ID
	DownloadOffset int64      `json:"downloadOffset"`   // 断点续传下载偏移
	ErrorMsg       string     `json:"errorMsg"`
	// ===== B-01：tasks 表列迁移（06 §2.2）=====
	// 说明：06 文档中的 next_retry_at / attempt 两列复用既有字段 CoolDownUntil / RetryCount，
	// 避免同一语义出现双份字段；其余列按下述字段一一对应。
	TaskType        TaskType  `json:"taskType,omitempty"`        // TRANSCODE(默认)|RENDER_EDL|GEN_PROXY
	ProjectID       string    `json:"projectId,omitempty"`       // RenderEDL 关联项目 ID
	ProjectRev      int       `json:"projectRev,omitempty"`      // 提交时刻的项目版本（冻结，03 §4.3）
	PayloadJSON     string    `json:"payloadJson,omitempty"`     // 下发载荷快照（审计与重试依据）
	Checksum        string    `json:"checksum,omitempty"`        // 幂等键（clips+profile 规范化 sha256 前 16 位）
	CredentialID    string    `json:"credentialId,omitempty"`    // 渲染节点凭据档案键（07 §5，替代明文 SMB 口令）
	Stage           string    `json:"stage,omitempty"`           // prepare|segment|concat|mux|finalize
	SegIndex        int       `json:"segIndex,omitempty"`        // 当前分段序号（1-based，0=未开始）
	SegTotal        int       `json:"segTotal,omitempty"`        // 分段总数
	TotalMs         int64     `json:"totalMs,omitempty"`         // Σ(outMs-inMs)，段级进度加权基准
	FastCopyAllowed bool      `json:"fastCopyAllowed,omitempty"` // 同源直通预判（05 §3.1，提交侧计算，仅展示）
	OutTimeMs       int64     `json:"outTimeMs,omitempty"`       // 最近一次进度上报的输出时间（毫秒）
	Speed           string    `json:"speed,omitempty"`           // 最近一次上报的 ffmpeg 倍速
	CooldownReason  string    `json:"cooldownReason,omitempty"`  // 冷却原因（对应 cooldown_reason；到期时间见 CoolDownUntil）
	// ===== P2-1：可观测性（trace ID 贯穿任务全生命周期 + 耗时指标）=====
	TraceID    string     `json:"traceId,omitempty"`    // 任务链路追踪 ID（下发到 FVCS 透传，跨端日志聚合）
	StartedAt  *time.Time `json:"startedAt,omitempty"`  // 首次进入执行态（RUNNING/本地转码）时刻，nil=未开始
	FinishedAt *time.Time `json:"finishedAt,omitempty"` // 终态（完成/失败）时刻，nil=未结束
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// Server 转码服务器。
type Server struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	IP            string     `json:"ip"`
	Port          int        `json:"port"`          // WebSocket 端口
	AuthKey       string     `json:"authKey"`       // 已加密落盘（AES-GCM，enc:v1: 前缀，见 internal/security/crypto.go；内存态保持明文供出站连接使用）
	KeyExpireAt   *time.Time `json:"keyExpireAt"`   // 密钥过期时间
	Status        string     `json:"status"`        // online/offline
	LockExpireSec int        `json:"lockExpireSec"` // 锁自动过期时长（秒）
	IsLocal       bool       `json:"isLocal"`       // 是否为本地转码（使用FVCC自身算力）

	// WSS 数据面加密（改进方向 P1-3 / SECURITY.md §4）：
	// UseWSS=true 时 remote 以 wss:// 拨号；TLSCACert 为 CA 证书 PEM 文件路径
	// （空则用系统根证书池，内网自签 CA 需导入系统信任）；
	// TLSSkipVerify=true 为显式危险开关（默认 false，生产禁止开启）。
	UseWSS        bool   `json:"useWSS"`
	TLSCACert     string `json:"tlsCACert"`
	TLSSkipVerify bool   `json:"tlsSkipVerify"`
}

// Profile 转码方案。
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// 视频参数
	Vcodec      string `json:"vcodec"`      // copy/libx264/libx265/h264_nvenc/...
	Width       int    `json:"width"`       // 0=保持原值
	Height      int    `json:"height"`      // 0=保持原值
	AspectRatio string `json:"aspectRatio"` // scale/crop/fill/stretch
	Fps         string `json:"fps"`         // "original" 或数字字符串
	FpsCustom   int    `json:"fpsCustom"`   // 自定义帧率（fps=custom 时使用）
	RateControl string `json:"rateControl"` // crf/cbr/vbr
	Crf         int    `json:"crf"`         // 0-51
	Bitrate     string `json:"bitrate"`     // kbps，CBR/VBR 模式
	Preset      string `json:"preset"`      // ultrafast~veryslow
	PixFmt      string `json:"pixFmt"`      // yuv420p/yuv422p/...
	Profile     string `json:"profile"`     // baseline/main/high/...
	Tune        string `json:"tune"`        // film/animation/grain/...
	Rotate      int    `json:"rotate"`      // 0/90/180/270
	Gop         int    `json:"gop"`         // GOP 长度，0=自动
	Bframes     int    `json:"bframes"`     // B 帧数量，0=自动
	ScaleAlgo   string `json:"scaleAlgo"`   // bicubic/bilinear/...
	// 视频扩展参数
	VideoMaxRate     int     `json:"videoMaxRate"`     // 最大视频码率 kbps
	VideoBufSize     int     `json:"videoBufSize"`     // 码率缓冲区 kbps
	VideoBrightness  float64 `json:"videoBrightness"`  // 画面亮度 -1.0~1.0，0=原值
	VideoContrast    float64 `json:"videoContrast"`    // 画面对比度 0~2.0，1=原值
	VideoSaturation  float64 `json:"videoSaturation"`  // 色彩饱和度 0~3.0，1=原值
	EnableYadif      bool    `json:"enableYadif"`      // 开启去隔行
	EnableUnsharp    bool    `json:"enableUnsharp"`    // 开启画面锐化
	UnsharpStrength  float64 `json:"unsharpStrength"`  // 锐化强度 0~3，1.0=默认
	ColorSpace       string  `json:"colorSpace"`       // 色彩空间 original/bt709/bt2020/smpte170m
	VideoRefs        int     `json:"videoRefs"`        // 参考帧数量 1-16
	X264AQStrength   float64 `json:"x264AQStrength"`   // x264 自适应量化强度 0~1.5，1.0=默认
	NvencSpatialAQ   bool    `json:"nvencSpatialAQ"`   // NVENC 空间域自适应量化
	NvencTemporalAQ  bool    `json:"nvencTemporalAQ"`  // NVENC 时间域自适应量化
	VideoScThreshold int     `json:"videoScThreshold"` // 场景切换阈值 0-1000，0=自动
	CtuSize          int     `json:"ctuSize"`          // x265 CTU 尺寸 64/32/16
	RdLevel          int     `json:"rdLevel"`          // x265 帧间决策等级 0-6
	// 音频参数
	Acodec             string  `json:"acodec"`             // copy/aac/libmp3lame/...
	Channels           string  `json:"channels"`           // original/1/2/4/6/8
	SampleRate         string  `json:"sampleRate"`         // original/44100/...
	SampleRateCustom   int     `json:"sampleRateCustom"`   // 自定义采样率（sampleRate=custom 时使用）
	AudioBitrate       string  `json:"audioBitrate"`       // original/128/192/...
	AudioBitrateCustom int     `json:"audioBitrateCustom"` // 自定义音频码率（audioBitrate=custom 时使用）
	SampleFmt          string  `json:"sampleFmt"`          // original/s16/s32/flt/fltp
	AacProfile         string  `json:"aacProfile"`         // original/lc/he/he_v2
	Volume             float64 `json:"volume"`             // 音量倍率，1.0=原值
	Silence            bool    `json:"silence"`            // 是否去除静音
	// 音频扩展参数
	AudioDynNorm    bool `json:"audioDynNorm"`    // 自动音量归一化
	AudioCutoff     int  `json:"audioCutoff"`     // 音频高频截止频率 Hz
	OpusCompLevel   int  `json:"opusCompLevel"`   // Opus 压缩级别 0-10
	AudioSyncOffset int  `json:"audioSyncOffset"` // 音视频同步偏移 ms
	// 高级封装参数（永久显示）
	HwAccel      string `json:"hwAccel"`      // 硬件解码模式 original/auto/cuda/qsv/d3d11va/none
	MovFastStart bool   `json:"movFastStart"` // MP4 网页快速播放
	ThreadCount  string `json:"threadCount"`  // 编码线程数量 auto/1/2/4/6/8/12/16
	// 其他
	ExtraArgs        string `json:"extraArgs"`        // 额外 FFmpeg 参数
	CustomFfmpeg     bool   `json:"customFfmpeg"`     // 自定义 FFmpeg 参数模式
	CustomFfmpegArgs string `json:"customFfmpegArgs"` // 自定义 FFmpeg 参数（启用时直接使用）
	OutputSuffix     string `json:"outputSuffix"`     // 默认输出后缀
	QualityControl   string `json:"qualityControl"`   // quality/bitrate 质量控制模式
	ConstantQuality  int    `json:"constantQuality"`  // 恒定质量值（CRF/CQ）
	OutputPath       string `json:"outputPath"`       // 转换文件另存为路径
	DeleteSource     bool   `json:"deleteSource"`     // 转码完成后删除源文件
}

// Lock 服务器锁（传输锁 + 转码锁）。
type Lock struct {
	ServerID  string     `json:"serverId"`
	TransLock *LockEntry `json:"transLock"` // 传输锁（上传/下载）
	CodeLock  *LockEntry `json:"codeLock"`  // 转码锁
}

// LockEntry 锁条目。
type LockEntry struct {
	TaskID       string    `json:"taskId"`
	LockExpireAt time.Time `json:"lockExpireAt"`
}

type StreamInfo struct {
	Index         int               `json:"index"`
	CodecType     string            `json:"codecType"`
	CodecName     string            `json:"codecName"`
	CodecLongName string            `json:"codecLongName"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	RFrameRate    string            `json:"rFrameRate"`
	BitRate       string            `json:"bitRate"`
	SampleRate    string            `json:"sampleRate"`
	Channels      int               `json:"channels"`
	ChannelLayout string            `json:"channelLayout"`
	Tags          map[string]string `json:"tags"`
}

// VideoInfo ffprobe 提取的视频元数据。
type VideoInfo struct {
	Path         string       `json:"path"`
	FileName     string       `json:"fileName"`
	Format       string       `json:"format"` // 文件扩展名（如 mp4, mkv）
	Size         int64        `json:"size"`
	Duration     float64      `json:"duration"`   // 秒
	Resolution   string       `json:"resolution"` // 1920x1080
	Width        int          `json:"width"`
	Height       int          `json:"height"`
	Codec        string       `json:"codec"`   // 视频编码
	Bitrate      string       `json:"bitrate"` // 视频码率
	Fps          string       `json:"fps"`
	AudioCodec   string       `json:"audioCodec"`
	AudioBitrate string       `json:"audioBitrate"`
	SampleRate   string       `json:"sampleRate"`  // 音频采样率
	Channels     int          `json:"channels"`    // 音频声道数
	StreamCount  int          `json:"streamCount"` // 流数量
	Probed       bool         `json:"probed"`      // 是否已提取
	Streams      []StreamInfo `json:"streams"`     // 完整流信息
}

// ===== 配置文件根结构 =====

type TasksFile struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

type HistoryFile struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"` // 保留最近 1000 条
}

type ServersFile struct {
	Version int      `json:"version"`
	Servers []Server `json:"servers"`
}

type ProfilesFile struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

type LocksFile struct {
	Version int    `json:"version"`
	Locks   []Lock `json:"locks"`
}

type VideoInfoCache struct {
	Path         string       `json:"path"`
	FileName     string       `json:"fileName"`
	Format       string       `json:"format"`
	Size         int64        `json:"size"`
	Duration     float64      `json:"duration"`
	Resolution   string       `json:"resolution"`
	Width        int          `json:"width"`
	Height       int          `json:"height"`
	Codec        string       `json:"codec"`
	Bitrate      string       `json:"bitrate"`
	Fps          string       `json:"fps"`
	AudioCodec   string       `json:"audioCodec"`
	AudioBitrate string       `json:"audioBitrate"`
	SampleRate   string       `json:"sampleRate"`
	Channels     int          `json:"channels"`
	StreamCount  int          `json:"streamCount"`
	Probed       bool         `json:"probed"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	Streams      []StreamInfo `json:"streams"`
}

// ToVideoInfo 将缓存条目转换为面向展示的 VideoInfo。
// 收敛混乱报告 §2.3 证据 3：doScanDirectory 中 cache→VideoInfo 30+ 字段逐字段复制的重复块。
func (c VideoInfoCache) ToVideoInfo() VideoInfo {
	return VideoInfo{
		Path:         c.Path,
		FileName:     c.FileName,
		Format:       c.Format,
		Size:         c.Size,
		Duration:     c.Duration,
		Resolution:   c.Resolution,
		Width:        c.Width,
		Height:       c.Height,
		Codec:        c.Codec,
		Bitrate:      c.Bitrate,
		Fps:          c.Fps,
		AudioCodec:   c.AudioCodec,
		AudioBitrate: c.AudioBitrate,
		SampleRate:   c.SampleRate,
		Channels:     c.Channels,
		StreamCount:  c.StreamCount,
		Probed:       c.Probed,
		Streams:      c.Streams,
	}
}

// ToCache 将探测得到的 VideoInfo 转为可持久化的缓存条目。
// UpdatedAt 由 store.UpsertVideoCache 写入时统一赋值，转换时保持零值。
func (v VideoInfo) ToCache() VideoInfoCache {
	return VideoInfoCache{
		Path:         v.Path,
		FileName:     v.FileName,
		Format:       v.Format,
		Size:         v.Size,
		Duration:     v.Duration,
		Resolution:   v.Resolution,
		Width:        v.Width,
		Height:       v.Height,
		Codec:        v.Codec,
		Bitrate:      v.Bitrate,
		Fps:          v.Fps,
		AudioCodec:   v.AudioCodec,
		AudioBitrate: v.AudioBitrate,
		SampleRate:   v.SampleRate,
		Channels:     v.Channels,
		StreamCount:  v.StreamCount,
		Probed:       v.Probed,
		Streams:      v.Streams,
	}
}

type VideoCacheFile struct {
	Version int              `json:"version"`
	Entries []VideoInfoCache `json:"entries"`
}

// 默认设置兜底值（saveSettings 校验 / DefaultSettings / scheduler 分片同源引用，
// 消除散落硬编码；见混乱报告 §2.5 证据 3）
const (
	DefaultSchedulerIntervalSec = 1    // 调度器轮询间隔（秒）
	DefaultChunkSizeMB          = 4    // 上传分片大小（MB）
	DefaultHistoryLimit         = 1000 // 历史记录保留条数
)

// Settings 应用全局设置（可运行时修改并持久化）。
type Settings struct {
	SchedulerIntervalSec   int      `json:"schedulerIntervalSec"`   // 调度器轮询间隔（秒）
	ChunkSizeMB            int      `json:"chunkSizeMB"`            // 上传分片大小（MB）
	MaxRetry               int      `json:"maxRetry"`               // 任务最大重试次数
	HistoryLimit           int      `json:"historyLimit"`           // 历史记录保留条数
	OutputSuffix           string   `json:"outputSuffix"`           // 默认输出后缀
	AccessiblePaths        []string `json:"accessiblePaths"`        // 手动配置的额外授权目录
	TransferMode           string   `json:"transferMode"`           // 传输模式: http/smb
	SMBSharePath           string   `json:"smbSharePath"`           // SMB共享路径
	SMBUser                string   `json:"smbUser"`                // SMB用户名
	SMBPassword            string   `json:"smbPassword"`            // SMB密码
	LogLevel               string   `json:"logLevel"`               // 日志级别: DEBUG/INFO/WARN/ERROR/FATAL
	MaxLocalTranscodeCount int      `json:"maxLocalTranscodeCount"` // 本地最大并发转码数
	// 2026-09-16 修复⑤：剪辑页预览器默认静音（指针类型，nil 表示旧设置文件未配置 → 按 true 处理）
	PlayerMuted *bool `json:"playerMuted,omitempty"`
	// ===== B-04：EDL 素材根/成品根（03 §2.3 / 09 §4.2）=====
	VideoRoot  string `json:"videoRoot"`  // 素材根（对应 payload.sourceRoot，默认 /media/videos）
	ExportRoot string `json:"exportRoot"` // 成品根（对应 payload.destRoot，默认 /media/exports）
	// ===== M4：预览网关代理根 / 缩略图缓存根（04 §2.4、§2.6）=====
	// 均非设置页表单字段：为空时按 videoRoot 推导，避免被前端覆盖成空（omitempty 保证旧设置文件兼容）。
	ProxyRoot string `json:"proxyRoot,omitempty"` // 代理根（默认 <videoRoot>/_proxy）
	CacheRoot string `json:"cacheRoot,omitempty"` // 缩略图缓存根（默认 <videoRoot>/_wve_cache）
}

// DefaultSettings 返回默认设置。
func DefaultSettings() Settings {
	return Settings{
		SchedulerIntervalSec:   DefaultSchedulerIntervalSec,
		ChunkSizeMB:            DefaultChunkSizeMB,
		MaxRetry:               3,
		HistoryLimit:           DefaultHistoryLimit,
		OutputSuffix:           "_trans.mp4",
		TransferMode:           "http",
		SMBSharePath:           "",
		SMBUser:                "",
		SMBPassword:            "",
		LogLevel:               "INFO",
		MaxLocalTranscodeCount: 1,
		PlayerMuted:            BoolPtr(true), // 修复⑤：默认静音（保持既有行为）
		VideoRoot:              "/media/videos",  // 03 §2.3
		ExportRoot:             "/media/exports", // 03 §2.3（缺省可回落 SourceRoot/_exports）
	}
}

type SettingsFile struct {
	Version  int      `json:"version"`
	Settings Settings `json:"settings"`
}

// BoolPtr 返回布尔指针（修复⑤：Settings.PlayerMuted 用指针区分「未配置」与「显式 false」）。
func BoolPtr(b bool) *bool { return &b }

// ===== EDL 项目模型（03 §2.2）=====

// Timeline 时间线输出参数（03 §2.2；与 FVCS/pkg/protocol.Timeline 同形）。
type Timeline struct {
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	FPS        int  `json:"fps"`        // 恒定帧率，默认 30
	SampleRate int  `json:"sampleRate"` // 默认 48000
	Audio      bool `json:"audio"`      // P0 恒为 true（P0 不做纯视频导出）
}

// Transition 片段间转场。P0 必须为 nil，仅占位以固化 JSON 形状（03 §2.1）。
type Transition struct {
	Type       string `json:"type"`       // [P2+] 才启用
	DurationMs int    `json:"durationMs"` // [P2+] 才启用
}

// EDLClip 时间线片段（03 §2.2）。inMs/outMs 为源素材时间轴上的整数毫秒，outMs 为开区间。
type EDLClip struct {
	ClipID           string      `json:"clipId"`               // 'c_<8hex>'
	AssetID          string      `json:"assetId"`              // 'a_<8hex>'
	File             string      `json:"file"`                 // 相对 SourceRoot 的 POSIX 路径
	InMs             int64       `json:"inMs"`                 // 入点
	OutMs            int64       `json:"outMs"`                // 出点（开区间）
	Speed            float64     `json:"speed"`                // P0 恒为 1.0
	SourceDurationMs int64       `json:"sourceDurationMs"`     // 打点时的素材时长快照
	Transition       *Transition `json:"transition,omitempty"` // P0 恒 nil
}

// Project EDL 项目（03 §2.2 契约字段 / 06 §2.1 projects 表）。
// 契约字段严格对齐 03 §2.2；派生字段以 json:"-" 排除，仅供服务端内部使用。
type Project struct {
	ID        string    `json:"id"` // 'p_<8hex>'
	Name      string    `json:"name"`
	Rev       int       `json:"rev"` // 乐观锁版本，从 1 开始，成功保存后 +1
	Timeline  Timeline  `json:"timeline"`
	Clips     []EDLClip `json:"clips"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// 派生字段（03 §2.2）：不参与落盘，读取时由 normalizeProject 重算。
	ClipCount int   `json:"clipCount"`
	TotalMs   int64 `json:"totalMs"`
	// 随项目 JSON 落盘的内部字段（03 §7）。
	SchemaVer        int    `json:"schemaVer,omitempty"`
	LastRenderTaskID string `json:"lastRenderTaskId,omitempty"`
}

// ProjectSummary 项目列表轻量视图（03 §2.2）。
type ProjectSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Rev       int       `json:"rev"`
	ClipCount int       `json:"clipCount"`
	UpdatedAt time.Time `json:"updatedAt"`
	// 海报帧（动效改进 2026-09-20）：取首片段 file+inMs，供项目选择页卡片式缩略图复用 /thumb 抽帧。
	// 无片段时省略，前端回退到图标占位。
	PosterFile string `json:"posterFile,omitempty"`
	PosterMs   int64  `json:"posterMs,omitempty"`
}

// WireClip 线协议片段（03 §2.2 / §3；in/out 为 "HH:MM:SS.mmm"）。
type WireClip struct {
	File  string  `json:"file"`  // 相对 SourceRoot 的 POSIX 路径
	In    string  `json:"in"`    // "HH:MM:SS.mmm"
	Out   string  `json:"out"`   // "HH:MM:SS.mmm"
	Speed float64 `json:"speed"` // P0 恒 1.0
}

// RenderVideoProfile 渲染视频参数（与 FVCS/pkg/protocol.RenderVideoProfile 同形）。
type RenderVideoProfile struct {
	Codec   string `json:"codec"`
	CRF     int    `json:"crf"`
	Preset  string `json:"preset"`
	PixFmt  string `json:"pixFmt"`
	Bitrate string `json:"bitrate,omitempty"` // [P1+] 预留，P0 存在即拒绝
}

// RenderAudioProfile 渲染音频参数（与 FVCS/pkg/protocol.RenderAudioProfile 同形）。
type RenderAudioProfile struct {
	Codec      string `json:"codec"` // P0 恒为 aac
	Bitrate    string `json:"bitrate"`
	Channels   int    `json:"channels"`
	SampleRate int    `json:"sampleRate"`
}

// RenderProfile 渲染方案（03 §2.2 profile）。
type RenderProfile struct {
	PresetKey       string             `json:"presetKey"` // 必须命中枚举表（05 §5.2）
	Container       string             `json:"container"` // P0 恒为 mp4
	Video           RenderVideoProfile `json:"video"`
	Audio           RenderAudioProfile `json:"audio"`
	FastCopyAllowed bool               `json:"fastCopyAllowed"` // 服务端计算后回填，前端仅展示
}

// RenderTaskPayload 渲染任务下发载荷（03 §2.2 / §3.2，FVCC → FVCS）。
type RenderTaskPayload struct {
	Type       string        `json:"type"` // 恒为 "RenderEDL"
	ProjectID  string        `json:"projectId"`
	ProjectRev int           `json:"projectRev"`
	SourceRoot string        `json:"sourceRoot"` // 相对共享根的素材根
	DestRoot   string        `json:"destRoot"`
	Output     string        `json:"output"` // 相对 DestRoot，P0 强制 .mp4
	Timeline   Timeline      `json:"timeline"`
	Clips      []WireClip    `json:"clips"`
	Profile    RenderProfile `json:"profile"`
	TotalMs    int64         `json:"totalMs"`
	Checksum   string        `json:"checksum"` // clips+profile 规范化 sha256 前 16 位
}

// ===== 渲染节点模型（06 §2.1 / §5）=====

// GPUInfo 节点 GPU 能力。
type GPUInfo struct {
	Vendor   string   `json:"vendor"`   // nvidia|amd|intel
	Name     string   `json:"name"`     // RTX 4070
	Encoders []string `json:"encoders"` // h264_nvenc|hevc_nvenc|...
}

// NodeCaps 渲染节点能力快照（服务端权威，对应 node_caps 表）。
type NodeCaps struct {
	ServerID      string    `json:"serverId"`
	AgentVersion  string    `json:"agentVersion"`
	OS            string    `json:"os"`
	CPUCores      int       `json:"cpuCores"`
	GPU           []GPUInfo `json:"gpu"`
	Encoders      []string  `json:"encoders"`      // ffmpeg -encoders 探测结果
	MaxConcurrent int       `json:"maxConcurrent"` // 最大并发任务数
	FFmpegPath    string    `json:"ffmpegPath"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// NodeHealthSample 节点健康采样（对应 node_health_samples 表，保留最近 N 条）。
type NodeHealthSample struct {
	ID        int64     `json:"id"`
	ServerID  string    `json:"serverId"`
	SampledAt time.Time `json:"sampledAt"`
	Online    bool      `json:"online"`
	Running   int       `json:"running"` // 在跑任务数
	CPUPct    float64   `json:"cpuPct"`
	MemPct    float64   `json:"memPct"`
	FailCount int       `json:"failCount"` // 该采样窗口内失败数
}

// AuditEntry 审计日志条目（07 §5.4：凭据与权限相关操作单独落盘）。
type AuditEntry struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`  // 操作者标识（UID / username）
	Action string    `json:"action"` // 如 credential.create / project.delete
	Target string    `json:"target"` // 目标对象标识
	Detail string    `json:"detail"` // 脱敏后的补充说明（日志中禁止出现明文口令）
	Result string    `json:"result"` // ok|denied|error
}

// ===== 新增持久化文件根结构（与既有 tasks.json 等同风格）=====

type ProjectsFile struct {
	Version  int       `json:"version"`
	Projects []Project `json:"projects"`
}

type NodeCapsFile struct {
	Version int        `json:"version"`
	Items   []NodeCaps `json:"items"`
}

type NodeHealthFile struct {
	Version int                `json:"version"`
	Samples []NodeHealthSample `json:"samples"`
}

type AuditLogFile struct {
	Version int          `json:"version"`
	Entries []AuditEntry `json:"entries"`
}

// ===== M4：代理映射（04 §4.2）=====

// AssetProxy 素材代理映射，落 dataDir/asset_proxies.json（本仓库 Store 为 JSON 文件持久化）。
// assetKey 为主键，语义与 04 §4.2 的 asset_key 一致（相对 SourceRoot 的 POSIX 路径）。
type AssetProxy struct {
	AssetKey        string `json:"assetKey"`        // 相对 SourceRoot 的 POSIX 路径（含文件名）
	ProxyRel        string `json:"proxyRel"`        // 相对 ProxyRoot 的代理路径
	SrcDurationMs   int64  `json:"srcDurationMs"`   // 源时长（ms）
	ProxyDurationMs int64  `json:"proxyDurationMs"` // 代理时长（ms）
	SrcFPS          string `json:"srcFps"`          // 原始分数形式，如 "30000/1001"
	ProxyFPS        string `json:"proxyFps"`
	Mode            string `json:"mode"`  // full | keyframe_only
	State           string `json:"state"` // ready | invalid | stale
	TaskID          string `json:"taskId,omitempty"`
	GeneratedAt     string `json:"generatedAt"`
	LastAccessAt    string `json:"lastAccessAt,omitempty"`
	SizeBytes       int64  `json:"sizeBytes"`
}

// 代理模式（04 §4.2 mode）。
const (
	ProxyModeFull         = "full"
	ProxyModeKeyframeOnly = "keyframe_only"
)

// 代理状态（04 §4.2 state / §4.3）。
const (
	ProxyStateReady   = "ready"
	ProxyStateInvalid = "invalid"
	ProxyStateStale   = "stale"
)

// AssetProxiesFile asset_proxies.json 根结构（与既有 tasks.json 等同风格）。
type AssetProxiesFile struct {
	Version int          `json:"version"`
	Items   []AssetProxy `json:"items"`
}

// ===== M4：代理生成载荷（04 §3.2，FVCC → FVCS）=====

// ProxyTemplate 代理编码参数模板（与 FVCS/pkg/protocol.ProxyTemplate 同形，04 §3.2/§3.3）。
type ProxyTemplate struct {
	PresetKey    string `json:"presetKey"`
	Height       int    `json:"height"`
	FPSFollowSrc bool   `json:"fpsFollowSource"`
	CRF          int    `json:"crf"`
	Preset       string `json:"preset"`
	AudioBitrate string `json:"audioBitrate"`
	GOPSec       int    `json:"gopSeconds"`
}

// GenProxyPayload 代理生成载荷（04 §3.2）。
type GenProxyPayload struct {
	Type      string        `json:"type"` // 恒为 "GenProxy"
	AssetID   string        `json:"assetId"`
	SrcFile   string        `json:"srcFile"`   // 相对素材根（SourceRoot）的 POSIX 路径
	ProxyFile string        `json:"proxyFile"` // 相对代理根（ProxyRoot=共享根/_proxy）的 POSIX 路径，如 demo/a_01.proxy.mp4
	Template  ProxyTemplate `json:"template"`
	// SourceRoot 本次任务实际使用的素材根（修复①：剪辑页支援全部授权目录）。
	// 为空表示沿用缺省素材根（Settings.videoRoot 的兼容口径）；非空时：
	//   - FVCS 挂载该根的共享（SMBPath = toSMBUNC(SourceRoot)）；
	//   - 代理落盘根为 <SourceRoot>/_proxy（与提交侧、校验侧同口径）。
	SourceRoot string `json:"sourceRoot,omitempty"`
}

// DefaultProxyTemplate 代理参数模板默认值（需求6：默认走 GPU 硬编 h264_nvenc；
// 远端 FVCS 无 NVIDIA 编码器时自动降级 libx264，见 FVCS proxy_flow.go）。
func DefaultProxyTemplate() ProxyTemplate {
	return ProxyTemplate{
		PresetKey:    "proxy_720p_nvenc",
		Height:       720,
		FPSFollowSrc: true,
		CRF:          23,
		Preset:       "p5",
		AudioBitrate: "128k",
		GOPSec:       2,
	}
}
