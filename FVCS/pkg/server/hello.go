package server

import (
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/ffmpeg"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/task"
	"Fnos.VC_Service/pkg/version"
)

// 节点能力上报（06 §5.1）：调度端（FVCC）连接后发 Hello，节点回包能力清单。
// 旧版节点不回 supportsRenderEDL / supportsGenProxy，调度端据此判定其不可承接 EDL 任务（06 §7）。

// agentVersion 节点版本号（供调度端做能力/兼容性判断）。
// 唯一来源为 pkg/version（编译期由 go:embed 读入 VERSION 文件），
// 禁止在此另写版本字面量，避免与交付包版本号漂移（历史缺陷：硬编码 1.2.0 vs 包版本 1.2.1）。
var agentVersion = version.Version

// handleHello 处理调度端 Hello 请求，返回节点能力（含 GPU/编码器/在跑任务）
func (c *ClientConnection) handleHello(req *protocol.WebSocketRequest) error {
	data := buildHelloData()
	resp, _ := protocol.BuildSuccessResponse(data)
	c.safeSend(resp)

	logger.Info("server", "Hello handled for client %s: encoders=%d, running=%d",
		c.ClientID, len(data.Encoders), len(data.Running))
	return nil
}

// pushHello 认证成功后由节点主动推送能力上报（06 §5.1，A-07 端到端联调补强）。
//
// 背景：调度端（FVCC `server/remote.go::readPump`）按「推送」语义识别能力上报——
// 仅当收到 `Cmd == "Hello"` 且 Data 非空的应用层帧时才解析并落库 node_caps；
// 节点侧此前只在被请求时回包，认证成功后不再有 Hello 交互，能力无法上报。
// 故此处复用 buildHelloData() 的采集口径主动推送一帧 Hello。
// 载荷字段与 FVCC `parseHelloCaps` 的解析口径一致（camelCase：
// agentVersion / os / cpuCores / maxConcurrent / ffmpegPath / gpu / encoders / running），
// 其中 agentVersion 非空即视为有效上报。
func (c *ClientConnection) pushHello() {
	data := buildHelloData()
	frame, err := buildHelloFrame(data)
	if err != nil {
		logger.Warn("server", "Hello 推送构造失败: client=%s err=%v", c.ClientID, err)
		return
	}

	c.safeSend(frame)

	logger.Info("server", "Hello pushed to client %s: agentVersion=%s encoders=%d running=%d",
		c.ClientID, data.AgentVersion, len(data.Encoders), len(data.Running))
}

// buildHelloFrame 构造 Hello 主动推送帧。
// 线上格式为节点侧统一的应用层封装 {Cmd, Data}（与 Progress 推送同形），
// Cmd 必须为 "Hello"、Data 为非空能力载荷，否则调度端会按普通响应丢弃（不落库）。
func buildHelloFrame(data protocol.HelloResponseData) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(protocol.WebSocketRequest{
		Cmd:  string(protocol.CmdHello),
		Data: payload,
	})
}

// buildHelloData 采集本机能力（编码器探测失败不阻塞上报，仅返回空列表）
func buildHelloData() protocol.HelloResponseData {
	cfg := config.Get()
	ffmpegPath := ""
	maxConcurrent := 0
	if cfg != nil {
		ffmpegPath = cfg.FFmpegPath
		maxConcurrent = cfg.MaxConcurrentTasks
	}

	encoders := ffmpeg.ListEncoders(ffmpegPath)
	gpus := ffmpeg.DetectGPUs(ffmpegPath)

	data := protocol.HelloResponseData{
		AgentVersion:      agentVersion,
		OS:                detectOSName(),
		CPUCores:          runtime.NumCPU(),
		MaxConcurrent:     maxConcurrent,
		FFmpegPath:        ffmpegPath,
		GPU:               gpus,
		Encoders:          encoders,
		Running:           collectRunningTasks(),
		SupportsRenderEDL: true,
		SupportsGenProxy:  true,
	}
	if data.GPU == nil {
		data.GPU = []protocol.GPUInfo{}
	}
	if data.Encoders == nil {
		data.Encoders = []string{}
	}
	return data
}

// collectRunningTasks 汇总节点当前在跑任务（06 §5.1 上线补报对账用）
func collectRunningTasks() []protocol.RunningTaskInfo {
	out := []protocol.RunningTaskInfo{}
	for _, t := range task.GetAllTasks() {
		if t == nil {
			continue
		}
		if t.Status != task.StatusTranscoding && t.Status != task.StatusWaiting && t.Status != task.StatusPaused {
			continue
		}
		out = append(out, protocol.RunningTaskInfo{
			TaskId:   t.TaskID,
			TaskType: t.TaskType,
			Progress: t.Progress,
			Stage:    t.Stage,
		})
	}
	return out
}

// detectOSName 读取系统描述（失败时回退运行时标识）
func detectOSName() string {
	if runtime.GOOS == "windows" {
		for _, args := range [][]string{
			{"powershell", "-NoProfile", "-NonInteractive", "-Command",
				"(Get-CimInstance Win32_OperatingSystem).Caption + ' ' + (Get-CimInstance Win32_OperatingSystem).Version"},
			{"cmd", "/c", "ver"},
		} {
			out, err := exec.Command(args[0], args[1:]...).Output()
			if err != nil {
				continue
			}
			name := strings.TrimSpace(string(out))
			if name != "" {
				return name
			}
		}
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}
