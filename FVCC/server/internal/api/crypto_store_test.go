package api

// P0-1 凭据加密 Store 集成测试：落盘密文与读盘解密、旧明文自动迁移。
// （crypto 纯单元测试已随实现迁入 internal/security，本文件随 store 迁移。）

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

	// 落盘文件应含密文前缀且不含明文
	rawSettings, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	rawServers, _ := os.ReadFile(filepath.Join(dir, "server.json"))
	if !strings.Contains(string(rawSettings), security.SecretPrefix) {
		t.Fatalf("settings.json 未加密落盘")
	}
	if strings.Contains(string(rawSettings), "smb-pass-123") {
		t.Fatalf("settings.json 泄露明文 SMBPassword")
	}
	if !strings.Contains(string(rawServers), security.SecretPrefix) {
		t.Fatalf("server.json 未加密落盘")
	}
	if strings.Contains(string(rawServers), "auth-secret-456") {
		t.Fatalf("server.json 泄露明文 AuthKey")
	}

	// 重新 Load：内存态应还原明文
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

	// 触发一次落盘后应自动加密迁移
	s.SaveSettings(s.GetSettings())
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if !strings.Contains(string(raw), security.SecretPrefix) || strings.Contains(string(raw), "old-plain") {
		t.Fatalf("旧明文未自动加密迁移")
	}
}
