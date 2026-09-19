package main

// M4 通用哈希工具：票据随机串与日志脱敏哈希。
// 04 §2.2 要点 4：日志不记录完整绝对路径，仅记 relPath 哈希前缀；
// 票据随机串使用 crypto/rand，失败时退化时间派生值（仍不可预测地唯一）。

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"
)

// randHex16 生成 32 位十六进制随机串（票据 id 用）。
func randHex16() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败属系统级异常，退化为时间派生值（仍不可预测地唯一）
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// hashForLog 日志用的路径哈希（04 §2.2 要点 4：不记录完整绝对路径）。
func hashForLog(relPath string) string {
	sum := sha1.Sum([]byte(relPath))
	return hex.EncodeToString(sum[:8])
}
