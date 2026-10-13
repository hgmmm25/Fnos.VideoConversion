package version

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

// repoRootMarkers 是"仓库根"的判定条件：该目录下同时存在这些文件。
// 用文件存在性而不是 .git 目录判定，是因为 submodule / tarball / CI 检出未必带 .git。
var repoRootMarkers = []string{
	filepath.Join("FVCC", "manifest"),
	filepath.Join("FVCC", "ui-src", "package.json"),
}

// findRepoRoot 从候选起始目录逐级上溯，定位仓库根。
//
// 为什么要定位仓库根，而不是写 `../../manifest` 这类相对工作目录的路径：
// `go test` 的工作目录是被测包目录（FVCC/server/internal/version），
// 而打包版本文件的真实位置在 FVCC/ 下 —— 从包目录上溯是 4 层，不是 2 层。
// 旧实现把路径写成 `../../manifest`（实际解析到不存在的 FVCC/server/manifest），
// 又对读取失败走 t.Skipf，于是本机 `go test ./...` 长期"绿但什么都没校验"，
// v1.6.4 的 VERSION 漂移（VERSION=1.6.0 而 manifest/package.json=1.6.4）就是这么溜过收口的。
// 现在：定位不到仓库根、或读不到版本字段，一律 t.Fatal —— 校验缺失必须是红的。
func findRepoRoot(t *testing.T) string {
	t.Helper()
	starts := uniqueDirs(candidateStartDirs(t))
	for _, start := range starts {
		dir := filepath.Clean(start)
		for depth := 0; depth < 12; depth++ {
			if matchesAllMarkers(dir) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break // 已到文件系统根
			}
			dir = parent
		}
	}
	t.Fatalf("未能定位仓库根（需同时存在 %s）；候选起始目录：%v。"+
		"若本测试跑在被裁剪过的目录树里，请改在完整仓库检出中执行", strings.Join(repoRootMarkers, " 与 "), starts)
	return ""
}

func uniqueDirs(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, d := range in {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// candidateStartDirs 给出仓库根搜索的起点：测试源文件所在目录（编译期路径），以及当前工作目录。
// 前者抗 -trimpath/非常规工作目录，后者抗源码被复制到别处构建。
func candidateStartDirs(t *testing.T) []string {
	t.Helper()
	var dirs []string
	if _, self, _, ok := runtime.Caller(0); ok && filepath.IsAbs(self) {
		dirs = append(dirs, filepath.Dir(self))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	if len(dirs) == 0 {
		t.Fatal("既无法取得测试源文件路径，也无法取得工作目录")
	}
	return dirs
}

func matchesAllMarkers(root string) bool {
	for _, m := range repoRootMarkers {
		if _, err := os.Stat(filepath.Join(root, m)); err != nil {
			return false
		}
	}
	return true
}

// TestAppVerMatchesPackagingFiles 校验二进制版本与打包版本号（manifest / package.json）未漂移。
// 三方对齐：internal/version/VERSION（go:embed → AppVer）== FVCC/manifest == FVCC/ui-src/package.json。
func TestAppVerMatchesPackagingFiles(t *testing.T) {
	root := findRepoRoot(t)
	checks := []struct {
		label   string
		path    string
		pattern string
	}{
		{"FVCC/manifest", filepath.Join(root, "FVCC", "manifest"), `(?m)^\s*version\s*=\s*(\S+)`},
		{"FVCC/ui-src/package.json", filepath.Join(root, "FVCC", "ui-src", "package.json"), `"version"\s*:\s*"([^"]+)"`},
	}
	ran := 0
	for _, c := range checks {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", c.path, err)
		}
		m := regexp.MustCompile(c.pattern).FindStringSubmatch(string(raw))
		if m == nil {
			t.Fatalf("%s 中未找到版本号字段（模式 %q）", c.label, c.pattern)
		}
		if m[1] != AppVer {
			t.Fatalf("%s 中版本=%q 与二进制版本 AppVer=%q 不一致：出新包时请同步修改（VERSION / manifest / package.json 三处一起 bump）",
				c.label, m[1], AppVer)
		}
		ran++
	}
	if ran != len(checks) {
		t.Fatalf("预期校验 %d 个打包版本源，实际校验 %d 个", len(checks), ran)
	}
	t.Logf("版本三方对齐通过：AppVer=%s（manifest / package.json 一致）", AppVer)
}

// TestAppVerIsNotPinnedLiteral 守住"禁止在代码里另写版本字面量"的约定：
// 本包的生产代码不应出现 x.y.z 形式的硬编码版本号，否则 VERSION 一 bump 就留下第二真源
// （历史缺陷：FVCS 曾在 hello.go 硬编码 agentVersion="1.2.0"）。
// 测试文件不在扫描范围：用例里的样例版本串（如 9.9.9）属于夹具，不是版本源。
func TestAppVerIsNotPinnedLiteral(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败：%v", err)
	}
	pin := regexp.MustCompile(`["` + "`" + `]\d+\.\d+\.\d+["` + "`" + `]`)
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", name, err)
		}
		if loc := pin.FindIndex(raw); loc != nil {
			t.Fatalf("%s 中存在硬编码版本字面量（偏移 %d）：版本唯一来源应为 VERSION 文件", name, loc[0])
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何生产代码文件：包目录结构可能已变化")
	}
}
