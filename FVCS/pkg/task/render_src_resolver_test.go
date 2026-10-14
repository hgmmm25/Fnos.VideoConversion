package task

// #8 渲染落地取根回归：makeSrcResolver 必须与 GEN_PROXY 同机制处理挂载点根写法。
// 真机症状——FVCS 报 `E_PAYLOAD_INVALID: sourceRoot 为空`，实为旧实现把"带尾分隔符的合法根"
// （MountSMBShare 返回 `\\host\share\`；或盘符根 `X:\`）误判为空。修复后仅"空根"才非法。
//
// 覆盖：
//  1) UNC 带/不带尾分隔符 + 本地目录根 → 构造成功（resolver 非空、无错误）；空/纯空白 → E_PAYLOAD_INVALID；
//  2) 行为：真实临时目录下命中素材返回落地路径；缺失素材 E_ASSET_MISSING；穿越/绝对 rel E_ASSET_NOT_IN_ROOT。
//
// OS 可移植：1) 不触达文件系统；2) 用 t.TempDir 实际文件，Linux/Windows 皆通过。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Fnos.VC_Service/pkg/protocol"
)

// 1. 根写法构造判定
func TestMakeSrcResolverAcceptsUNCAndRootForms(t *testing.T) {
	ok := []string{
		`\\192.168.1.106\Test\`,  // 真机实际 mountPath：UNC 带尾分隔符（旧实现误报为空）
		`\\192.168.1.106\Test`,   // UNC 不带尾分隔符
		`/vol2/1000/Test`,        // POSIX 授权根
		t.TempDir(),              // 本地目录根
	}
	for _, base := range ok {
		resolver, err := makeSrcResolver(base)
		if err != nil {
			t.Errorf("makeSrcResolver(%q) 应构造成功，实际 err=%v", base, err)
			continue
		}
		if resolver == nil {
			t.Errorf("makeSrcResolver(%q) 返回 nil 解析器", base)
		}
	}

	for _, blank := range []string{"", "   ", "\t"} {
		resolver, err := makeSrcResolver(blank)
		if err == nil || resolver != nil {
			t.Errorf("空根 %q 应拒绝：err=%v", blank, err)
			continue
		}
		if !strings.Contains(err.Error(), protocol.ErrCodePayloadInvalid) {
			t.Errorf("空根应报 E_PAYLOAD_INVALID，实际 %v", err)
		}
	}
}

// 2. 解析行为：命中 / 缺失 / 越权
func TestMakeSrcResolverResolveBehavior(t *testing.T) {
	root := t.TempDir()
	rel := filepath.ToSlash(filepath.Join("sub", "clip_a.mp4"))
	full := filepath.Join(root, "sub", "clip_a.mp4")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
		t.Fatalf("建文件失败: %v", err)
	}

	resolver, err := makeSrcResolver(root)
	if err != nil {
		t.Fatalf("本地根应构造成功: %v", err)
	}

	got, err := resolver(rel)
	if err != nil {
		t.Fatalf("命中素材应解析成功: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(full) {
		t.Errorf("解析落地路径不符：got=%q want=%q", got, full)
	}

	if _, err := resolver("sub/missing.mp4"); err == nil ||
		!strings.Contains(err.Error(), protocol.ErrCodeAssetMissing) {
		t.Errorf("缺失素材应 E_ASSET_MISSING，实际 %v", err)
	}

	for _, bad := range []string{"../evil.mp4", "/abs/evil.mp4", "sub/../out.mp4"} {
		if _, err := resolver(bad); err == nil ||
			!strings.Contains(err.Error(), protocol.ErrCodeAssetNotInRoot) {
			t.Errorf("非法相对路径 %q 应 E_ASSET_NOT_IN_ROOT，实际 %v", bad, err)
		}
	}
}
