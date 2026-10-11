package media

// roots_test.go — ResolveMediaRoots 素材根回落回归测试。
//
// 背景（2026-09-22 修复）：素材根 videoRoot 为空 **或本机不可见**（dev 下默认
// POSIX 根 /media/videos 在 Windows 上不存在、或 SMB/UNC 暂未挂载）时，src 根
// 回落授权目录首个（pv.AccessPaths()[0]），与素材扫描/浏览口径一致，避免
// "列表可见、票据申请 E_ASSET_MISSING" 的错位。

import (
	"os"
	"path/filepath"
	"testing"

	"fvcc/internal/security"
	"fvcc/internal/store/model"
)

type mockSettings struct{ s model.Settings }

func (m mockSettings) GetSettings() model.Settings { return m.s }

// newTestPV 构造授权目录受控的 PathValidator（非 dev，envFile 指向 temp 文件）。
func newTestPV(t *testing.T, authorized string) *security.PathValidator {
	t.Helper()
	envFile := filepath.Join(t.TempDir(), "accessible_paths.env")
	if err := os.WriteFile(envFile, []byte(authorized), 0o600); err != nil {
		t.Fatal(err)
	}
	return security.NewPathValidator(false, envFile)
}

func TestResolveMediaRoots_SrcFallbackToAccessiblePath(t *testing.T) {
	dir := t.TempDir()
	pv := newTestPV(t, dir)

	cases := []struct {
		name      string
		videoRoot string
		wantSrc   string
	}{
		{"videoRoot empty falls back to first authorized path", "", dir},
		// 修复核心：videoRoot 非空但本机不可见（如 /media/videos 在 Windows 上不存在）→ 回落授权目录
		{"invisible videoRoot falls back to first authorized path", filepath.Join(t.TempDir(), "no_such_dir"), dir},
		{"POSIX invisible videoRoot falls back to first authorized path", "/media/videos", dir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roots := ResolveMediaRoots(mockSettings{model.Settings{VideoRoot: tc.videoRoot}}, pv)
			want := filepath.Clean(tc.wantSrc)
			if !samePath(roots.SourceLocal, want) {
				t.Fatalf("SourceLocal = %q, want %q", roots.SourceLocal, want)
			}
		})
	}
}

func TestResolveMediaRoots_VisibleVideoRootKept(t *testing.T) {
	dir := t.TempDir()
	pv := newTestPV(t, t.TempDir()) // 授权目录与 videoRoot 不同，确保不靠回落
	roots := ResolveMediaRoots(mockSettings{model.Settings{VideoRoot: dir}}, pv)

	if !samePath(roots.SourceLocal, dir) {
		t.Fatalf("SourceLocal = %q, want %q", roots.SourceLocal, dir)
	}
	wantProxy := filepath.Join(filepath.Clean(dir), "_proxy")
	if !samePath(roots.ProxyLocal, wantProxy) {
		t.Fatalf("ProxyLocal = %q, want %q", roots.ProxyLocal, wantProxy)
	}
	wantDest := filepath.Join(filepath.Clean(dir), "_exports")
	if !samePath(roots.DestLocal, wantDest) {
		t.Fatalf("DestLocal = %q, want %q", roots.DestLocal, wantDest)
	}
}

func TestResolveMediaRoots_NoAccessiblePathsKeepsConfig(t *testing.T) {
	dir := t.TempDir()
	roots := ResolveMediaRoots(mockSettings{model.Settings{VideoRoot: dir}}, nil)
	if !samePath(roots.SourceLocal, dir) {
		t.Fatalf("SourceLocal = %q, want %q", roots.SourceLocal, dir)
	}
}
