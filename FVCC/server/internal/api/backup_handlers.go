// backup_handlers.go — P3-5 阶段 A：store 备份 HTTP 入口（仅后端 API，评审点 4）。
// 方案依据：FVCC/docs/STORE_PERSISTENCE_ROADMAP.md §2.1。
//   POST /api/store/backup  （admin）→ 立即做一次手动备份，返回备份 ID
//   GET  /api/store/backups （admin）→ 列出备份目录内的历史备份（含定时/写前/手动）

package api

import (
	"net/http"

	"fvcc/internal/protocol"
	"fvcc/logger"

	"github.com/gin-gonic/gin"
)

// createStoreBackup 手动触发一次备份（BackupKindManual，永久保留直至用户删除）。
func (h *Handlers) createStoreBackup(c *gin.Context) {
	res, err := h.store.Backup("manual")
	if err != nil {
		logger.Error("api", "store/backup: %v", err)
		protocol.Fail(c, http.StatusInternalServerError, "备份失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "backup": res})
}

// listStoreBackups 返回备份列表（按时间倒序）。
func (h *Handlers) listStoreBackups(c *gin.Context) {
	list, err := h.store.ListBackups()
	if err != nil {
		logger.Error("api", "store/backups: %v", err)
		protocol.Fail(c, http.StatusInternalServerError, "读取备份列表失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "backups": list})
}
