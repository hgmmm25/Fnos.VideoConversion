package main

// P0-1 凭据加密测试：往返、旧明文兼容、脱敏、Store 落盘密文与读盘解密。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCryptoRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef") // 32B
	enc, err := EncryptSecret(key, "s3cret-pass")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if !strings.HasPrefix(enc, secretPrefix) {
		t.Fatalf("密文缺少前缀: %q", enc)
	}
	if strings.Contains(enc, "s3cret-pass") {
		t.Fatalf("密文中泄露明文")
	}
	dec, err := DecryptSecret(key, enc)
	if err != nil {
		t.Fatalf("DecryptSecret: %v", err)
	}
	if dec != "s3cret-pass" {
		t.Fatalf("往返不一致: got %q want %q", dec, "s3cret-pass")
	}
	// 两次加密产出不同密文（随机 nonce）
	enc2, _ := EncryptSecret(key, "s3cret-pass")
	if enc == enc2 {
		t.Fatalf("随机 nonce 失效：两次密文相同")
	}
}

func TestCryptoLegacyPlaintext(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	// 旧明文（无前缀）应原样返回，兼容迁移
	dec, err := DecryptSecret(key, "plain-old-pass")
	if err != nil {
		t.Fatalf("DecryptSecret(plaintext): %v", err)
	}
	if dec != "plain-old-pass" {
		t.Fatalf("旧明文未原样保留: got %q", dec)
	}
	if IsEncrypted("plain-old-pass") {
		t.Fatalf("旧明文不应判定为密文")
	}
	// 空值
	if enc, _ := EncryptSecret(key, ""); enc != "" {
		t.Fatalf("空明文应返回空串, got %q", enc)
	}
	if dec, _ := DecryptSecret(key, ""); dec != "" {
		t.Fatalf("空密文应返回空串, got %q", dec)
	}
}

func TestCryptoBadKey(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	enc, _ := EncryptSecret(key, "secret")
	other := []byte("fedcba9876543210fedcba9876543210")
	if _, err := DecryptSecret(other, enc); err == nil {
		t.Fatalf("错误密钥应解密失败")
	}
}

func TestCryptoMask(t *testing.T) {
	if MaskSecret("") != "" {
		t.Fatalf("空值脱敏应保持空")
	}
	if MaskSecret("abc") != secretMaskValue {
		t.Fatalf("非空脱敏应为掩码")
	}
}

func TestCryptoStorePersistence(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 主密钥应已生成
	keyPath := filepath.Join(dir, secretKeyFile)
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
	if !strings.Contains(string(rawSettings), secretPrefix) {
		t.Fatalf("settings.json 未加密落盘")
	}
	if strings.Contains(string(rawSettings), "smb-pass-123") {
		t.Fatalf("settings.json 泄露明文 SMBPassword")
	}
	if !strings.Contains(string(rawServers), secretPrefix) {
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
	if !strings.Contains(string(raw), secretPrefix) || strings.Contains(string(raw), "old-plain") {
		t.Fatalf("旧明文未自动加密迁移")
	}
}
