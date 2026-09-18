package server

// D-03 单测：任务创建阶段的凭据前置拒绝（07 §5.3）
//
// 覆盖：无 credentialId 直通、库未初始化拒绝、凭据不存在拒绝、共享根一致性、绝对 UNC 越界拒绝、
// 相对路径忽略、合法目标放行。

import (
	"errors"
	"net/http"
	"testing"

	"Fnos.VC_Service/pkg/smb"
)

func TestPrecheckCredentialTarget(t *testing.T) {
	store := withAdminStore(t)

	// 未携带 credentialId → 直通（兼容旧客户端明文支路）
	if err := precheckCredentialTarget("", adminTestShare); err != nil {
		t.Errorf("空 credentialId 应放行，实际 %v", err)
	}

	// 凭据不存在 → E_CREDENTIAL_NOT_FOUND
	if err := precheckCredentialTarget("cred_missing", adminTestShare); !errors.Is(err, smb.ErrCredentialNotFound) {
		t.Errorf("未知凭据应返回 ErrCredentialNotFound，实际 %v", err)
	}

	// 建立档案（shareBase=\\nas-01\media，scope=[\\nas-01\media\footage]）
	if code, _, raw := doCredentialRequest(t, http.MethodPut, "/local/credentials", putBody("cred_nas01"), "127.0.0.1:54000"); code != http.StatusOK {
		t.Fatalf("创建凭据失败: %d %s", code, raw)
	}

	// 共享根与档案一致 + 绝对 UNC 落在 scope 内 → 放行
	if err := precheckCredentialTarget("cred_nas01", adminTestShare, adminTestScope, `\\nas-01\media\footage\sub\a.mov`); err != nil {
		t.Errorf("合法目标应放行，实际 %v", err)
	}
	// 相对路径由执行期校验，创建阶段忽略
	if err := precheckCredentialTarget("cred_nas01", adminTestShare, "footage/a.mov", ""); err != nil {
		t.Errorf("相对路径应放行，实际 %v", err)
	}
	// 空字段不应视为越界
	if err := precheckCredentialTarget("cred_nas01", "", ""); err != nil {
		t.Errorf("空目标应放行，实际 %v", err)
	}

	// 共享根被挪用到其它主机 → 拒绝
	if err := precheckCredentialTarget("cred_nas01", `\\nas-02\media`); !errors.Is(err, smb.ErrCredentialScopeDenied) {
		t.Errorf("异地共享应被拒，实际 %v", err)
	}
	// 绝对 UNC 越出 scope → 拒绝（含前缀边界 mediax）
	for _, bad := range []string{`\\nas-01\other`, `\\nas-02\media`, `\\nas-01\mediax`, adminTestShare} {
		if err := precheckCredentialTarget("cred_nas01", adminTestShare, bad); !errors.Is(err, smb.ErrCredentialScopeDenied) {
			t.Errorf("目标 %s 应被拒（Scope），实际 %v", bad, err)
		}
	}

	// 库不可用 → E_CREDENTIAL_STORE_UNAVAILABLE
	smb.SetDefaultCredStore(nil)
	t.Cleanup(func() { smb.SetDefaultCredStore(store) })
	if err := precheckCredentialTarget("cred_nas01", adminTestShare); !errors.Is(err, smb.ErrCredentialStoreUnavailable) {
		t.Errorf("库未初始化应返回 ErrCredentialStoreUnavailable，实际 %v", err)
	}
}
