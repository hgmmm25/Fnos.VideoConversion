package remote

// P2-1 B轮：Hello 能力上报载荷解析测试（06 §5.1）随实现迁入 internal/remote。
// camelCase / snake_case / 嵌套三种形态兼容；无能力信息或非法 JSON → 忽略。

import (
	"encoding/json"
	"testing"
)

func TestB08ParseHelloCaps(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantID  string
		wantVer string
		wantMax int
	}{
		{"camelCase", `{"serverId":"srv9","agentVersion":"1.2.0","maxConcurrent":4,"encoders":["h264_nvenc"]}`, "srv9", "1.2.0", 4},
		{"snakeCase", `{"agent_version":"1.3.0","max_concurrent":2,"encoders":["libx264"],"cpu_cores":8}`, "srv_sock", "1.3.0", 2},
		{"nestedCaps", `{"caps":{"agentVersion":"1.1.0","maxConcurrent":6}}`, "srv_sock", "1.1.0", 6},
		{"nestedSnake", `{"nodeCaps":{"agent_version":"1.0.5","max_concurrent":5}}`, "srv_sock", "1.0.5", 5},
	}
	for _, tc := range cases {
		caps, ok := ParseHelloCaps("srv_sock", json.RawMessage(tc.raw))
		if !ok {
			t.Fatalf("%s: 应解析成功 (%s)", tc.name, tc.raw)
		}
		if caps.ServerID != tc.wantID || caps.AgentVersion != tc.wantVer || caps.MaxConcurrent != tc.wantMax {
			t.Fatalf("%s: 解析结果不符: %+v", tc.name, caps)
		}
	}

	// 无能力信息 / 非法 JSON → 忽略（不落库）。
	for _, raw := range []string{`{}`, `{"foo":1}`, `not-json`} {
		if _, ok := ParseHelloCaps("srv1", json.RawMessage(raw)); ok {
			t.Fatalf("载荷 %s 不应被识别为有效能力", raw)
		}
	}
}
