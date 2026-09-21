package security

// P0-1 凭据加密测试（纯单元部分）：往返、旧明文兼容、脱敏。
// Store 落盘集成部分留在 server/crypto_store_test.go（随 store 迁移）。

import (
	"strings"
	"testing"
)

func TestCryptoRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef") // 32B
	enc, err := EncryptSecret(key, "s3cret-pass")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if !strings.HasPrefix(enc, SecretPrefix) {
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
	if MaskSecret("abc") != SecretMaskValue {
		t.Fatalf("非空脱敏应为掩码")
	}
}
