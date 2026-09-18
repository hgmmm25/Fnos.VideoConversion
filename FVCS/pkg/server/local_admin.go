package server

// 本机凭据管理接口（07 §5.4）
//
// 定位：FVCS 侧只暴露给 NAS 网关/运维本机调用的凭据档案管理面。
// 约束：
//   - 只服务环回来源（非 127.0.0.0/8 / ::1 一律 403），与 config.ListenLocalOnly 叠加生效；
//   - 不回传 secret：列表/详情一律只返回 CredentialMeta；
//   - 日志只出现 credentialId / 共享主机名，禁止出现用户名与口令。

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/smb"
	"Fnos.VC_Service/pkg/task"
)

const (
	credentialDBFileName = "credentials.db"
	// 单次请求体上限：档案元数据为小对象，64KB 足够
	maxCredentialBodyBytes = 64 << 10
)

// codeCredentialStoreUnavailable 凭据库不可用（与 smb.ErrCredentialStoreUnavailable 前缀一致）
const codeCredentialStoreUnavailable = "E_CREDENTIAL_STORE_UNAVAILABLE"

// credentialSecretStore 存储方式标识（07 §5.3 响应示例：secretStored=dpapi）
const credentialSecretStore = "dpapi"

// InitCredentialAdmin 初始化进程级凭据档案库，并把"任务占用判定"注入档案库（07 §5.4）。
// 幂等：重复调用只重新注册占用判定，不会重复打开库文件。
func InitCredentialAdmin() error {
	if store := smb.DefaultCredStore(); store != nil {
		task.RegisterCredentialInUseChecker(store)
		return nil
	}

	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	dbPath := filepath.Join(dir, credentialDBFileName)

	store, err := smb.InitDefaultCredStore(dbPath)
	if err != nil {
		logger.Error("server", "Failed to init credential store: %v", err)
		return err
	}
	task.RegisterCredentialInUseChecker(store)
	logger.Info("server", "Credential store initialized: %s", dbPath)
	return nil
}

type credentialAPIResponse struct {
	OK      bool        `json:"ok"`
	Code    string      `json:"code,omitempty"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// handleLocalCredentials 本机凭据档案管理：
//   - POST/PUT  body: smb.PutCredentialRequest → 创建或轮换
//   - GET      无 id → 列表；带 id → 单条
//   - DELETE   id → 删除（被运行中任务引用时拒绝）
func handleLocalCredentials(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if !isLoopbackRequest(r) {
		// 只记来源 IP，不记 body
		logger.Warn("server", "Credential admin API rejected non-loopback caller: %s", remoteHost(r))
		writeCredentialError(w, http.StatusForbidden, protocol.ErrCodeCredentialScopeDenied, "该接口仅允许本机访问")
		return
	}

	store := smb.DefaultCredStore()
	if store == nil {
		writeCredentialError(w, http.StatusServiceUnavailable, codeCredentialStoreUnavailable, "凭据库未初始化")
		return
	}

	switch r.Method {
	case http.MethodPost, http.MethodPut:
		handlePutCredential(w, r, store)
	case http.MethodGet:
		handleGetCredential(w, r, store)
	case http.MethodDelete:
		handleDeleteCredential(w, r, store)
	default:
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		writeCredentialError(w, http.StatusMethodNotAllowed, protocol.ErrCodeCredentialInvalid, "不支持的请求方法")
	}
}

func handlePutCredential(w http.ResponseWriter, r *http.Request, store *smb.CredStore) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCredentialBodyBytes+1))
	if err != nil {
		writeCredentialError(w, http.StatusBadRequest, protocol.ErrCodeCredentialInvalid, "读取请求体失败")
		return
	}
	if len(body) > maxCredentialBodyBytes {
		writeCredentialError(w, http.StatusRequestEntityTooLarge, protocol.ErrCodeCredentialInvalid, "请求体过大")
		return
	}

	var req smb.PutCredentialRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeCredentialError(w, http.StatusBadRequest, protocol.ErrCodeCredentialInvalid, "请求体不是合法 JSON")
		return
	}
	// body 不再需要，显式清引用（口令仅经结构体字段传递一次）
	body = nil

	if err := store.Put(req); err != nil {
		status, code := credentialErrStatus(err)
		writeCredentialError(w, status, code, credentialMessage(err))
		return
	}

	meta, err := store.Get(req.ID)
	if err != nil {
		// 落库成功但回读失败：仍只回 credentialId 与存储方式
		writeCredentialOK(w, map[string]interface{}{"credentialId": req.ID, "secretStored": credentialSecretStore})
		return
	}
	writeCredentialOK(w, map[string]interface{}{
		"credentialId": meta.ID,
		"shareBase":    meta.ShareBase,
		"scope":        meta.Scope,
		"createdAt":    meta.CreatedAt,
		"updatedAt":    meta.UpdatedAt,
		"lastUsedAt":   meta.LastUsedAt,
		"secretStored": credentialSecretStore,
	})
}

func handleGetCredential(w http.ResponseWriter, r *http.Request, store *smb.CredStore) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		list, err := store.List()
		if err != nil {
			status, code := credentialErrStatus(err)
			writeCredentialError(w, status, code, credentialMessage(err))
			return
		}
		writeCredentialOK(w, list)
		return
	}

	meta, err := store.Get(id)
	if err != nil {
		status, code := credentialErrStatus(err)
		writeCredentialError(w, status, code, credentialMessage(err))
		return
	}
	writeCredentialOK(w, meta)
}

func handleDeleteCredential(w http.ResponseWriter, r *http.Request, store *smb.CredStore) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeCredentialError(w, http.StatusBadRequest, protocol.ErrCodeCredentialInvalid, "缺少 id 参数")
		return
	}

	if err := store.Delete(id); err != nil {
		status, code := credentialErrStatus(err)
		writeCredentialError(w, status, code, credentialMessage(err))
		return
	}
	writeCredentialOK(w, map[string]interface{}{"credentialId": id, "deleted": true})
}

// ---- 辅助 ----

func isLoopbackRequest(r *http.Request) bool {
	host := remoteHost(r)
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func writeCredentialOK(w http.ResponseWriter, data interface{}) {
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(credentialAPIResponse{OK: true, Data: data})
}

func writeCredentialError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(credentialAPIResponse{OK: false, Code: code, Message: message})
}

// credentialErrStatus 07 §5.4 错误码 → HTTP 状态
func credentialErrStatus(err error) (int, string) {
	switch {
	case errors.Is(err, smb.ErrCredentialNotFound):
		return http.StatusNotFound, protocol.ErrCodeCredentialNotFound
	case errors.Is(err, smb.ErrCredentialScopeDenied):
		return http.StatusForbidden, protocol.ErrCodeCredentialScopeDenied
	case errors.Is(err, smb.ErrCredentialInUse):
		return http.StatusConflict, protocol.ErrCodeCredentialInUse
	default:
		return http.StatusBadRequest, protocol.ErrCodeCredentialInvalid
	}
}

// credentialMessage 去掉文本前的错误码前缀，只回传可读文案（不含口令）
func credentialMessage(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ":"); i >= 0 {
		msg = msg[i+1:]
	}
	return strings.TrimSpace(msg)
}
