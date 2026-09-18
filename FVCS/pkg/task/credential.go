package task

// 凭据档案接入（07 §5.2 / §5.6，M1 阶段）
//
// 挂载策略：
//   - task.CredentialID != "" → 走 DPAPI 档案（密码只在渲染节点本机，解密后进内存、用完清零）；
//   - 否则回退既有明文路径（兼容旧 FVCC），并记 WARN 审计，便于 M3/M4 收敛统计。

import (
	"encoding/json"
	"fmt"
	"strings"

	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/smb"
)

// mountTaskShare 按任务凭据来源挂载 SMB 共享，返回挂载点。
// 调用方负责在任务收尾时 smb.UnmountSMBShare。
func mountTaskShare(task *Task) (string, error) {
	if task == nil {
		return "", fmt.Errorf("%s: 任务不存在", protocol.ErrCodeCredentialInvalid)
	}

	if id := strings.TrimSpace(task.CredentialID); id != "" {
		logger.Info("task", "mount share via credential: taskID=%s, credentialId=%s", task.TaskID, id)
		return smb.MountSMBShareWithCredential(task.SMBPath, id)
	}

	// 修复（代理 E_RENDER_FAILED 闭环）：挂载前缺凭据前置判定。
	// 档案与明文全空时若仍调用 net use，会以空账号发起认证，SMB 客户端进入交互式
	// 认证态并最终以 System error 1223 失败，失败原因无法定位。此处直接返回明确的
	// 缺凭据错误码，细化失败节点（FVCC 侧据此提示补齐凭据）。
	if strings.TrimSpace(task.SMBUser) == "" && strings.TrimSpace(task.SMBPassword) == "" {
		logger.Error("task", "mount aborted: missing credential, taskID=%s, share=%s", task.TaskID, task.SMBPath)
		if store := smb.DefaultCredStore(); store != nil {
			store.Audit("credential.missing", task.TaskID, "warn", "")
		}
		return "", fmt.Errorf("%s: 缺少挂载凭据（credentialId 与账号口令均为空），拒绝以空凭据发起 SMB 挂载", protocol.ErrCodeCredentialMissing)
	}

	// M1 兼容支路：旧 FVCC 仍下发明文账号口令
	if task.SMBUser != "" || task.SMBPassword != "" {
		logger.Warn("task", "legacy plaintext credential path in use: taskID=%s", task.TaskID)
		if store := smb.DefaultCredStore(); store != nil {
			store.Audit("credential.plaintext_fallback", task.TaskID, "warn", "")
		}
	}
	return smb.MountSMBShare(task.SMBPath, task.SMBUser, task.SMBPassword)
}

// applyCredentialPrecedence 凭据档案优先（07 §5.3 M1）：带 credentialId 时不下发、不留存明文口令。
func applyCredentialPrecedence(task *Task) {
	if task == nil || strings.TrimSpace(task.CredentialID) == "" {
		return
	}
	if task.SMBUser != "" || task.SMBPassword != "" {
		logger.Warn("task", "credentialId 优先，已丢弃下发的明文账号口令: taskID=%s", task.TaskID)
	}
	task.SMBUser = ""
	task.SMBPassword = ""
}

// RunningTaskIDsByCredential 返回引用该凭据且尚未进入终态的任务（供档案删除前校验，07 §5.4）。
func RunningTaskIDsByCredential(credentialID string) []string {
	if strings.TrimSpace(credentialID) == "" || manager == nil {
		return nil
	}

	manager.mutex.RLock()
	defer manager.mutex.RUnlock()

	out := make([]string, 0, 2)
	for _, t := range manager.tasks {
		if t.CredentialID != credentialID || t.Status.IsTerminal() {
			continue
		}
		out = append(out, t.TaskID)
	}
	return out
}

// RegisterCredentialInUseChecker 把任务占用判定注入凭据库（避免 smb ↔ task 包循环依赖）。
func RegisterCredentialInUseChecker(store *smb.CredStore) {
	if store == nil {
		return
	}
	store.SetInUseChecker(RunningTaskIDsByCredential)
}

// ---- 载荷凭据字段剔除（07 §5.6.5）----

var credentialFieldNames = map[string]struct{}{
	"password":         {},
	"passwd":           {},
	"pwd":              {},
	"secret":           {},
	"smbpassword":      {},
	"smbpass":          {},
	"credentialsecret": {},
	"secretcipher":     {},
	"passwordcipher":   {},
	"passwordenc":      {},
	"authkey":          {},
}

// sanitizePayloadJSON 落库前剔除载荷中的凭据字段。
// 仅在检测到敏感字段时才重写（返回 true），否则原样返回，避免影响既有重放语义。
func sanitizePayloadJSON(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return raw, false
	}

	var node interface{}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&node); err != nil {
		return raw, false
	}

	if !stripCredentialFields(node) {
		return raw, false
	}

	out, err := json.Marshal(node)
	if err != nil {
		return raw, false
	}

	// 只记录"已剔除"，禁止记录字段值
	logger.Warn("task", "payload 中的凭据字段已在落库前剔除（07 §5.6.5）")
	return string(out), true
}

func stripCredentialFields(node interface{}) bool {
	changed := false

	switch v := node.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if isCredentialFieldName(key) {
				delete(v, key)
				changed = true
				continue
			}
			if stripCredentialFields(val) {
				changed = true
			}
		}
	case []interface{}:
		for _, item := range v {
			if stripCredentialFields(item) {
				changed = true
			}
		}
	}

	return changed
}

func isCredentialFieldName(name string) bool {
	n := strings.ToLower(strings.ReplaceAll(name, "_", ""))
	n = strings.ReplaceAll(n, "-", "")
	_, ok := credentialFieldNames[n]
	return ok
}
