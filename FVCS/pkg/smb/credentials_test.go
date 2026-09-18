package smb

// D-03 单测：DPAPI 档案密文落库、Scope 校验、删除占用保护、挂载前范围判定（07 §5.3/§5.4/§5.6）
//
// 覆盖项对应 08 §4.4 D-03 完成判据：
//  1. 落库为密文（db 文件字节中不出现明文口令）
//  2. Resolve 越 Scope 被拒（E_CREDENTIAL_SCOPE_DENIED）
//  3. secret 只经内存传递，Wipe 后不可再取
//  4. 被运行中任务引用时拒绝删除（E_CREDENTIAL_IN_USE）

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Fnos.VC_Service/pkg/protocol"
)

const (
	testPassword = "P@ssw0rd-明文不应落库"
	testShare    = `\\nas-01\media`
	testScope    = `\\nas-01\media\footage`
)

func newTestStore(t *testing.T) (*CredStore, string) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "credentials.db")

	store, err := OpenCredStore(dbPath)
	if err != nil {
		t.Fatalf("OpenCredStore(%s) 失败: %v", dbPath, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dbPath
}

func putTestCredential(t *testing.T, store *CredStore, id string) {
	t.Helper()

	err := store.Put(PutCredentialRequest{
		ID:        id,
		ShareBase: testShare,
		Username:  "nasuser",
		Password:  testPassword,
		Scope:     []string{testScope},
	})
	if err != nil {
		t.Fatalf("Put(%s) 失败: %v", id, err)
	}
}

// D-03-#1 落库为密文：共享库文件（含 WAL）字节中不得出现明文口令
func TestCredentialStoredAsCiphertext(t *testing.T) {
	store, dbPath := newTestStore(t)
	putTestCredential(t, store, "cred_nas01")

	// 触发 WAL 落盘，确保密文已写入磁盘文件
	store.Audit("credential.verify", "cred_nas01", "ok", "")

	dir := filepath.Dir(dbPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取库目录失败: %v", err)
	}

	var scanned int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), filepath.Base(dbPath)) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", e.Name(), err)
		}
		scanned++
		if bytes.Contains(data, []byte(testPassword)) {
			t.Fatalf("明文口令出现在库文件 %s 中（DPAPI 未生效）", e.Name())
		}
	}

	if scanned == 0 {
		t.Fatal("未扫描到任何凭据库文件，测试无效")
	}
}

// D-03-#1 附带：Get/List 只返回元数据，不含 secret 字段
func TestCredentialMetaHasNoSecret(t *testing.T) {
	store, _ := newTestStore(t)
	putTestCredential(t, store, "cred_nas01")

	meta, err := store.Get("cred_nas01")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if meta.Username != "nasuser" {
		t.Errorf("Username = %q, 期望 nasuser", meta.Username)
	}
	if len(meta.Scope) != 1 || meta.Scope[0] != testScope {
		t.Errorf("Scope = %v, 期望 [%s]", meta.Scope, testScope)
	}
	if meta.RotatedAt != nil {
		t.Errorf("首次创建不应带 rotatedAt，实际 %v", meta.RotatedAt)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List 长度 = %d, 期望 1", len(list))
	}
}

// D-03-#1 轮换：同 ID 二次 Put 更新密文并置 rotatedAt
func TestCredentialRotate(t *testing.T) {
	store, _ := newTestStore(t)
	putTestCredential(t, store, "cred_nas01")

	before, err := store.Get("cred_nas01")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}

	if err := store.Put(PutCredentialRequest{
		ID:        "cred_nas01",
		ShareBase: testShare,
		Username:  "nasuser2",
		Password:  "new-secret-1",
		Scope:     []string{testShare},
	}); err != nil {
		t.Fatalf("轮换 Put 失败: %v", err)
	}

	after, err := store.Get("cred_nas01")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if after.Username != "nasuser2" {
		t.Errorf("轮换后 Username = %q, 期望 nasuser2", after.Username)
	}
	if after.RotatedAt == nil {
		t.Error("轮换后 rotatedAt 应非空")
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("轮换不应改变 createdAt: before=%v after=%v", before.CreatedAt, after.CreatedAt)
	}

	cred, err := store.Resolve("cred_nas01", testShare)
	if err != nil {
		t.Fatalf("轮换后 Resolve 失败: %v", err)
	}
	defer cred.Wipe()
	if got := cred.Secret(); got != "new-secret-1" {
		t.Errorf("轮换后 secret = %q, 期望 new-secret-1", got)
	}
}

// D-03-#2 越 Scope 被拒：Resolve 与 VerifyScope 均需返回 E_CREDENTIAL_SCOPE_DENIED
func TestCredentialScopeDenied(t *testing.T) {
	store, _ := newTestStore(t)
	putTestCredential(t, store, "cred_nas01")

	outside := []string{
		`\\nas-01\media\other`,   // 同共享、越出 scope 子目录
		`\\nas-01\other`,         // 同主机、不同共享
		`\\nas-02\media\footage`, // 不同主机
	}
	for _, target := range outside {
		if _, err := store.Resolve("cred_nas01", target); !errors.Is(err, ErrCredentialScopeDenied) {
			t.Errorf("Resolve(%s) 应被拒，实际 err=%v", target, err)
		}
		if err := store.VerifyScope("cred_nas01", target); !errors.Is(err, ErrCredentialScopeDenied) {
			t.Errorf("VerifyScope(%s) 应被拒，实际 err=%v", target, err)
		}
	}

	// 错误码需与 protocol 对齐，便于直接回包
	if got := ErrCredentialScopeDenied.Error(); !strings.HasPrefix(got, protocol.ErrCodeCredentialScopeDenied) {
		t.Errorf("错误码前缀 = %q, 期望 %s", got, protocol.ErrCodeCredentialScopeDenied)
	}

	// 越界时不得触及解密：档案仍可正常读元数据
	if _, err := store.Get("cred_nas01"); err != nil {
		t.Errorf("越界拒绝后 Get 失败: %v", err)
	}
}

// D-03-#2 边界：scope 前缀不得误放行（\\nas-01\media\footage2）
func TestCredentialScopePrefixBoundary(t *testing.T) {
	store, _ := newTestStore(t)
	putTestCredential(t, store, "cred_nas01")

	if _, err := store.Resolve("cred_nas01", `\\nas-01\media\footage2`); !errors.Is(err, ErrCredentialScopeDenied) {
		t.Errorf("兄弟目录不应命中 scope，实际 err=%v", err)
	}

	// 命中 scope 自身与子路径均放行
	for _, target := range []string{`\\nas-01\media\footage`, `\\NAS-01\Media\Footage\2026\a.mov`, `//nas-01/media/footage/sub`} {
		cred, err := store.Resolve("cred_nas01", target)
		if err != nil {
			t.Errorf("Resolve(%s) 应放行，实际 err=%v", target, err)
			continue
		}
		cred.Wipe()
	}
}

// D-03-#3 secret 只在内存：Wipe 后不可再取，且 Resolve 不解密进任何持久字段
func TestCredentialWipeClearsSecret(t *testing.T) {
	store, _ := newTestStore(t)
	putTestCredential(t, store, "cred_nas01")

	cred, err := store.Resolve("cred_nas01", testScope)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if cred.Secret() != testPassword {
		t.Fatalf("secret 解密结果不符")
	}
	if cred.LoginName() != "nasuser" {
		t.Errorf("LoginName = %q, 期望 nasuser", cred.LoginName())
	}

	cred.Wipe()
	if got := cred.Secret(); got != "" {
		t.Errorf("Wipe 后仍可取到 secret: %q", got)
	}

	// last_used_at 应被回填，且不携带任何 secret 内容
	meta, err := store.Get("cred_nas01")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if meta.LastUsedAt == nil {
		t.Error("Resolve 后 lastUsedAt 应非空")
	}
}

// D-03-#3 带域账号的登录名组装
func TestCredentialLoginNameWithDomain(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.Put(PutCredentialRequest{
		ID:        "cred_dom",
		ShareBase: testShare,
		Username:  "nasuser",
		Domain:    "CORP",
		Password:  "dom-secret-1",
		Scope:     []string{testShare},
	}); err != nil {
		t.Fatalf("Put 失败: %v", err)
	}

	cred, err := store.Resolve("cred_dom", testShare)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	defer cred.Wipe()
	if want := `CORP\nasuser`; cred.LoginName() != want {
		t.Errorf("LoginName = %q, 期望 %q", cred.LoginName(), want)
	}
}

// D-03-#4 被运行中任务引用 → 拒绝删除
func TestCredentialDeleteRejectedWhenInUse(t *testing.T) {
	store, _ := newTestStore(t)
	putTestCredential(t, store, "cred_nas01")

	store.SetInUseChecker(func(id string) []string {
		if id == "cred_nas01" {
			return []string{"task_a", "task_b"}
		}
		return nil
	})

	if err := store.Delete("cred_nas01"); !errors.Is(err, ErrCredentialInUse) {
		t.Fatalf("占用中删除应被拒，实际 err=%v", err)
	}
	if got := ErrCredentialInUse.Error(); !strings.HasPrefix(got, protocol.ErrCodeCredentialInUse) {
		t.Errorf("错误码前缀 = %q, 期望 %s", got, protocol.ErrCodeCredentialInUse)
	}

	// 释放占用后可删除
	store.SetInUseChecker(func(string) []string { return nil })
	if err := store.Delete("cred_nas01"); err != nil {
		t.Fatalf("释放后删除失败: %v", err)
	}
	if _, err := store.Get("cred_nas01"); !errors.Is(err, ErrCredentialNotFound) {
		t.Errorf("删除后 Get 应返回 not found，实际 err=%v", err)
	}
}

// D-03-#4 删除不存在档案 / 非法 ID
func TestCredentialInvalidAndNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	putTestCredential(t, store, "cred_nas01")

	if _, err := store.Get("cred_missing"); !errors.Is(err, ErrCredentialNotFound) {
		t.Errorf("Get 不存在档案应返回 not found，实际 err=%v", err)
	}
	if got := ErrCredentialNotFound.Error(); !strings.HasPrefix(got, protocol.ErrCodeCredentialNotFound) {
		t.Errorf("错误码前缀 = %q, 期望 %s", got, protocol.ErrCodeCredentialNotFound)
	}

	for _, bad := range []string{"", " ", "../etc/passwd", `nas\share`, strings.Repeat("a", 65)} {
		if err := store.Delete(bad); !errors.Is(err, ErrCredentialInvalid) {
			t.Errorf("Delete(%q) 应报 invalid，实际 err=%v", bad, err)
		}
		if _, err := store.Resolve(bad, ""); !errors.Is(err, ErrCredentialInvalid) {
			t.Errorf("Resolve(%q) 应报 invalid，实际 err=%v", bad, err)
		}
	}
}

// D-03-#5 Put 入参校验：禁止编造凭据（空口令/空用户名/非 UNC/越界 scope 直接拒）
func TestPutCredentialValidation(t *testing.T) {
	store, _ := newTestStore(t)

	cases := []struct {
		name string
		req  PutCredentialRequest
	}{
		{"空口令", PutCredentialRequest{ID: "c1", ShareBase: testShare, Username: "u", Password: ""}},
		{"空用户名", PutCredentialRequest{ID: "c2", ShareBase: testShare, Username: "", Password: "p"}},
		{"shareBase 非 UNC", PutCredentialRequest{ID: "c3", ShareBase: `D:\media`, Username: "u", Password: "p"}},
		{"scope 越出 shareBase", PutCredentialRequest{ID: "c4", ShareBase: testShare, Username: "u", Password: "p", Scope: []string{`\\nas-02\media`}}},
		{"credentialId 非法", PutCredentialRequest{ID: "..bad id", ShareBase: testShare, Username: "u", Password: "p"}},
	}
	for _, c := range cases {
		if err := store.Put(c.req); !errors.Is(err, ErrCredentialInvalid) {
			t.Errorf("%s: 应报 invalid，实际 err=%v", c.name, err)
		}
	}
	if err := store.Put(cases[0].req); err != nil {
		t.Logf("（首次调用已计入 audit，重复无碍）")
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("非法入参不应落库，实际 %d 条", len(list))
	}
}

// D-03-#5 审计写入 + 超期清理（180 天）
func TestAuditRetention(t *testing.T) {
	store, _ := newTestStore(t)

	old := time.Now().AddDate(0, 0, -200)
	store.mu.Lock()
	store.now = func() time.Time { return old }
	store.mu.Unlock()
	store.Audit("credential.put", "cred_old", "ok", "")

	store.mu.Lock()
	store.now = time.Now
	store.mu.Unlock()
	store.Audit("credential.put", "cred_new", "ok", "")
	store.purgeExpiredAudit()

	var expired, fresh int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE target = 'cred_old'`).Scan(&expired); err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE target = 'cred_new'`).Scan(&fresh); err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if expired != 0 {
		t.Errorf("超期审计应被清理，实际残留 %d 条", expired)
	}
	if fresh != 1 {
		t.Errorf("正常审计应保留 1 条，实际 %d 条", fresh)
	}
}

// D-03-#5 UNC 归一化与范围判定的纯函数边界
func TestScopeHelpers(t *testing.T) {
	if !ScopeContains([]string{testScope}, `\\NAS-01/media/footage/sub/`) {
		t.Error("大小写与斜杠差异应归一化后命中")
	}
	if ScopeContains([]string{testScope}, `\\nas-01\media\footage2`) {
		t.Error("前缀相似但不属子路径，不应命中")
	}
	if ScopeContains([]string{testScope}, "relative/path") {
		t.Error("非 UNC 目标不应命中")
	}
	if ScopeContains(nil, testScope) {
		t.Error("空 scope 不应命中任何目标")
	}
	if !ScopeWithinShare([]string{testScope}, testShare) {
		t.Error("scope 位于 shareBase 内应通过")
	}
	if ScopeWithinShare([]string{`\\nas-01\media-extra`}, testShare) {
		t.Error("shareBase 前缀相似（media-extra）不应通过")
	}
	if got := maskUNC(`\\nas-01\media\footage\secret.mov`); got != `\\nas-01\media` {
		t.Errorf("maskUNC = %q, 期望 \\\\nas-01\\media", got)
	}
	if got := maskUNC(`D:\media\a.mov`); got != "<path>" {
		t.Errorf("非 UNC 脱敏 = %q, 期望 <path>", got)
	}
}

// D-03：默认实例注入（本机接口依赖 DefaultCredStore 非空）
func TestDefaultCredStoreInjection(t *testing.T) {
	prev := DefaultCredStore()
	t.Cleanup(func() { SetDefaultCredStore(prev) })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	store, err := NewCredStore(db)
	if err != nil {
		t.Fatalf("NewCredStore 失败: %v", err)
	}
	SetDefaultCredStore(store)
	if DefaultCredStore() != store {
		t.Error("SetDefaultCredStore 未生效")
	}
}
