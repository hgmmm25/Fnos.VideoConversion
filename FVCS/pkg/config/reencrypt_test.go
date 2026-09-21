package config

// M3 迁移测试（07 §5.5）：`fvcs-cli reencrypt` 的底层实现 ReencryptFile / NeedsReencrypt。
//  1) legacy 密文 → 判定需要迁移、dry-run 不写盘、正式执行后可用主密钥解出同一明文；
//  2) 非 legacy 密文 → 幂等 no-op，文件字节不变；
//  3) 损坏文件 / 不存在的路径 → 报错且不产生新文件（不覆盖为垃圾内容）。

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sealWithLegacy 用旧硬编码密钥加密（模拟 M3 之前的存量配置文件）。
func sealWithLegacy(t *testing.T, plain []byte) []byte {
	t.Helper()
	data, err := seal(legacyEncryptKey, plain)
	if err != nil {
		t.Fatalf("legacy 加密失败: %v", err)
	}
	return data
}

func TestM3ReencryptFileMigratesLegacy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.enc.json")

	plain, err := json.MarshalIndent(Config{WsPort: 9090, QpsLimit: 120, LogLevel: "INFO"}, "", "  ")
	if err != nil {
		t.Fatalf("构造明文失败: %v", err)
	}
	legacyBlob := sealWithLegacy(t, plain)
	if err := os.WriteFile(path, legacyBlob, 0o644); err != nil {
		t.Fatalf("写入 legacy 配置失败: %v", err)
	}

	// 1) 判定：需要迁移
	need, err := NeedsReencrypt(path)
	if err != nil || !need {
		t.Fatalf("legacy 文件应判定需要迁移: need=%v err=%v", need, err)
	}

	// 2) dry-run：只报告不写盘
	rewritten, err := ReencryptFile(path, true)
	if err != nil || !rewritten {
		t.Fatalf("dry-run 应报告需要迁移: rewritten=%v err=%v", rewritten, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !bytes.Equal(after, legacyBlob) {
		t.Fatal("dry-run 不得改动配置文件")
	}
	if tmp := path + ".m3tmp"; fileExists(tmp) {
		t.Fatal("dry-run 不得留下临时文件")
	}

	// 3) 正式迁移：重写且内容不变
	rewritten, err = ReencryptFile(path, false)
	if err != nil || !rewritten {
		t.Fatalf("迁移应成功: rewritten=%v err=%v", rewritten, err)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移后配置失败: %v", err)
	}
	if bytes.Equal(migrated, legacyBlob) {
		t.Fatal("迁移后文件内容应发生变化（新密钥 + 新 nonce）")
	}
	if tmp := path + ".m3tmp"; fileExists(tmp) {
		t.Fatal("迁移完成后不得残留临时文件")
	}

	got, usedLegacy, err := decryptForLoad(migrated)
	if err != nil {
		t.Fatalf("迁移后应可解密: %v", err)
	}
	if usedLegacy {
		t.Fatal("迁移后不应再走 legacy 密钥")
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("迁移前后明文必须一致\n期望: %s\n实际: %s", plain, got)
	}

	// 4) 迁移后判定为无需迁移
	need, err = NeedsReencrypt(path)
	if err != nil || need {
		t.Fatalf("迁移后应判定无需迁移: need=%v err=%v", need, err)
	}
}

func TestM3ReencryptFileIdempotentOnMasterKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.enc.json")

	sealed, err := encrypt([]byte(`{"ws_port":8080}`))
	if err != nil {
		t.Fatalf("主密钥加密失败: %v", err)
	}
	if err := os.WriteFile(path, sealed, 0o644); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	need, err := NeedsReencrypt(path)
	if err != nil || need {
		t.Fatalf("主密钥密文不应判定需要迁移: need=%v err=%v", need, err)
	}
	rewritten, err := ReencryptFile(path, false)
	if err != nil || rewritten {
		t.Fatalf("非 legacy 文件应为 no-op: rewritten=%v err=%v", rewritten, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !bytes.Equal(after, sealed) {
		t.Fatal("no-op 分支不得改动配置文件")
	}
}

func TestM3ReencryptFileErrors(t *testing.T) {
	dir := t.TempDir()

	// 1) 文件不存在
	if _, err := ReencryptFile(filepath.Join(dir, "nope.json"), false); err == nil {
		t.Fatal("不存在的文件应返回错误")
	}

	// 2) 损坏 / 非法密文：报错且不改动原文件
	broken := filepath.Join(dir, "broken.json")
	garbage := []byte("not-a-valid-gcm-blob")
	if err := os.WriteFile(broken, garbage, 0o644); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}
	if _, err := ReencryptFile(broken, false); err == nil {
		t.Fatal("非法密文应返回错误")
	}
	after, err := os.ReadFile(broken)
	if err != nil {
		t.Fatalf("读取损坏文件失败: %v", err)
	}
	if !bytes.Equal(after, garbage) {
		t.Fatal("解密失败时不得覆盖原文件")
	}
	if tmp := broken + ".m3tmp"; fileExists(tmp) {
		t.Fatal("失败路径不得残留临时文件")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
