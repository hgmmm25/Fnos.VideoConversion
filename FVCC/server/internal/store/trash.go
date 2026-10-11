package store

// trash.go — P2-5 删除回收站化（混乱报告硬伤②修复）
//
// 视频删除不再物理删除：文件移入所在授权根下的 `_trash` 目录（受 `_` 前缀
// 保留规则保护，不会作为素材库内容展示），并提供恢复/清空入口。NAS 用户
// 误操作可恢复，与 04 §2.4 网关保留名规则保持一致性。
//
// 本文件承载回收站的纯存储逻辑（授权根判定 + 移动）；HTTP handler
// （listTrash / restoreTrash / emptyTrash）随 api 层留在 main 包
// （trash_handlers.go，阶段 B 迁入 internal/api）。

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TrashDirName 回收站目录名（`_` 前缀受 isReservedMediaName 保护）。
const TrashDirName = "_trash"

// withinRoot 判断 abs 是否位于 root 之下（含自身），带分隔符边界。
// 边界特判：Windows 盘符根（D:\）Clean 后仍以分隔符结尾，直接拼接会产生
// 双分隔符（D:\\）导致子路径永不命中，此时根本身即为边界。
func withinRoot(root, abs string) bool {
	r := filepath.Clean(root)
	if abs == r {
		return true
	}
	if strings.HasSuffix(r, string(filepath.Separator)) {
		return strings.HasPrefix(abs, r)
	}
	return strings.HasPrefix(abs, r+string(filepath.Separator))
}

// TrashRootFor 返回 path 所在授权根下的回收站目录；未匹配授权根时返回错误。
func TrashRootFor(roots []string, path string) (string, error) {
	best := ""
	for _, root := range roots {
		root = filepath.Clean(root)
		if !withinRoot(root, path) {
			continue
		}
		if len(root) > len(best) {
			best = root
		}
	}
	if best == "" {
		return "", errors.New("路径不在授权目录内，无法移入回收站")
	}
	return filepath.Join(best, TrashDirName), nil
}

// MoveToTrash 将文件移入回收站，保留相对路径结构避免同名覆盖；
// 回收站内已存在同名文件时追加时间戳后缀。
func MoveToTrash(roots []string, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root := ""
	for _, r := range roots {
		r = filepath.Clean(r)
		if withinRoot(r, abs) && len(r) > len(root) {
			root = r
		}
	}
	if root == "" {
		return "", errors.New("路径不在授权目录内，无法移入回收站")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	trashRoot := filepath.Join(root, TrashDirName)
	dst := filepath.Join(trashRoot, rel)
	if _, err := os.Stat(dst); err == nil {
		ext := filepath.Ext(dst)
		base := strings.TrimSuffix(dst, ext)
		dst = base + "." + strconv.FormatInt(time.Now().UnixMilli(), 10) + ext
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(abs, dst); err != nil {
		return "", err
	}
	return dst, nil
}
