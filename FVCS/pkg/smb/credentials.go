package smb

// 本地凭据档案（设计文档 07 §5.2 / §5.3 / §5.4 / §5.6）
//
// 目标模型：NAS 侧只存 credentialId 与共享白名单，密码只在渲染节点本机存在，
// 以 DPAPI(CryptProtectData, LocalMachine) 保护后落 credentials 表；执行挂载时才
// 解密进内存，用完即覆写清零，不落盘、不落日志、不回传。
//
// 硬约束（07 §5.6）：
//  1. 禁止编造凭据（缺失时返回错误，由本机管理界面索取）；
//  2. 禁止把 secret 回传 FVCC / 浏览器；
//  3. 禁止凭据进日志、错误消息、进度消息 —— 本文件所有日志只出现 credentialId；
//  4. 禁止在 URL / 查询串中携带凭据；
//  5. 禁止将 secret 写入任务载荷（payload_json 落库前剔除，见 pkg/task 的 sanitize）。

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/winapi"

	_ "github.com/mattn/go-sqlite3"
)

// ---- 错误（错误码取自 protocol，供上层直接作为 code 回包）----

var (
	// ErrCredentialNotFound 档案不存在
	ErrCredentialNotFound = fmt.Errorf("%s: 凭据档案不存在", protocol.ErrCodeCredentialNotFound)
	// ErrCredentialScopeDenied 目标 UNC 越出档案 Scope
	ErrCredentialScopeDenied = fmt.Errorf("%s: 目标路径越出凭据允许范围", protocol.ErrCodeCredentialScopeDenied)
	// ErrCredentialInvalid 凭据数据无效（参数非法 / 解密失败 / 挂载被拒）
	ErrCredentialInvalid = fmt.Errorf("%s: 凭据数据无效", protocol.ErrCodeCredentialInvalid)
	// ErrCredentialInUse 凭据被运行中任务引用
	ErrCredentialInUse = fmt.Errorf("%s: 凭据被运行中任务引用", protocol.ErrCodeCredentialInUse)
)

func credError(base error, format string, args ...interface{}) error {
	if len(args) == 0 {
		return base
	}
	return fmt.Errorf("%w (%s)", base, fmt.Sprintf(format, args...))
}

// ---- 数据结构 ----

// CredentialMeta 档案元数据（不含 secret，可安全回传本机管理界面）
type CredentialMeta struct {
	ID         string     `json:"credentialId"`
	ShareBase  string     `json:"shareBase"`
	Username   string     `json:"username"`
	Domain     string     `json:"domain,omitempty"`
	Scope      []string   `json:"scope"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RotatedAt  *time.Time `json:"rotatedAt,omitempty"`
}

// Credential 解析结果：明文 secret 仅存在于内存
type Credential struct {
	ID        string
	ShareBase string
	Username  string
	Domain    string
	Scope     []string

	// secret 明文仅存在于内存（no-copy：禁止赋值给长期存活的结构体字段、
	// 禁止进入 Task 持久化字段、禁止参与序列化）。使用后必须调用 Wipe()。
	secret []byte
}

// Secret 返回明文口令，仅供 net use 参数使用。
func (c *Credential) Secret() string {
	if len(c.secret) == 0 {
		return ""
	}
	return string(c.secret)
}

// LoginName 组装 net use /user: 参数（有域时形如 DOMAIN\user）
func (c *Credential) LoginName() string {
	if c.Domain == "" {
		return c.Username
	}
	return c.Domain + `\` + c.Username
}

// Wipe 覆写并丢弃明文（defer 调用）
func (c *Credential) Wipe() {
	for i := range c.secret {
		c.secret[i] = 0
	}
	c.secret = nil
}

// PutCredentialRequest 创建/轮换档案入参（来自本机管理接口）
type PutCredentialRequest struct {
	ID        string   `json:"credentialId"`
	ShareBase string   `json:"shareBase"`
	Username  string   `json:"username"`
	Domain    string   `json:"domain"`
	Password  string   `json:"password"`
	Scope     []string `json:"scope"`
}

// ---- 存储 ----

const credentialSchema = `
CREATE TABLE IF NOT EXISTS credentials (
	credential_id TEXT PRIMARY KEY,
	share_base    TEXT NOT NULL,
	username      TEXT NOT NULL,
	domain        TEXT,
	secret_cipher BLOB NOT NULL,
	scope_json    TEXT NOT NULL,
	created_at    TEXT NOT NULL,
	updated_at    TEXT NOT NULL,
	last_used_at  TEXT,
	rotated_at    TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	ts     TEXT NOT NULL,
	actor  TEXT NOT NULL,
	action TEXT NOT NULL,
	target TEXT,
	result TEXT NOT NULL,
	detail TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts DESC);
`

const auditRetentionDays = 180

// CredStore 本地凭据档案存储
type CredStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	inUse func(credentialID string) []string
	now   func() time.Time
}

// OpenCredStore 打开（或创建）凭据库
func OpenCredStore(dbPath string) (*CredStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store, err := NewCredStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// NewCredStore 基于既有连接构造（测试用）
func NewCredStore(db *sql.DB) (*CredStore, error) {
	store := &CredStore{db: db, now: time.Now}
	if _, err := db.Exec(credentialSchema); err != nil {
		return nil, err
	}
	store.purgeExpiredAudit()
	return store, nil
}

func (s *CredStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// SetInUseChecker 注入"该凭据是否被运行中任务引用"的判定（由 task 包注册，避免包循环依赖）
func (s *CredStore) SetInUseChecker(fn func(credentialID string) []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inUse = fn
}

// ---- 全局默认实例 ----

var (
	defaultStore   *CredStore
	defaultStoreMu sync.RWMutex
)

// InitDefaultCredStore 初始化进程级默认档案库
func InitDefaultCredStore(dbPath string) (*CredStore, error) {
	store, err := OpenCredStore(dbPath)
	if err != nil {
		return nil, err
	}
	defaultStoreMu.Lock()
	defaultStore = store
	defaultStoreMu.Unlock()
	return store, nil
}

// DefaultCredStore 返回进程级默认档案库（可能为 nil，调用方需判空）
func DefaultCredStore() *CredStore {
	defaultStoreMu.RLock()
	defer defaultStoreMu.RUnlock()
	return defaultStore
}

// SetDefaultCredStore 注入默认档案库（测试用）
func SetDefaultCredStore(store *CredStore) {
	defaultStoreMu.Lock()
	defaultStore = store
	defaultStoreMu.Unlock()
}

// ---- 写入 ----

// Put 创建或轮换档案：先 DPAPI 加密，再落库；已存在则视为轮换（更新 rotated_at）。
func (s *CredStore) Put(req PutCredentialRequest) error {
	scope, err := normalizePutRequest(&req)
	if err != nil {
		s.Audit("credential.put", req.ID, protocol.ErrCodeCredentialInvalid, err.Error())
		return err
	}

	cipher, err := winapi.DPAPIProtect([]byte(req.Password), "FVCS credential "+req.ID)
	// 入参口令不再使用，立即断开引用（Go 字符串不可变，此处仅解除持有）
	req.Password = ""
	if err != nil {
		logger.Error("smb", "credential put failed: id=%s, dpapi error=%v", req.ID, err)
		s.Audit("credential.put", req.ID, protocol.ErrCodeCredentialInvalid, "DPAPI 加密失败")
		return credError(ErrCredentialInvalid, "DPAPI 加密失败")
	}

	scopeJSON, _ := json.Marshal(scope)
	now := s.timestamp()

	var exists bool
	if err := s.db.QueryRow(`SELECT 1 FROM credentials WHERE credential_id = ?`, req.ID).Scan(new(int)); err == nil {
		exists = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return credError(ErrCredentialInvalid, "查询档案失败")
	}

	if exists {
		_, err = s.db.Exec(`UPDATE credentials
			SET share_base = ?, username = ?, domain = ?, secret_cipher = ?, scope_json = ?,
			    updated_at = ?, rotated_at = ?
			WHERE credential_id = ?`,
			req.ShareBase, req.Username, req.Domain, cipher, string(scopeJSON), now, now, req.ID)
	} else {
		_, err = s.db.Exec(`INSERT INTO credentials
			(credential_id, share_base, username, domain, secret_cipher, scope_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			req.ID, req.ShareBase, req.Username, req.Domain, cipher, string(scopeJSON), now, now)
	}
	if err != nil {
		logger.Error("smb", "credential put failed: id=%s, db error=%v", req.ID, err)
		s.Audit("credential.put", req.ID, protocol.ErrCodeCredentialInvalid, "落库失败")
		return credError(ErrCredentialInvalid, "落库失败")
	}

	// 审计与日志只出现 credentialId（禁止 username / password）
	s.Audit("credential.put", req.ID, "ok", map[bool]string{true: "rotate", false: "create"}[exists])
	logger.Info("smb", "credential stored: id=%s, shareHost=%s, scopeCount=%d, rotated=%v",
		req.ID, maskUNC(req.ShareBase), len(scope), exists)
	return nil
}

// Delete 删除档案；被运行中任务引用时拒绝（07 §5.4）
func (s *CredStore) Delete(id string) error {
	if !validCredentialID(id) {
		return credError(ErrCredentialInvalid, "credentialId 非法")
	}

	if _, err := s.Get(id); err != nil {
		return err
	}

	if ids := s.inUseTasks(id); len(ids) > 0 {
		s.Audit("credential.delete", id, protocol.ErrCodeCredentialInUse, fmt.Sprintf("被 %d 个运行中任务引用", len(ids)))
		logger.Warn("smb", "credential delete rejected: id=%s, runningTasks=%d", id, len(ids))
		return credError(ErrCredentialInUse, "被运行中任务引用")
	}

	if _, err := s.db.Exec(`DELETE FROM credentials WHERE credential_id = ?`, id); err != nil {
		s.Audit("credential.delete", id, protocol.ErrCodeCredentialInvalid, "删除失败")
		return credError(ErrCredentialInvalid, "删除失败")
	}

	s.Audit("credential.delete", id, "ok", "")
	logger.Info("smb", "credential deleted: id=%s", id)
	return nil
}

// ---- 读取 ----

// Get 读取档案元数据（不含 secret）
func (s *CredStore) Get(id string) (*CredentialMeta, error) {
	if !validCredentialID(id) {
		return nil, credError(ErrCredentialInvalid, "credentialId 非法")
	}

	row := s.db.QueryRow(`SELECT credential_id, share_base, username, domain, scope_json,
		created_at, updated_at, last_used_at, rotated_at FROM credentials WHERE credential_id = ?`, id)

	meta, err := scanCredentialMeta(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, credError(ErrCredentialInvalid, "读取档案失败")
	}
	return meta, nil
}

// List 列出全部档案元数据（不含 secret）
func (s *CredStore) List() ([]CredentialMeta, error) {
	rows, err := s.db.Query(`SELECT credential_id, share_base, username, domain, scope_json,
		created_at, updated_at, last_used_at, rotated_at FROM credentials ORDER BY credential_id`)
	if err != nil {
		return nil, credError(ErrCredentialInvalid, "读取档案列表失败")
	}
	defer rows.Close()

	out := make([]CredentialMeta, 0, 8)
	for rows.Next() {
		meta, err := scanCredentialMeta(rows)
		if err != nil {
			continue
		}
		out = append(out, *meta)
	}
	return out, nil
}

// Resolve 解析档案并校验目标路径（07 §5.3）。
//   - targetUNC 非空：必须落在 Scope 内，否则 E_CREDENTIAL_SCOPE_DENIED；
//   - targetUNC 为空：仅取档案（用于挂载 ShareBase 本身，调用方需另做共享级校验）。
//
// 返回的 Credential 由调用方负责 Wipe()。
func (s *CredStore) Resolve(id string, targetUNC string) (*Credential, error) {
	if !validCredentialID(id) {
		return nil, credError(ErrCredentialInvalid, "credentialId 非法")
	}

	var (
		shareBase string
		username  string
		domain    sql.NullString
		cipher    []byte
		scopeJSON string
	)
	err := s.db.QueryRow(`SELECT share_base, username, domain, secret_cipher, scope_json
		FROM credentials WHERE credential_id = ?`, id).
		Scan(&shareBase, &username, &domain, &cipher, &scopeJSON)
	if errors.Is(err, sql.ErrNoRows) {
		s.Audit("credential.use", id, protocol.ErrCodeCredentialNotFound, "")
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		s.Audit("credential.use", id, protocol.ErrCodeCredentialInvalid, "读取档案失败")
		return nil, credError(ErrCredentialInvalid, "读取档案失败")
	}

	var scope []string
	_ = json.Unmarshal([]byte(scopeJSON), &scope)

	if strings.TrimSpace(targetUNC) != "" && !ScopeContains(scope, targetUNC) {
		s.Audit("credential.use", id, protocol.ErrCodeCredentialScopeDenied, maskUNC(targetUNC))
		logger.Warn("smb", "credential scope denied: id=%s, target=%s", id, maskUNC(targetUNC))
		return nil, ErrCredentialScopeDenied
	}

	plain, err := winapi.DPAPIUnprotect(cipher)
	if err != nil {
		s.Audit("credential.use", id, protocol.ErrCodeCredentialInvalid, "DPAPI 解密失败")
		logger.Error("smb", "credential resolve failed: id=%s, dpapi error=%v", id, err)
		return nil, credError(ErrCredentialInvalid, "DPAPI 解密失败")
	}

	cred := &Credential{
		ID:        id,
		ShareBase: shareBase,
		Username:  username,
		Domain:    domain.String,
		Scope:     scope,
		secret:    plain,
	}
	s.touchLastUsed(id)
	return cred, nil
}

// VerifyScope 仅做范围校验（不解密），用于任务创建阶段的前置拒绝（含素材根 / 成品目录）。
func (s *CredStore) VerifyScope(id string, targets ...string) error {
	meta, err := s.Get(id)
	if err != nil {
		return err
	}
	for _, t := range targets {
		if strings.TrimSpace(t) == "" {
			continue
		}
		if !ScopeContains(meta.Scope, t) {
			s.Audit("credential.use", id, protocol.ErrCodeCredentialScopeDenied, maskUNC(t))
			logger.Warn("smb", "credential scope denied: id=%s, target=%s", id, maskUNC(t))
			return ErrCredentialScopeDenied
		}
	}
	return nil
}

// VerifyTaskTarget 任务创建阶段的一致性前置校验（07 §5.3）：
//   - shareBase：请求挂载的共享根，须等于档案 ShareBase 或落在其 Scope 内，
//     避免档案被挪用于其它共享/主机；
//   - extra：可选的绝对 UNC 目标（如成品输出目录），相对路径忽略；凡绝对 UNC 必须落在 Scope 内。
//
// 与 Resolve 的差异：不解密、不更新 last_used_at，只做前置拒绝。
func (s *CredStore) VerifyTaskTarget(id, shareBase string, extra ...string) error {
	meta, err := s.Get(id)
	if err != nil {
		return err
	}

	deny := func(target string) error {
		s.Audit("credential.use", id, protocol.ErrCodeCredentialScopeDenied, maskUNC(target))
		logger.Warn("smb", "credential scope denied: id=%s, target=%s", id, maskUNC(target))
		return ErrCredentialScopeDenied
	}

	if base := normalizeUNCPath(shareBase); base != "" {
		base = strings.ToLower(base)
		if base != strings.ToLower(normalizeUNCPath(meta.ShareBase)) && !ScopeContains(meta.Scope, base) {
			return deny(shareBase)
		}
	}

	for _, e := range extra {
		n := strings.ToLower(normalizeUNCPath(e))
		if n == "" || !isUNCPath(n) {
			continue
		}
		if !ScopeContains(meta.Scope, n) {
			return deny(e)
		}
	}
	return nil
}

// ---- 审计 ----

// Audit 写本地审计（07 §7：action / target / result / detail 脱敏）；actor 固定 'local'。
func (s *CredStore) Audit(action, target, result, detail string) {
	s.auditAs("local", action, target, result, detail)
}

// AuditEntry 审计行（07 §7 audit_log 结构）
type AuditEntry struct {
	ID     int64
	TS     string
	Actor  string
	Action string
	Target string
	Result string
	Detail string
}

// AuditFilter 审计查询条件：字段留空表示不过滤；Limit <= 0 时默认 50，按 ts 倒序返回。
type AuditFilter struct {
	Actor  string
	Action string
	Result string
	Limit  int
}

// ListAudit 读取审计（运维排查 / 管理端展示）。仅读，不写。
func (s *CredStore) ListAudit(f AuditFilter) ([]AuditEntry, error) {
	if s.db == nil {
		return nil, fmt.Errorf("credential store not ready")
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	q := `SELECT id, ts, actor, action, COALESCE(target,''), result, COALESCE(detail,'') FROM audit_log WHERE 1=1`
	args := []any{}
	if f.Actor != "" {
		q += ` AND actor = ?`
		args = append(args, f.Actor)
	}
	if f.Action != "" {
		q += ` AND action = ?`
		args = append(args, f.Action)
	}
	if f.Result != "" {
		q += ` AND result = ?`
		args = append(args, f.Result)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Target, &e.Result, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AuditAs 指定 actor 写审计（actor 取来源 IP / 密钥前 8 位哈希 / 'local'）。
func (s *CredStore) AuditAs(actor, action, target, result, detail string) {
	s.auditAs(actor, action, target, result, detail)
}

func (s *CredStore) auditAs(actor, action, target, result, detail string) {
	if s.db == nil {
		return
	}
	if actor == "" {
		actor = "local"
	}
	_, err := s.db.Exec(`INSERT INTO audit_log (ts, actor, action, target, result, detail)
		VALUES (?, ?, ?, ?, ?, ?)`,
		s.timestamp(), actor, action, target, result, detail)
	if err != nil {
		logger.Warn("smb", "audit write failed: action=%s, err=%v", action, err)
	}
}

// AuditEvent 安全/渲染链路的审计入口（07 §7）：供 server 层记录鉴权失败、限流拒绝、
// 任务提交与载荷校验拒绝等事件。审计库未就绪（无凭据库 / CGO 不可用）时降级为 WARN
// 日志，不阻断主流程——审计是旁路能力，不得影响请求可用性。
func AuditEvent(actor, action, target, result, detail string) {
	store := DefaultCredStore()
	if store == nil {
		logger.Warn("smb", "audit skipped (store not ready): actor=%s action=%s target=%s result=%s",
			actor, action, target, result)
		return
	}
	store.AuditAs(actor, action, target, result, detail)
}

func (s *CredStore) purgeExpiredAudit() {
	if s.db == nil {
		return
	}
	cutoff := s.now().AddDate(0, 0, -auditRetentionDays).Format(time.RFC3339)
	if _, err := s.db.Exec(`DELETE FROM audit_log WHERE ts < ?`, cutoff); err != nil {
		logger.Warn("smb", "audit purge failed: %v", err)
	}
}

func (s *CredStore) touchLastUsed(id string) {
	if _, err := s.db.Exec(`UPDATE credentials SET last_used_at = ? WHERE credential_id = ?`, s.timestamp(), id); err != nil {
		logger.Warn("smb", "credential last_used_at update failed: id=%s", id)
	}
}

func (s *CredStore) inUseTasks(id string) []string {
	s.mu.RLock()
	fn := s.inUse
	s.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(id)
}

func (s *CredStore) timestamp() string {
	return s.now().Format(time.RFC3339)
}

// ---- 行扫描与校验辅助 ----

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanCredentialMeta(row rowScanner) (*CredentialMeta, error) {
	var (
		meta       CredentialMeta
		scopeJSON  string
		createdAt  string
		updatedAt  string
		lastUsedAt sql.NullString
		rotatedAt  sql.NullString
	)
	if err := row.Scan(&meta.ID, &meta.ShareBase, &meta.Username, &meta.Domain, &scopeJSON,
		&createdAt, &updatedAt, &lastUsedAt, &rotatedAt); err != nil {
		return nil, err
	}

	_ = json.Unmarshal([]byte(scopeJSON), &meta.Scope)
	meta.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	meta.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if lastUsedAt.Valid {
		if t, err := time.Parse(time.RFC3339, lastUsedAt.String); err == nil {
			meta.LastUsedAt = &t
		}
	}
	if rotatedAt.Valid {
		if t, err := time.Parse(time.RFC3339, rotatedAt.String); err == nil {
			meta.RotatedAt = &t
		}
	}
	return &meta, nil
}

var credentialIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validCredentialID(id string) bool {
	return credentialIDRe.MatchString(id)
}

// normalizePutRequest 校验并归一化入参；返回生效 Scope。
func normalizePutRequest(req *PutCredentialRequest) ([]string, error) {
	req.ID = strings.TrimSpace(req.ID)
	req.ShareBase = strings.TrimSpace(req.ShareBase)
	req.Username = strings.TrimSpace(req.Username)
	req.Domain = strings.TrimSpace(req.Domain)

	if !validCredentialID(req.ID) {
		return nil, credError(ErrCredentialInvalid, "credentialId 缺失或格式非法")
	}
	if !isUNCPath(req.ShareBase) {
		return nil, credError(ErrCredentialInvalid, "shareBase 必须是 UNC 路径（\\\\host\\share）")
	}
	if req.Username == "" {
		return nil, credError(ErrCredentialInvalid, "username 不能为空（禁止使用默认值填充）")
	}
	if req.Password == "" {
		return nil, credError(ErrCredentialInvalid, "password 不能为空（禁止使用默认值填充）")
	}

	scope := make([]string, 0, len(req.Scope))
	for _, s := range req.Scope {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !isUNCPath(s) {
			return nil, credError(ErrCredentialInvalid, "scope 项必须是 UNC 路径")
		}
		if !ScopeWithinShare([]string{s}, req.ShareBase) {
			return nil, credError(ErrCredentialInvalid, "scope 项越出 shareBase")
		}
		scope = append(scope, s)
	}
	// Scope 为空时收敛为 ShareBase 本身（最小可用范围）
	if len(scope) == 0 {
		scope = []string{req.ShareBase}
	}
	req.ShareBase = normalizeUNCPath(req.ShareBase)
	return scope, nil
}

// ---- UNC 归一化与范围判定 ----

// normalizeUNCPath 统一分隔符、去尾部分隔符、转小写（UNC 大小写不敏感）
func normalizeUNCPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "/", `\`)
	p = strings.TrimRight(p, `\`)
	return strings.ToLower(p)
}

// isUNCPath 判定形如 \\host\share 的 UNC 路径
func isUNCPath(p string) bool {
	n := normalizeUNCPath(p)
	if !strings.HasPrefix(n, `\\`) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(n, `\\`), `\`)
	if len(parts) < 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}

// ScopeContains 判断 targetUNC 是否落在 scope 任一条目内（含自身与子路径边界）
func ScopeContains(scope []string, targetUNC string) bool {
	target := normalizeUNCPath(targetUNC)
	if target == "" || !isUNCPath(target) {
		return false
	}
	for _, s := range scope {
		n := normalizeUNCPath(s)
		if n == "" {
			continue
		}
		if target == n || strings.HasPrefix(target, n+`\`) {
			return true
		}
	}
	return false
}

// ScopeWithinShare 判断 scope 是否指向 shareBase 之内（挂载共享前的最小校验，
// 防止档案被用于挂载与其 Scope 无关的共享）。
func ScopeWithinShare(scope []string, shareBase string) bool {
	base := normalizeUNCPath(shareBase)
	if base == "" {
		return false
	}
	for _, s := range scope {
		n := normalizeUNCPath(s)
		if n == "" {
			continue
		}
		if n == base || strings.HasPrefix(n, base+`\`) {
			return true
		}
	}
	return false
}

// maskUNC 日志/审计用脱敏 UNC：只保留 \\host\share 前缀
func maskUNC(p string) string {
	n := strings.ReplaceAll(strings.TrimSpace(p), "/", `\`)
	if !strings.HasPrefix(n, `\\`) {
		return "<path>"
	}
	parts := strings.Split(strings.TrimPrefix(n, `\\`), `\`)
	if len(parts) < 2 {
		return `<share>`
	}
	return `\\` + parts[0] + `\` + parts[1]
}

// ---- 挂载入口（07 §5.2：凭据只在节点本机解密，用完即弃）----

// MountSMBShareWithCredential 用本地档案挂载共享。
// 校验：档案存在 → Scope 覆盖该共享 → DPAPI 解密 → net use → Wipe。
func MountSMBShareWithCredential(smbPath, credentialID string) (string, error) {
	store := DefaultCredStore()
	if store == nil {
		return "", credError(ErrCredentialInvalid, "凭据库未初始化")
	}

	cred, err := store.Resolve(credentialID, "")
	if err != nil {
		return "", err
	}
	defer cred.Wipe()

	if !ScopeWithinShare(cred.Scope, smbPath) {
		store.Audit("credential.use", credentialID, protocol.ErrCodeCredentialScopeDenied, maskUNC(smbPath))
		logger.Warn("smb", "credential mount denied: id=%s, share=%s", credentialID, maskUNC(smbPath))
		return "", ErrCredentialScopeDenied
	}

	logger.Info("smb", "mounting with credential: id=%s, share=%s", credentialID, maskUNC(smbPath))
	mountPath, err := MountSMBShare(smbPath, cred.LoginName(), cred.Secret())
	if err != nil {
		// 挂载失败（拒绝访问等）→ 07 §5.4 失效检测口径
		store.Audit("credential.use", credentialID, protocol.ErrCodeCredentialInvalid, maskUNC(smbPath))
		return "", fmt.Errorf("%w: 挂载失败", ErrCredentialInvalid)
	}
	return mountPath, nil
}

// ErrCredentialStoreUnavailable 档案库不可用
var ErrCredentialStoreUnavailable = errors.New("E_CREDENTIAL_STORE_UNAVAILABLE: 凭据库未初始化")
