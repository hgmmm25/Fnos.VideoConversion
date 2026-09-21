package config

// D-04 / 07 §5.3 验收测试：DPAPI 主密钥落盘 + 旧硬编码密钥（M3 迁移）兼容解密。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// 主密钥可用时：加解密走 DPAPI 主密钥，且解密结果标记为非 legacy
func TestD04MasterKeyRoundTrip(t *testing.T) {
	if _, err := masterKey(); err != nil {
		t.Fatalf("主密钥不可用: %v", err)
	}

	plain := []byte(`{"ws_port":18080,"secret":"s3cr3t"}`)
	enc, err := encrypt(plain)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	got, usedLegacy, err := decryptForLoad(enc)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if usedLegacy {
		t.Fatalf("主密钥密文不应被判定为 legacy")
	}
	if string(got) != string(plain) {
		t.Fatalf("解密结果不一致: %s", got)
	}
}

// M3 迁移：旧硬编码密钥密文仍可解密，且被标记为需要重写
func TestD04LegacyKeyMigration(t *testing.T) {
	plain := []byte(`{"ws_port":18081}`)
	enc, err := seal(legacyEncryptKey, plain)
	if err != nil {
		t.Fatalf("旧密钥加密失败: %v", err)
	}

	got, usedLegacy, err := decryptForLoad(enc)
	if err != nil {
		t.Fatalf("旧密钥密文应可解密: %v", err)
	}
	if !usedLegacy {
		t.Fatalf("旧密钥密文应被标记为 legacy（触发重写）")
	}
	if string(got) != string(plain) {
		t.Fatalf("解密结果不一致: %s", got)
	}
}

// 密文被篡改（GCM 校验失败）应返回 ErrConfigDecrypt，而非静默回退
func TestD04TamperedCiphertextRejected(t *testing.T) {
	enc, err := encrypt([]byte(`{"ws_port":18082}`))
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	enc[len(enc)-1] ^= 0xFF

	if _, _, err := decryptForLoad(enc); err == nil {
		t.Fatalf("被篡改的密文必须解密失败")
	}
}

// 主密钥惰性生成：文件存在、内容为 32 字节并可跨进程复用
func TestD04MasterKeyFileStable(t *testing.T) {
	k1, err := masterKey()
	if err != nil {
		t.Fatalf("主密钥生成失败: %v", err)
	}
	if len(k1) != aesKeySize {
		t.Fatalf("主密钥长度应为 %d，实际 %d", aesKeySize, len(k1))
	}

	// 复位惰性缓存后重新读取，应得到同一密钥（落盘生效）
	masterKeyOnce = sync.Once{}
	masterKeyErr = nil
	masterKeyVal = nil
	k2, err := masterKey()
	if err != nil {
		t.Fatalf("重新读取主密钥失败: %v", err)
	}
	if string(k1) != string(k2) {
		t.Fatalf("主密钥未稳定落盘：两次读取不一致")
	}
}

// 旧版配置缺失新增字段时的兜底（不覆盖用户显式值）
func TestD04NormalizeDefaults(t *testing.T) {
	cfg := &Config{MaxWSConns: 0, QpsLimit: 0, ListenAddr: "  192.168.1.5  "}
	normalizeDefaults(cfg)
	if cfg.MaxWSConns != defaultMaxWSConns {
		t.Errorf("MaxWSConns 兜底应为 %d，实际 %d", defaultMaxWSConns, cfg.MaxWSConns)
	}
	if cfg.QpsLimit != 100 {
		t.Errorf("QpsLimit 兜底应为 100，实际 %d", cfg.QpsLimit)
	}
	if cfg.ListenAddr != "192.168.1.5" {
		t.Errorf("ListenAddr 应被 trim，实际 %q", cfg.ListenAddr)
	}

	cfg2 := &Config{MaxWSConns: 5, QpsLimit: 300}
	normalizeDefaults(cfg2)
	if cfg2.MaxWSConns != 5 || cfg2.QpsLimit != 300 {
		t.Errorf("显式配置不应被覆盖: %+v", cfg2)
	}
}

// 落盘配置文件应可用主密钥解出（Save → Load 往返）
func TestD04SaveLoadRoundTrip(t *testing.T) {
	orig := Get()
	defer func() {
		Set(orig)
		_ = Save()
	}()

	cfg := *orig
	cfg.WsPort = 19099
	cfg.MaxWSConns = 2
	Set(&cfg)
	if err := Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(getAppDir(), configFileName))
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}
	plain, usedLegacy, err := decryptForLoad(data)
	if err != nil {
		t.Fatalf("落盘配置应可用主密钥解密: %v", err)
	}
	if usedLegacy {
		t.Fatalf("新写入的配置不应使用旧密钥")
	}

	var reloaded Config
	if err := json.Unmarshal(plain, &reloaded); err != nil {
		t.Fatalf("配置内容不可解析: %v", err)
	}
	if reloaded.WsPort != 19099 || reloaded.MaxWSConns != 2 {
		t.Fatalf("往返后配置不一致: %+v", reloaded)
	}
}
