package version

import (
	"regexp"
	"strings"
	"testing"
)

// 回归用例（2026-09-16）：节点上报版本必须来自 VERSION 文件（go:embed），且为合法三段式版本号。
// 历史缺陷：pkg/server/hello.go 硬编码 agentVersion="1.2.0"，而交付包已是 1.2.1，
// 导致调度端（FVCC）拿到的节点版本与真实运行版本不一致，能力/兼容性判断失准。
func TestVersionEmbeddedFromFile(t *testing.T) {
	if strings.TrimSpace(Version) == "" {
		t.Fatal("Version 为空：pkg/version/VERSION 缺失或 go:embed 未生效")
	}
	if Version != strings.TrimSpace(Version) {
		t.Fatalf("Version 含首尾空白未清理: %q", Version)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("Version 非三段式版本号（应为 x.y.z）: %q", Version)
	}
	if Version != "1.2.6" {
		t.Fatalf("Version=%q 与本次交付包版本 1.2.6 不一致：出新包时请同步修改 pkg/version/VERSION", Version)
	}
}
