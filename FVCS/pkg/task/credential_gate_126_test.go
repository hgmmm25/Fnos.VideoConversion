package task

// v1.2.6 回归测试（代理生成 E_RENDER_FAILED 闭环）：
//  1) 凭据档案与明文全空时，mountTaskShare 必须在 net use 之前直接返回
//     E_CREDENTIAL_MISSING——旧行为会以空账号发起 SMB 挂载，进入交互式认证态并以
//     System error 1223 失败，失败原因无法定位；
//  2) 失败码映射：缺凭据类挂载失败归类 E_CREDENTIAL_MISSING，其余归 E_SMB_MOUNT_FAILED，
//     供 FVCC 侧细化失败节点上报。

import (
	"errors"
	"strings"
	"testing"

	"Fnos.VC_Service/pkg/protocol"
)

func TestMountTaskShareRejectsEmptyCredential(t *testing.T) {
	tk := &Task{TaskID: "t_empty_cred", SMBPath: `\\192.168.1.106\Test`}

	_, err := mountTaskShare(tk)
	if err == nil {
		t.Fatal("凭据全空时必须拒绝挂载，禁止以空凭据进入 net use 交互态")
	}
	if !strings.Contains(err.Error(), protocol.ErrCodeCredentialMissing) {
		t.Fatalf("错误码应为 %s，实际: %v", protocol.ErrCodeCredentialMissing, err)
	}
}

func TestMountTaskShareNilTask(t *testing.T) {
	if _, err := mountTaskShare(nil); err == nil {
		t.Fatal("空任务必须直接失败")
	}
}

func TestMountFailureCodeClassifiesMissingCredential(t *testing.T) {
	missing := errors.New("E_CREDENTIAL_MISSING: 缺少挂载凭据（credentialId 与账号口令均为空）")
	if got := mountFailureCode(missing); got != protocol.ErrCodeCredentialMissing {
		t.Fatalf("缺凭据失败码 = %q，期望 %q", got, protocol.ErrCodeCredentialMissing)
	}

	other := errors.New("E_SMB_MOUNT_FAILED: net use 失败: System error 1223")
	if got := mountFailureCode(other); got != protocol.ErrCodeSMBMountFailed {
		t.Fatalf("其它挂载失败码 = %q，期望 %q", got, protocol.ErrCodeSMBMountFailed)
	}
}
