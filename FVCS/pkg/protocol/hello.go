package protocol

// 节点注册与能力上报（06 §5.1）
//
// 说明：FVCS 为渲染节点侧 WS 服务端，注册由调度端（FVCC）发起 `Hello` 请求，
// 节点以本文件定义的载荷回包；`supportsRenderEDL` / `supportsGenProxy` 能力位
// 供调度端判定该节点是否可承接结构化剪辑任务（06 §7：旧版节点缺字段即视为不支持）。

// CmdHello 节点能力上报命令
const CmdHello CmdType = "Hello"

// GPUInfo 单张显卡及其可用硬编编码器
type GPUInfo struct {
	Vendor   string   `json:"vendor"`
	Name     string   `json:"name"`
	Encoders []string `json:"encoders"`
}

// RunningTaskInfo 节点当前在跑任务（用于调度端对账，06 §5.1 上线补报）
type RunningTaskInfo struct {
	TaskId   string  `json:"taskId"`
	TaskType string  `json:"taskType,omitempty"`
	Progress float64 `json:"progress"`
	Stage    string  `json:"stage,omitempty"`
}

// HelloResponseData Hello 回包载荷
type HelloResponseData struct {
	AgentVersion  string            `json:"agentVersion"`
	OS            string            `json:"os"`
	CPUCores      int               `json:"cpuCores"`
	MaxConcurrent int               `json:"maxConcurrent"`
	FFmpegPath    string            `json:"ffmpegPath"`
	GPU           []GPUInfo         `json:"gpu"`
	Encoders      []string          `json:"encoders"`
	Running       []RunningTaskInfo `json:"running"`

	// 能力位（06 §7）
	SupportsRenderEDL bool `json:"supportsRenderEDL"`
	SupportsGenProxy  bool `json:"supportsGenProxy"`
}
