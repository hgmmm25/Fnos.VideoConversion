package version

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// 回归用例（2026-09-16，与 FVCS/pkg/version/version_test.go 同构）：
// 二进制内版本必须来自 VERSION 文件（go:embed），并与交付包相关版本号保持一致。
// 历史缺陷：appVer 曾硬编码 "1.0.0"，而交付包已到 1.2.x，前端「设置」页展示的
// 应用版本与真实包版本不一致（同一轮修复中，FVCS 也出现过 hello.go 硬编码 1.2.0 的问题）。
func TestAppVerEmbeddedFromFile(t *testing.T) {
	if strings.TrimSpace(AppVer) == "" {
		t.Fatal("appVer 为空：server/VERSION 缺失或 go:embed 未生效")
	}
	if AppVer != strings.TrimSpace(AppVer) {
		t.Fatalf("AppVer 含首尾空白未清理: %q", AppVer)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(AppVer) {
		t.Fatalf("AppVer 非三段式版本号（应为 x.y.z）: %q", AppVer)
	}
}

// TestAppVerMatchesPackagingFiles 校验二进制版本与打包相关版本号未漂移。
func TestAppVerMatchesPackagingFiles(t *testing.T) {
	checks := []struct {
		path    string
		pattern string
	}{
		{"../../manifest", `(?m)^version\s*=\s*(\S+)`},
		{"../../ui-src/package.json", `"version"\s*:\s*"([^"]+)"`},
	}
	for _, c := range checks {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			t.Skipf("跳过 %s（读取失败：%v）", c.path, err)
		}
		m := regexp.MustCompile(c.pattern).FindStringSubmatch(string(raw))
		if m == nil {
			t.Fatalf("%s 中未找到版本号字段", c.path)
		}
		if m[1] != AppVer {
			t.Fatalf("%s 中版本=%q 与二进制版本 AppVer=%q 不一致：出新包时请同步修改", c.path, m[1], AppVer)
		}
	}
}
