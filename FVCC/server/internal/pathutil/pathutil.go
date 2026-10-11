// Package pathutil 提供路径归属判定的单一真源。叶子包，仅依赖标准库，
// 供 edl / store / smbshare / api 等复用，避免盘符根边界逻辑多处重复实现而漏改。
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// WithinRoot 判定 abs 是否等于 root 或位于 root 之下，带分隔符边界
// （防 /media/videos2 被误判为 /media/videos 的子路径）。
//
// 边界特判：Windows 盘符根（如 D:\）经 filepath.Clean 后仍以分隔符结尾，若再拼接
// 分隔符会得到双分隔符（D:\\），任何子路径（D:\xxx）都无法命中前缀——表现为
// "授权根为盘符根时读取文件全部报无权限"。此时根本身即为边界，子路径只需以该根为前缀。
//
// 入参应为已解析的绝对路径；root 比较前会做 Clean（幂等）。
func WithinRoot(root, abs string) bool {
	r := filepath.Clean(root)
	if abs == r {
		return true
	}
	sep := string(os.PathSeparator)
	if strings.HasSuffix(r, sep) {
		return strings.HasPrefix(abs, r)
	}
	return strings.HasPrefix(abs, r+sep)
}
