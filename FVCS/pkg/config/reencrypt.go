package config

// 07 §5.5 M3：主密钥迁移（`fvcs-cli reencrypt` 的底层实现）。
//
// 语义：把仍由旧硬编码密钥（legacyEncryptKey）加密的 config.enc.json 一次性重写为
// DPAPI 主密钥密文 —— JSON 明文与字段值完全不变，仅更换加密密钥与 nonce。
// 非 legacy 文件不做任何改动（幂等，可反复执行）；dryRun 只检测不落盘。

import (
	"os"
	"path/filepath"

	"Fnos.VC_Service/pkg/logger"
)

// ConfigPath 返回默认配置文件绝对路径（与 Load/Save 同一定位：exe 同级目录）。
func ConfigPath() string {
	return filepath.Join(getAppDir(), configFileName)
}

// MasterKeyPath 返回 DPAPI 主密钥文件路径（诊断用，不触发密钥生成）。
func MasterKeyPath() string {
	return filepath.Join(getAppDir(), masterKeyFileName)
}

// NeedsReencrypt 检测指定配置文件是否仍为旧硬编码密钥密文。
// 文件不存在或不可解密时返回 error。
func NeedsReencrypt(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	_, usedLegacy, err := decryptForLoad(data)
	if err != nil {
		return false, err
	}
	return usedLegacy, nil
}

// ReencryptFile 执行一次迁移：legacy 密文 → DPAPI 主密钥密文。
//   - rewritten 表示「是否需要（dryRun）/ 已完成（非 dryRun）重写」；
//   - 非 legacy 文件返回 (false, nil)，不做写入；
//   - 解密失败（文件损坏 / 非本机密钥）原样返回错误，不做任何覆盖。
func ReencryptFile(path string, dryRun bool) (rewritten bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	plain, usedLegacy, err := decryptForLoad(data)
	if err != nil {
		return false, err
	}
	if !usedLegacy {
		return false, nil
	}
	if dryRun {
		return true, nil
	}

	sealed, err := encrypt(plain)
	if err != nil {
		return false, err
	}

	// 同目录临时文件 + rename：避免迁移中途中断写出半截密文（与 Save 同策略）。
	tmp := path + ".m3tmp"
	if err := os.WriteFile(tmp, sealed, 0644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}

	logger.Info("config", "M3 reencrypt done: %s (legacy hardcoded key -> DPAPI master key)", path)
	return true, nil
}
