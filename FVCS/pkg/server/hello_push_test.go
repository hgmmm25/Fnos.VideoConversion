package server

import (
	"encoding/json"
	"testing"

	"Fnos.VC_Service/pkg/protocol"
)

// TestHelloPushFrameContract 校验「认证后主动推送的 Hello 帧」与调度端解析口径一致：
// Cmd 必须为 "Hello"、Data 必须是非空 JSON 对象且携带能力字段
// （对应 FVCC `server/remote.go::readPump` 的 `resp.Cmd == "Hello" && len(resp.Data) > 0`
// 判定与 `parseHelloCaps` 解析，06 §5.1）。
func TestHelloPushFrameContract(t *testing.T) {
	data := protocol.HelloResponseData{
		AgentVersion:      agentVersion,
		OS:                "windows",
		CPUCores:          8,
		MaxConcurrent:     2,
		FFmpegPath:        `D:\ffmpeg\ffmpeg.exe`,
		GPU:               []protocol.GPUInfo{{Vendor: "nvidia", Name: "RTX 4070", Encoders: []string{"h264_nvenc"}}},
		Encoders:          []string{"h264_nvenc", "libx264"},
		SupportsRenderEDL: true,
		SupportsGenProxy:  true,
	}

	frame, err := buildHelloFrame(data)
	if err != nil {
		t.Fatalf("buildHelloFrame 失败: %v", err)
	}

	var wire struct {
		Cmd  string          `json:"Cmd"`
		Data json.RawMessage `json:"Data"`
	}
	if err := json.Unmarshal(frame, &wire); err != nil {
		t.Fatalf("Hello 帧非法 JSON: %v", err)
	}
	if wire.Cmd != "Hello" {
		t.Fatalf("Cmd 期望 Hello，实际 %q", wire.Cmd)
	}
	if len(wire.Data) == 0 {
		t.Fatal("Data 为空：调度端会丢弃该能力上报")
	}
	if wire.Data[0] != '{' {
		t.Fatalf("Data 必须是 JSON 对象（parseHelloCaps 按对象解析）：%s", string(wire.Data))
	}

	var caps protocol.HelloResponseData
	if err := json.Unmarshal(wire.Data, &caps); err != nil {
		t.Fatalf("Data 解析失败: %v", err)
	}
	if caps.AgentVersion == "" {
		t.Error("agentVersion 为空：调度端 parseHelloCaps 会判定载荷无效并忽略")
	}
	if !caps.SupportsRenderEDL || !caps.SupportsGenProxy {
		t.Error("能力位缺失：supportsRenderEDL / supportsGenProxy 应为 true")
	}
	if len(caps.Encoders) == 0 || len(caps.GPU) == 0 {
		t.Error("编码器 / GPU 能力缺失")
	}
}

// TestHelloPushFrameFieldNames 锁定线上字段名：FVCC 的 NodeCaps 采用 camelCase，
// 若改为 snake_case 则该分支解析不到，能力会丢失。
func TestHelloPushFrameFieldNames(t *testing.T) {
	frame, err := buildHelloFrame(protocol.HelloResponseData{AgentVersion: agentVersion, CPUCores: 4})
	if err != nil {
		t.Fatalf("buildHelloFrame 失败: %v", err)
	}

	var wire struct {
		Cmd  string                     `json:"Cmd"`
		Data map[string]json.RawMessage `json:"Data"`
	}
	if err := json.Unmarshal(frame, &wire); err != nil {
		t.Fatalf("帧解析失败: %v", err)
	}

	for _, key := range []string{
		"agentVersion", "os", "cpuCores", "maxConcurrent", "ffmpegPath",
		"gpu", "encoders", "running", "supportsRenderEDL", "supportsGenProxy",
	} {
		if _, ok := wire.Data[key]; !ok {
			t.Errorf("能力字段缺失: %s", key)
		}
	}
}
