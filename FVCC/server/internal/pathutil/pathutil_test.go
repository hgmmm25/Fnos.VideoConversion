package pathutil

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestWithinRoot 用斜杠字面量表达路径，经 filepath.FromSlash 转成本机分隔符后传入，
// 使同一组用例在 Linux（CI，分隔符 `/`）与 Windows（本机，分隔符 `\`）上均成立。
// filepath.Clean 在本机把 `/` 归一为原生分隔符，故必须用 FromSlash 对齐输入，
// 否则斜杠字面量与 Clean 结果不再相等（例如盘符根之外的普通根也会误判）。
func TestWithinRoot(t *testing.T) {
	tests := []struct {
		name string
		root string
		abs  string
		want bool
	}{
		{"根自身", "/media/videos", "/media/videos", true},
		{"子路径命中", "/media/videos", "/media/videos/a.mp4", true},
		{"深层子路径", "/media/videos", "/media/videos/sub/a.mp4", true},
		{"兄弟目录误判防护", "/media/videos", "/media/videos2/a.mp4", false},
		{"完全无关", "/media/videos", "/other/a.mp4", false},
		// 根以分隔符结尾（POSIX 根 `/`；Windows 盘符根同类逻辑见 TestWithinRootDriveRoot）：
		// Clean 保留尾分隔符，若不特判会拼成双分隔符使任何子路径都命中不了。
		{"根为分隔符结尾_根自身", "/", "/", true},
		{"根为分隔符结尾_子路径", "/", "/etc/passwd", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.FromSlash(tt.root)
			abs := filepath.FromSlash(tt.abs)
			if got := WithinRoot(root, abs); got != tt.want {
				t.Errorf("WithinRoot(%q, %q) = %v, want %v", root, abs, got, tt.want)
			}
		})
	}
}

// TestWithinRootDriveRoot 仅在 Windows 上验证盘符根（D:\）边界：Clean 保留尾分隔符，
// 子路径须以该根为前缀命中（Linux 上反斜杠非分隔符，无法复现该场景）。
func TestWithinRootDriveRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("盘符根边界为 Windows 专有，Linux CI 由 TestWithinRoot 的分隔符结尾分支等价覆盖")
	}
	cases := []struct {
		root, abs string
		want      bool
	}{
		{`D:\`, `D:\`, true},
		{`D:\`, `D:\a.mp4`, true},
		{`D:\`, `D:\sub\a.mp4`, true},
		{`D:\media`, `D:\media\a.mp4`, true},
		{`D:\media`, `D:\media2\a.mp4`, false},
	}
	for _, c := range cases {
		if got := WithinRoot(c.root, c.abs); got != c.want {
			t.Errorf("WithinRoot(%q, %q) = %v, want %v", c.root, c.abs, got, c.want)
		}
	}
}
