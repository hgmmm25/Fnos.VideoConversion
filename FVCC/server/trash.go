package main

// trash.go — P2-5 删除回收站化（混乱报告硬伤②修复）
//
// 视频删除不再物理删除：文件移入所在授权根下的 `_trash` 目录（受 `_` 前缀
// 保留规则保护，不会作为素材库内容展示），并提供恢复/清空入口。NAS 用户
// 误操作可恢复，与 04 §2.4 网关保留名规则保持一致性。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// trashDirName 回收站目录名（`_` 前缀受 isReservedMediaName 保护）。
const trashDirName = "_trash"

// trashEntry 回收站条目（供前端列表展示）。
type trashEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`     // 回收站内物理路径（移回时依据）
	OrigPath string `json:"origPath"` // 原始路径（恢复目标）
	Size     int64  `json:"size"`
	ModTime  string `json:"modTime"`
}

// trashRootFor 返回 path 所在授权根下的回收站目录；未匹配授权根时返回错误。
func (h *Handlers) trashRootFor(path string) (string, error) {
	best := ""
	for _, root := range h.pv.AccessPaths() {
		root = filepath.Clean(root)
		if !strings.HasPrefix(path, root+string(filepath.Separator)) {
			continue
		}
		if len(root) > len(best) {
			best = root
		}
	}
	if best == "" {
		return "", errors.New("路径不在授权目录内，无法移入回收站")
	}
	return filepath.Join(best, trashDirName), nil
}

// moveToTrash 将文件移入回收站，保留相对路径结构避免同名覆盖；
// 回收站内已存在同名文件时追加时间戳后缀。
func (h *Handlers) moveToTrash(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root := ""
	for _, r := range h.pv.AccessPaths() {
		r = filepath.Clean(r)
		if strings.HasPrefix(abs, r+string(filepath.Separator)) && len(r) > len(root) {
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
	trashRoot := filepath.Join(root, trashDirName)
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

// listTrash 列出全部授权根下回收站内容（相对路径结构 + 原始路径）。
func (h *Handlers) listTrash(c *gin.Context) {
	entries := []trashEntry{}
	for _, root := range h.pv.AccessPaths() {
		trashRoot := filepath.Join(filepath.Clean(root), trashDirName)
		info, err := os.Stat(trashRoot)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.Walk(trashRoot, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(trashRoot, p)
			if rerr != nil {
				return nil
			}
			entries = append(entries, trashEntry{
				Name:     fi.Name(),
				Path:     p,
				OrigPath: filepath.Join(filepath.Clean(root), rel),
				Size:     fi.Size(),
				ModTime:  fi.ModTime().Format(time.RFC3339),
			})
			return nil
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	c.JSON(200, gin.H{"ok": true, "items": entries, "count": len(entries)})
}

// restoreTrash 将回收站文件恢复到原始路径；原始路径已存在时拒绝并提示。
func (h *Handlers) restoreTrash(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "参数错误")
		return
	}
	// 安全校验：仅允许回收站目录内的路径
	allowed := false
	trashPaths := []string{}
	for _, root := range h.pv.AccessPaths() {
		trashPaths = append(trashPaths, filepath.Join(filepath.Clean(root), trashDirName))
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		fail(c, 400, "路径无效")
		return
	}
	for _, tp := range trashPaths {
		tp = filepath.Clean(tp)
		if strings.HasPrefix(abs, tp+string(filepath.Separator)) {
			allowed = true
			break
		}
	}
	if !allowed {
		fail(c, 403, "仅支持恢复回收站内的文件")
		return
	}
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		fail(c, 404, "回收站文件不存在")
		return
	}
	// 恢复目标 = 回收站路径去掉 _trash 前缀
	orig := ""
	for _, tp := range trashPaths {
		tp = filepath.Clean(tp)
		if strings.HasPrefix(abs, tp+string(filepath.Separator)) {
			rel, rerr := filepath.Rel(tp, abs)
			if rerr != nil {
				fail(c, 500, rerr.Error())
				return
			}
			root := strings.TrimSuffix(tp, trashDirName)
			root = strings.TrimRight(root, `/\`)
			orig = filepath.Join(root, rel)
			break
		}
	}
	if orig == "" {
		fail(c, 500, "无法推导原始路径")
		return
	}
	if _, err := os.Stat(orig); err == nil {
		fail(c, 409, fmt.Sprintf("原始位置已存在同名文件，无法恢复: %s", orig))
		return
	}
	if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
		fail(c, 500, "恢复失败: "+err.Error())
		return
	}
	if err := os.Rename(abs, orig); err != nil {
		fail(c, 500, "恢复失败: "+err.Error())
		return
	}
	c.JSON(200, gin.H{"ok": true, "path": orig})
}

// emptyTrash 清空全部授权根下回收站（破坏性操作，requireAdmin 保护）。
func (h *Handlers) emptyTrash(c *gin.Context) {
	var removed []string
	for _, root := range h.pv.AccessPaths() {
		trashRoot := filepath.Join(filepath.Clean(root), trashDirName)
		info, err := os.Stat(trashRoot)
		if err != nil || !info.IsDir() {
			continue
		}
		entries, err := os.ReadDir(trashRoot)
		if err != nil {
			continue
		}
		for _, e := range entries {
			full := filepath.Join(trashRoot, e.Name())
			removed = append(removed, full)
			if err := os.RemoveAll(full); err != nil {
				fail(c, 500, "清空回收站失败: "+err.Error())
				return
			}
		}
	}
	c.JSON(200, gin.H{"ok": true, "removed": len(removed)})
}
