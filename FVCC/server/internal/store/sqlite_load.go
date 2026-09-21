// sqlite_load.go — P0-2 提交 3：Load 从 SQLite 读回全部数据到内存。
//
// 语义对齐 JSON 路径（store.go Load）：
//   - tasks 严格加载：data 损坏 / 读取失败 → 上抛错误拒绝启动（与 loadTasksLocked 一致，
//     避免静默丢弃任务）；旧版结构由 migrateTaskColumns 幂等补列（SQLite 无 Version 字段，
//     schema 结构由 schema_version 管，实体列迁移收敛为幂等补齐）。
//   - 其余集合宽松加载：读取/解析失败仅告警并按空集合处理（与 loadJSON 默认值语义一致）。
//   - servers/settings 落盘为密文（security.Encrypt*ForDisk 结果），读回后走
//     DecryptServersOnLoad / DecryptSettingsOnLoad 解密，与 JSON 路径同一解密入口。
//   - 顺序保持：keyed 表按 rowid（= 写入顺序），seq 表按 seq（= 追加顺序），
//     与 saveJSON 数组顺序语义一致。

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"fvcc/internal/security"
	"fvcc/internal/store/model"
	"fvcc/logger"
)

// readKeyedAll 读取 keyed 表全部行（按 rowid 保序，等同导入/写入顺序）。
func readKeyedAll[T any](sq *storeSQLite, table string) ([]T, error) {
	rows, err := sq.db.Query(fmt.Sprintf(`SELECT data FROM %s ORDER BY rowid`, table))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan %s: %w", table, err)
		}
		var v T
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil, fmt.Errorf("unmarshal %s: %w", table, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", table, err)
	}
	return out, nil
}

// readSingle 读取 keyed 表单行（如 settings/default）；不存在返回 ok=false。
func readSingle[T any](sq *storeSQLite, table, id string) (T, bool, error) {
	var zero T
	var data string
	err := sq.db.QueryRow(`SELECT data FROM `+table+` WHERE id=?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, fmt.Errorf("read %s/%s: %w", table, id, err)
	}
	var v T
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return zero, false, fmt.Errorf("unmarshal %s/%s: %w", table, id, err)
	}
	return v, true, nil
}

// readSeqAll 读取 seq 表全部行（按 seq 升序 = 追加顺序）。
func readSeqAll[T any](sq *storeSQLite, table string) ([]T, error) {
	rows, err := sq.db.Query(fmt.Sprintf(`SELECT data FROM %s ORDER BY seq`, table))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan %s: %w", table, err)
		}
		var v T
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil, fmt.Errorf("unmarshal %s: %w", table, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", table, err)
	}
	return out, nil
}

// mustReadKeyed 宽松读取 keyed 表：失败仅告警并按空集合处理（对齐 loadJSON 默认值语义）。
func mustReadKeyed[T any](sq *storeSQLite, table string) []T {
	out, err := readKeyedAll[T](sq, table)
	if err != nil {
		logger.Warn("store", "从 SQLite 读取 %s 失败，按空集合处理: %v", table, err)
		return nil
	}
	return out
}

// mustReadSeq 宽松读取 seq 表。
func mustReadSeq[T any](sq *storeSQLite, table string) []T {
	out, err := readSeqAll[T](sq, table)
	if err != nil {
		logger.Warn("store", "从 SQLite 读取 %s 失败，按空集合处理: %v", table, err)
		return nil
	}
	return out
}

// loadTasksFromSQLiteLocked 严格加载 tasks（对齐 loadTasksLocked：失败拒绝启动）。
// 调用方必须已持有 s.mu。旧版结构（缺 TaskType/SegIndex 等列）由 migrateTaskColumns 幂等补齐。
func (s *Store) loadTasksFromSQLiteLocked(sq *storeSQLite) error {
	list, err := readKeyedAll[model.Task](sq, "tasks")
	if err != nil {
		return fmt.Errorf("从 SQLite 读取任务失败，拒绝启动以免静默丢弃任务: %w", err)
	}
	migrated := false
	for i := range list {
		if migrateTaskColumns(&list[i]) {
			migrated = true
		}
	}
	s.tasks = list
	if migrated {
		logger.Info("store", "SQLite tasks 列迁移完成（幂等补齐新列），共 %d 条任务", len(s.tasks))
	}
	return nil
}

// loadAllFromSQLiteLocked 从 SQLite 读回全部 12 类数据到内存（提交 3）。
// 需在 Load 写锁内调用。仅 tasks 严格（错误上抛）；其余集合读失败按空处理。
func (s *Store) loadAllFromSQLiteLocked(sq *storeSQLite) error {
	if err := s.loadTasksFromSQLiteLocked(sq); err != nil {
		return err
	}
	s.history = mustReadKeyed[model.Task](sq, "history_tasks")
	svs := mustReadKeyed[model.Server](sq, "servers")
	s.servers = security.DecryptServersOnLoad(s.secretKey, svs)
	s.profiles = mustReadKeyed[model.Profile](sq, "profiles")
	s.locks = mustReadKeyed[model.Lock](sq, "locks")
	// settings 单行：缺失（全新安装未导入）时用默认设置兜底，与 JSON 路径一致。
	st, ok, err := readSingle[model.Settings](sq, "settings", "default")
	if err != nil {
		logger.Warn("store", "从 SQLite 读取 settings 失败，使用默认设置: %v", err)
		s.settings = model.DefaultSettings()
	} else if !ok {
		s.settings = model.DefaultSettings()
	} else {
		s.settings = security.DecryptSettingsOnLoad(s.secretKey, st)
	}
	s.videoCache = mustReadKeyed[model.VideoInfoCache](sq, "video_cache")
	s.projects = mustReadKeyed[model.Project](sq, "projects")
	s.nodeCaps = mustReadKeyed[model.NodeCaps](sq, "node_caps")
	s.healthSamples = mustReadSeq[model.NodeHealthSample](sq, "node_health_samples")
	s.auditLog = mustReadSeq[model.AuditEntry](sq, "audit_log")
	s.assetProxies = mustReadKeyed[model.AssetProxy](sq, "asset_proxies")
	return nil
}
