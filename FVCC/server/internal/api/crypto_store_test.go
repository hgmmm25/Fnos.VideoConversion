package api

// P0-1 凭据加密 Store 集成测试：落盘密文与读盘解密、旧明文自动迁移。
// （crypto 纯单元测试已随实现迁入 internal/security，本文件随 store 迁移。）

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fvcc/internal/security"
)

func TestCryptoStorePersistence(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 主密钥应已生成
	keyPath := filepath.Join(dir, security.SecretKeyFile)
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("主密钥文件未生成: %v", err)
	}

	// 写入含凭据的设置与服务器
	st := DefaultSettings()
	st.SMBPassword = "smb-pass-123"
	s.SaveSettings(st)
	sv := Server{ID: "sv-1", Name: "node1", IP: "10.0.0.2", Port: 9000, AuthKey: "auth-secret-456", Status: "offline"}
	s.UpsertServer(sv)

	// P0-2 提交 3：主模式落 SQLite，settings.json/server.json 不再更新。
	// 密文落盘断言由 store 包 TestSQLiteCryptoPersist 覆盖（见 sqlite_crypto_test.go）。
	// 此处保留核心链路：重载后明文还原。
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load2: %v", err)
	}
	if got := s2.GetSettings().SMBPassword; got != "smb-pass-123" {
		t.Fatalf("重新加载后 SMBPassword 未还原: got %q", got)
	}
	if svv, ok := s2.GetServer("sv-1"); !ok || svv.AuthKey != "auth-secret-456" {
		t.Fatalf("重新加载后 AuthKey 未还原: ok=%v key=%q", ok, svv.AuthKey)
	}
}

func TestCryptoStoreLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	// 手工构造旧版明文文件
	legacySettings := SettingsFile{Version: 1, Settings: Settings{SMBPassword: "old-plain"}}
	b, _ := json.Marshal(legacySettings)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), b, 0o644); err != nil {
		t.Fatalf("写旧版 settings.json: %v", err)
	}
	legacyServers := ServersFile{Version: 1, Servers: []Server{{ID: "sv-old", AuthKey: "old-key"}}}
	b2, _ := json.Marshal(legacyServers)
	if err := os.WriteFile(filepath.Join(dir, "server.json"), b2, 0o644); err != nil {
		t.Fatalf("写旧版 server.json: %v", err)
	}

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := s.GetSettings().SMBPassword; got != "old-plain" {
		t.Fatalf("旧明文迁移失败: got %q", got)
	}
	if svv, ok := s.GetServer("sv-old"); !ok || svv.AuthKey != "old-key" {
		t.Fatalf("旧明文 AuthKey 迁移失败: ok=%v key=%q", ok, svv.AuthKey)
	}

	// 触发一次落盘后应自动加密迁移（P0-2 提交 3：落 SQLite，密文断言见 store 包
	// sqlite_crypto_test.go；此处验证重载后明文还原）
	s.SaveSettings(s.GetSettings())
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load2: %v", err)
	}
	if got := s2.GetSettings().SMBPassword; got != "old-plain" {
		t.Fatalf("迁移落盘后重载明文未还原: got %q", got)
	}
	if svv, ok := s2.GetServer("sv-old"); !ok || svv.AuthKey != "old-key" {
		t.Fatalf("迁移落盘后重载 AuthKey 未还原: ok=%v key=%q", ok, svv.AuthKey)
	}
}
