package server

import (
	"testing"

	"Fnos.VC_Service/pkg/version"
)

// 回归用例（2026-09-16）：Hello 上报的 agentVersion 只允许来自 pkg/version 单一来源。
// 一旦有人在 hello.go 里重新写死版本字面量，本用例即失败。
func TestAgentVersionSingleSource(t *testing.T) {
	if agentVersion != version.Version {
		t.Fatalf("agentVersion(%q) 必须等于 pkg/version.Version(%q)，禁止在 hello.go 另写版本字面量",
			agentVersion, version.Version)
	}
	if agentVersion == "" {
		t.Fatal("agentVersion 为空：调度端 parseHelloCaps 会判定载荷无效并忽略")
	}
}
