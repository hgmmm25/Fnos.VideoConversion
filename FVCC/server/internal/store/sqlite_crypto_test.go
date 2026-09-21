package store

// P0-2 提交 3 补充测试：SQLite 主模式下，含凭据的 settings/servers 落盘为密文
// （SecretPrefix 前缀 + 无明文泄露），重载后明文还原。
// （api 包 crypto_store_test.go 不再断言 JSON 文件密文，改由本文件覆盖。）

import (
	"strings"
	"testing"

	"fvcc/internal/security"
	"fvcc/internal/store/model"
)

func TestSQLiteCryptoPersist(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	st := model.DefaultSettings()
	st.SMBPassword = "smb-pass-123"
	s.SaveSettings(st)
	s.UpsertServer(model.Server{
		ID: "sv-1", Name: "node1", IP: "10.0.0.2", Port: 9000,
		AuthKey: "auth-secret-456", Status: "offline",
	})

	sq, err := openSQLite(dir)
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	defer sq.Close()

	var rawSettings string
	if err := sq.db.QueryRow(`SELECT data FROM settings WHERE id='default'`).Scan(&rawSettings); err != nil {
		t.Fatalf("读 settings 表: %v", err)
	}
	if !strings.Contains(rawSettings, security.SecretPrefix) {
		t.Fatalf("settings 未加密落盘（缺 SecretPrefix）: %s", rawSettings)
	}
	if strings.Contains(rawSettings, "smb-pass-123") {
		t.Fatalf("settings 泄露明文 SMBPassword: %s", rawSettings)
	}

	var rawServer string
	if err := sq.db.QueryRow(`SELECT data FROM servers WHERE id='sv-1'`).Scan(&rawServer); err != nil {
		t.Fatalf("读 servers 表: %v", err)
	}
	if !strings.Contains(rawServer, security.SecretPrefix) {
		t.Fatalf("servers 未加密落盘（缺 SecretPrefix）: %s", rawServer)
	}
	if strings.Contains(rawServer, "auth-secret-456") {
		t.Fatalf("servers 泄露明文 AuthKey: %s", rawServer)
	}

	// 重载后明文还原
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load2: %v", err)
	}
	if got := s2.GetSettings().SMBPassword; got != "smb-pass-123" {
		t.Fatalf("重载后 SMBPassword 未还原: got %q", got)
	}
	if sv, ok := s2.GetServer("sv-1"); !ok || sv.AuthKey != "auth-secret-456" {
		t.Fatalf("重载后 AuthKey 未还原: ok=%v key=%q", ok, sv.AuthKey)
	}
}
