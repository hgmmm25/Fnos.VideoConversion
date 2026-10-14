// backup_handlers.go — P3-5 阶段 A/C：store 备份与快照回滚 HTTP 入口（仅后端 API，评审点 4）。
// 方案依据：FVCC/docs/STORE_PERSISTENCE_ROADMAP.md §2.1 / §4。
//   POST /api/store/backup  （admin）→ 立即做一次手动备份，返回备份 ID
//   GET  /api/store/backups （admin）→ 列出备份目录内的历史备份（含定时/写前/手动）
//   POST /api/store/restore （admin）→ 从指定备份整包恢复（禁止部分回滚；回滚前自动 prerestore 备份）

package api

import (
	"net/http"
	"strings"

	"fvcc/internal/protocol"
	"fvcc/internal/security"
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

// restoreStoreBackup 从指定备份整包恢复（阶段 C，STORE_PERSISTENCE_ROADMAP §4）。
// 破坏性动作双重把关：RequireAdmin（路由中间件）+ 审计（store.Restore 内部
// snapshot.restore / 失败 security.alert 落 audit_log；此处再记一条带操作者身份的
// 破坏性审计，脱敏约定同 AuditDestructive）。
func (h *Handlers) restoreStoreBackup(c *gin.Context) {
	var req struct {
		BackupID string `json:"backupId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.BackupID) == "" {
		protocol.Fail(c, http.StatusBadRequest, "参数错误：需要 {\"backupId\": \"<备份ID>\"}")
		return
	}
	res, err := h.store.Restore(req.BackupID)
	if err != nil {
		logger.Error("api", "store/restore backup=%s: %v", req.BackupID, err)
		security.AuditDestructive(c, security.AuditActionSnapshotRestore, req.BackupID, "error")
		protocol.Fail(c, http.StatusInternalServerError, "恢复失败: "+err.Error())
		return
	}
	security.AuditDestructive(c, security.AuditActionSnapshotRestore, req.BackupID, "ok")
	c.JSON(http.StatusOK, gin.H{"ok": true, "restore": res})
}
