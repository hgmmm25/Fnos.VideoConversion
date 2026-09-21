package api

// trash_handlers.go — P2-5 删除回收站化的 HTTP handler 层。
//
// 纯存储逻辑（授权根判定 / 移动）已迁至 internal/store/trash.go；
// 本文件保留需要 gin.Context / Handlers 上下文的列表 / 恢复 / 清空端点
// （阶段 B 随 api 层整体迁入 internal/api）。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/internal/protocol"
	"fvcc/internal/store"
	"fvcc/internal/security"
)

// trashEntry 回收站条目（供前端列表展示）。
type trashEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`     // 回收站内物理路径（移回时依据）
	OrigPath string `json:"origPath"` // 原始路径（恢复目标）
	Size     int64  `json:"size"`
	ModTime  string `json:"modTime"`
}

// listTrash 列出全部授权根下回收站内容（相对路径结构 + 原始路径）。
func (h *Handlers) listTrash(c *gin.Context) {
	entries := []trashEntry{}
	for _, root := range h.pv.AccessPaths() {
		trashRoot := filepath.Join(filepath.Clean(root), store.TrashDirName)
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
		protocol.Fail(c, 400, "参数错误")
		return
	}
	// 安全校验：仅允许回收站目录内的路径
	allowed := false
	trashPaths := []string{}
	for _, root := range h.pv.AccessPaths() {
		trashPaths = append(trashPaths, filepath.Join(filepath.Clean(root), store.TrashDirName))
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		protocol.Fail(c, 400, "路径无效")
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
		protocol.Fail(c, 403, "仅支持恢复回收站内的文件")
		return
	}
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		protocol.Fail(c, 404, "回收站文件不存在")
		return
	}
	// 恢复目标 = 回收站路径去掉 _trash 前缀
	orig := ""
	for _, tp := range trashPaths {
		tp = filepath.Clean(tp)
		if strings.HasPrefix(abs, tp+string(filepath.Separator)) {
			rel, rerr := filepath.Rel(tp, abs)
			if rerr != nil {
				protocol.Fail(c, 500, rerr.Error())
				return
			}
			root := strings.TrimSuffix(tp, store.TrashDirName)
			root = strings.TrimRight(root, `/\`)
			orig = filepath.Join(root, rel)
			break
		}
	}
	if orig == "" {
		protocol.Fail(c, 500, "无法推导原始路径")
		return
	}
	if _, err := os.Stat(orig); err == nil {
		protocol.Fail(c, 409, fmt.Sprintf("原始位置已存在同名文件，无法恢复: %s", orig))
		return
	}
	if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
		protocol.Fail(c, 500, "恢复失败: "+err.Error())
		return
	}
	if err := os.Rename(abs, orig); err != nil {
		protocol.Fail(c, 500, "恢复失败: "+err.Error())
		return
	}
	c.JSON(200, gin.H{"ok": true, "path": orig})
}

// emptyTrash 清空全部授权根下回收站（破坏性操作，requireAdmin 保护）。
func (h *Handlers) emptyTrash(c *gin.Context) {
	var removed []string
	for _, root := range h.pv.AccessPaths() {
		trashRoot := filepath.Join(filepath.Clean(root), store.TrashDirName)
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
				protocol.Fail(c, 500, "清空回收站失败: "+err.Error())
				return
			}
		}
	}
	security.AuditDestructive(c, security.AuditActionDestructiveEmpty, "_trash", fmt.Sprintf("removed=%d", len(removed)))
	c.JSON(200, gin.H{"ok": true, "removed": len(removed)})
}
