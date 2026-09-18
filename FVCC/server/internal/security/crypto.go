// Package security 承载安全基座（P2-1 阶段 A：crypto/smb_validate/gateway 迁入）。
// 依赖方向：仅依赖标准库、fvcc/logger、internal/store/model；禁止反向依赖上层包。
package security

// ===== P0-1 凭据加密（FVCC_混乱度评价报告 P0-1）=====
// AES-GCM 对称加密：主密钥以 0600 权限落盘 <dataDir>/secret.key，
// 密文格式 "enc:v1:<base64(nonce|ciphertext)>"，旧明文（无前缀）兼容迁移。
// 适用范围：Server.AuthKey、Settings.SMBPassword 等凭据字段的落盘加密；
// 内存态保持明文供出站连接/挂载使用，对外 API 一律脱敏（MaskSecret）。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"fvcc/logger"
	"fvcc/internal/store/model"
)

const (
	SecretKeyFile   = "secret.key"
	SecretPrefix    = "enc:v1:"
	SecretMaskValue = "******"
)

// LoadOrCreateSecretKey 加载主密钥；不存在则生成 32 字节随机密钥并 0600 落盘。
func LoadOrCreateSecretKey(dataDir string) ([]byte, error) {
	kp := filepath.Join(dataDir, SecretKeyFile)
	if b, err := os.ReadFile(kp); err == nil && len(b) == 32 {
		return b, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(kp, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// EncryptSecret 加密明文，返回 enc:v1:<base64>；空串返回空串。
func EncryptSecret(key []byte, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return SecretPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptSecret 解密；非 enc: 前缀按旧明文原样返回（兼容迁移）。
// 密文但解密失败（密钥丢失/损坏）时返回错误，由调用方决定处置。
func DecryptSecret(key []byte, stored string) (string, error) {
	if stored == "" || !strings.HasPrefix(stored, SecretPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, SecretPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// IsEncrypted 判断存储值是否为密文格式。
func IsEncrypted(stored string) bool { return strings.HasPrefix(stored, SecretPrefix) }

// MaskSecret 脱敏：非空统一显示 ******，空保持空。
func MaskSecret(plain string) string {
	if plain == "" {
		return ""
	}
	return SecretMaskValue
}

// ===== Store 层辅助（settings / server 集合加密）=====

// EncryptSettingsForDisk 写盘前加密凭据字段（仅未加密值加密，幂等）。
func EncryptSettingsForDisk(key []byte, v model.Settings) model.Settings {
	if v.SMBPassword != "" && !IsEncrypted(v.SMBPassword) {
		if enc, err := EncryptSecret(key, v.SMBPassword); err == nil {
			v.SMBPassword = enc
		} else {
			logger.Error("crypto", "加密 SMBPassword 失败: %v", err)
		}
	}
	return v
}

// DecryptSettingsOnLoad 读盘后解密凭据字段；旧明文原样保留（兼容迁移）。
func DecryptSettingsOnLoad(key []byte, v model.Settings) model.Settings {
	if v.SMBPassword != "" && IsEncrypted(v.SMBPassword) {
		if dec, err := DecryptSecret(key, v.SMBPassword); err == nil {
			v.SMBPassword = dec
		} else {
			logger.Error("crypto", "解密 SMBPassword 失败（密钥丢失或损坏），已置空待重新配置: %v", err)
			v.SMBPassword = ""
		}
	}
	return v
}

// EncryptServersForDisk 写盘前加密所有 Server.AuthKey（幂等）。
func EncryptServersForDisk(key []byte, list []model.Server) []model.Server {
	out := make([]model.Server, len(list))
	for i, sv := range list {
		out[i] = sv
		if sv.AuthKey != "" && !IsEncrypted(sv.AuthKey) {
			if enc, err := EncryptSecret(key, sv.AuthKey); err == nil {
				out[i].AuthKey = enc
			} else {
				logger.Error("crypto", "加密 Server(%s) AuthKey 失败: %v", sv.ID, err)
			}
		}
	}
	return out
}

// DecryptServersOnLoad 读盘后解密所有 Server.AuthKey；旧明文原样保留。
func DecryptServersOnLoad(key []byte, list []model.Server) []model.Server {
	for i := range list {
		if list[i].AuthKey != "" && IsEncrypted(list[i].AuthKey) {
			if dec, err := DecryptSecret(key, list[i].AuthKey); err == nil {
				list[i].AuthKey = dec
			} else {
				logger.Error("crypto", "解密 Server(%s) AuthKey 失败（密钥丢失或损坏），已置空待重新配置: %v", list[i].ID, err)
				list[i].AuthKey = ""
			}
		}
	}
	return list
}
