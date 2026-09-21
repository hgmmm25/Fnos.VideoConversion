package remote

// v1.2.6 回归测试（代理生成 E_RENDER_FAILED 闭环）：
//  1) GEN_PROXY 下发的素材共享根与转码同源——由 settings.videoRoot 推导，
//     不再取已弃用的 settings.smbSharePath，消除 \\192.168.1.106\T 与 \Test 的共享根分裂
//     （转码可挂载、代理必失败 System error 1223）；
//  2) credentialId 为空时按既有转码下发同口径补齐 settings 明文账号口令，闭合凭据下发链路；
//     仅账号或仅口令时不下发半截凭据，避免把"缺凭据"伪装成"节点状态 Failed"。

import (
	"strings"
	"testing"

	"fvcc/internal/store/model"
)

func newGenProxyRemoteClient(t *testing.T, cfg model.Settings) *RemoteClient {
	t.Helper()
	rc := NewRemoteClient()
	t.Cleanup(rc.CloseAll)
	rc.SetSettingsProvider(func() model.Settings { return cfg })
	return rc
}

// GEN_PROXY 素材共享根必须跟随 videoRoot（与转码同源），而非已弃用的 smbSharePath。
func TestGenProxyShareRootFollowsVideoRoot(t *testing.T) {
	rc := newGenProxyRemoteClient(t, model.Settings{
		SMBSharePath: `\\192.168.1.106\T`,
		VideoRoot:    `\\192.168.1.106\Test`,
	})

	src, dst, err := rc.resolveRenderSharePaths(model.TaskTypeGenProxy, []byte(`{"type":"GenProxy"}`))
	if err != nil {
		t.Fatalf("GEN_PROXY 共享根解析失败: %v", err)
	}
	if src != `\\192.168.1.106\Test` {
		t.Fatalf("GEN_PROXY 素材共享根 = %q，期望与转码一致的 %q", src, `\\192.168.1.106\Test`)
	}
	if dst != `\\192.168.1.106\Test\_proxy` {
		t.Fatalf("GEN_PROXY 输出共享根 = %q，期望 %q", dst, `\\192.168.1.106\Test\_proxy`)
	}
}

// 未配置 videoRoot 时保持既有语义（回退 smbSharePath），不破坏旧部署。
func TestGenProxyShareRootFallsBackToSMBSharePath(t *testing.T) {
	rc := newGenProxyRemoteClient(t, model.Settings{SMBSharePath: `\\192.168.1.106\media`})

	src, dst, err := rc.resolveRenderSharePaths(model.TaskTypeGenProxy, []byte(`{"type":"GenProxy"}`))
	if err != nil {
		t.Fatalf("回退解析失败: %v", err)
	}
	if src != `\\192.168.1.106\media` {
		t.Fatalf("回退素材共享根 = %q，期望 %q", src, `\\192.168.1.106\media`)
	}
	if dst != `\\192.168.1.106\media\_proxy` {
		t.Fatalf("回退输出共享根 = %q，期望 %q", dst, `\\192.168.1.106\media\_proxy`)
	}
}

// 共享根缺失时必须下发前快速失败（可重试码），不静默下发空路径。
func TestGenProxyMissingShareRootFailsFast(t *testing.T) {
	rc := newGenProxyRemoteClient(t, model.Settings{})

	_, _, err := rc.resolveRenderSharePaths(model.TaskTypeGenProxy, []byte(`{"type":"GenProxy"}`))
	if err == nil || !strings.Contains(err.Error(), ErrCodeSMBMountFailed) {
		t.Fatalf("缺少共享根应返回 %s，实际: %v", ErrCodeSMBMountFailed, err)
	}
}

// 挂载凭据补齐策略：档案优先；档案缺失时回落设置明文；半截/全空凭据不下发。
func TestApplyRenderMountCredential(t *testing.T) {
	cases := []struct {
		name       string
		cfg        model.Settings
		credID     string
		wantSource string
		wantUser   string
		wantPass   string
	}{
		{
			name: "凭据档案优先，不下发明文", cfg: model.Settings{SMBUser: "nas", SMBPassword: "pw"},
			credID: "cred_node_1", wantSource: "archive",
		},
		{
			name: "档案缺失回落明文（与转码同口径）", cfg: model.Settings{SMBUser: "nas", SMBPassword: "pw"},
			wantSource: "plaintext", wantUser: "nas", wantPass: "pw",
		},
		{
			name: "仅账号无口令不下发半截凭据", cfg: model.Settings{SMBUser: "nas"},
		},
		{
			name: "全空保持无凭据（由 FVCS 前置报 E_CREDENTIAL_MISSING）", cfg: model.Settings{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc := newGenProxyRemoteClient(t, c.cfg)
			wire := &wsCmd{CredentialID: c.credID}

			if got := rc.applyRenderMountCredential(wire); got != c.wantSource {
				t.Fatalf("凭据来源 = %q，期望 %q", got, c.wantSource)
			}
			if wire.SMBUser != c.wantUser || wire.SMBPassword != c.wantPass {
				t.Fatalf("下发凭据 user/pass = %q/%q，期望 %q/%q",
					wire.SMBUser, wire.SMBPassword, c.wantUser, c.wantPass)
			}
		})
	}
}
