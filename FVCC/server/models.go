package main

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
)

// IsTerminal 判断是否终态。
func (s TaskStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled
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
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// Server 转码服务器。
type Server struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	IP            string     `json:"ip"`
	Port          int        `json:"port"`          // WebSocket 端口
	AuthKey       string     `json:"authKey"`       // MVP 明文存储，TODO: P0 AES 加密
	KeyExpireAt   *time.Time `json:"keyExpireAt"`   // 密钥过期时间
	Status        string     `json:"status"`        // online/offline
	LockExpireSec int        `json:"lockExpireSec"` // 锁自动过期时长（秒）
	IsLocal       bool       `json:"isLocal"`       // 是否为本地转码（使用FVCC自身算力）
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

type VideoCacheFile struct {
	Version int              `json:"version"`
	Entries []VideoInfoCache `json:"entries"`
}

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
}

// DefaultSettings 返回默认设置。
func DefaultSettings() Settings {
	return Settings{
		SchedulerIntervalSec:   1,
		ChunkSizeMB:            4,
		MaxRetry:               3,
		HistoryLimit:           1000,
		OutputSuffix:           "_trans.mp4",
		TransferMode:           "http",
		SMBSharePath:           "",
		SMBUser:                "",
		SMBPassword:            "",
		LogLevel:               "INFO",
		MaxLocalTranscodeCount: 1,
	}
}

type SettingsFile struct {
	Version  int      `json:"version"`
	Settings Settings `json:"settings"`
}
