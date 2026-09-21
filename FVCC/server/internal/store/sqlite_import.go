// sqlite_import.go — P0-2 提交 2：旧 JSON 一次性导入 SQLite（幂等 + 备份）。
//
// 设计（依据《P0-2_SQLite迁移成本评估.md》提交 2 拆分）：
//   FVCC 存量数据以 12 类 JSON 文件落盘（tasks.json / server.json / ...），
//   本次在 Load 中打开 SQLite 持久化载体并完成存量数据一次性导入：
//     1. SQLite 无导入标记且 dataDir 存在旧 JSON → 先整目录备份到
//        <dataDir>/backup/pre-sqlite-<ts>/，再全量导入，最后写导入标记；
//     2. 导入与标记写入在同一事务内完成，任一步失败整体回滚（下次启动自动重试）；
//     3. 导入标记存在 → 幂等跳过（重启不重复导入、不重复备份）。
//   servers/settings 落盘为密文（security.Encrypt*ForDisk 结果），导入直接按
//   JSON 原样复制，语义与文件一致；本提交仅建立数据副本与导入管线，Load 内存
//   加载仍以 JSON 为准（运行时行为零变化），提交 3（persist 替换）切换读写源
//   时复用本管线保证数据完整。

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fvcc/internal/store/model"
	"fvcc/logger"
)

// jsonFiles 存量 JSON 文件名清单（与 persist*/Load 一一对应）。
var jsonFiles = []string{
	"tasks.json", "history_tasks.json", "server.json", "transcode_profile.json",
	"locks.json", "settings.json", "video_cache.json", "projects.json",
	"node_caps.json", "node_health_samples.json", "audit_log.json", "asset_proxies.json",
}

// importMeta 表名与导入标记 key：记录「存量 JSON 是否已导入 SQLite」。
// 独立于 schema_version（后者管 schema 结构迁移，前者管数据迁移）。
const (
	importMetaTable = "import_meta"
	importMetaKey   = "json_imported"
)

// ImportStats 单次导入统计。
type ImportStats struct {
	Imported  bool           // 本次是否执行了导入（false = 已导入过 / 无数据源）
	BackupDir string         // 备份目录（仅导入过且存在旧 JSON 时非空）
	JSONFiles int            // 发现的存量 JSON 文件数
	Rows      map[string]int // 表名 -> 导入行数
}

// String 日志摘要。
func (st ImportStats) String() string {
	if !st.Imported {
		return "legacy JSON import skipped (already imported or no data source)"
	}
	return fmt.Sprintf("files=%d backup=%s rows=%v", st.JSONFiles, st.BackupDir, st.Rows)
}

// importLegacyJSONLocked 将 dataDir 下存量 JSON 一次性导入 SQLite。
// 需在 Load 写锁内调用（与内存加载串行）。幂等：已有导入标记直接跳过。
func importLegacyJSONLocked(sq *storeSQLite, dataDir string) (ImportStats, error) {
	stats := ImportStats{Rows: map[string]int{}}

	if _, err := sq.db.Exec(fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		importMetaTable)); err != nil {
		return stats, fmt.Errorf("create %s: %w", importMetaTable, err)
	}

	// 幂等：已导入过则跳过（重启不重复导入/备份）。
	var mark string
	err := sq.db.QueryRow(`SELECT value FROM `+importMetaTable+` WHERE key=?`, importMetaKey).Scan(&mark)
	if err == nil {
		return stats, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return stats, fmt.Errorf("read %s: %w", importMetaTable, err)
	}

	// 数据源探测：任一 JSON 文件存在才导入。全新安装无 JSON 时跳过且不写标记，
	// 后续用户手动放入旧 JSON（升级/恢复场景）仍可导入。
	now := time.Now().Unix()
	for _, name := range jsonFiles {
		if _, err := os.Stat(filepath.Join(dataDir, name)); err == nil {
			stats.JSONFiles++
		}
	}
	if stats.JSONFiles == 0 {
		return stats, nil
	}

	// 导入前整目录备份（可回滚；与评估 §2 备份目录 <dataDir>/backup/ 一致）。
	backupDir, err := backupJSONDir(dataDir)
	if err != nil {
		return stats, fmt.Errorf("backup legacy JSON: %w", err)
	}
	stats.BackupDir = backupDir

	tx, err := sq.db.Begin()
	if err != nil {
		return stats, fmt.Errorf("begin import tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // 提交成功后为 no-op

	rows, err := importAllLocked(tx, dataDir, now)
	if err != nil {
		return stats, err
	}
	stats.Rows = rows

	// 导入完成写标记（与数据同事务：要么全部成功，要么整体回滚可重试）。
	if _, err := tx.Exec(`INSERT OR REPLACE INTO `+importMetaTable+` (key, value) VALUES (?, ?)`,
		importMetaKey, fmt.Sprintf("imported_at=%d", now)); err != nil {
		return stats, fmt.Errorf("mark %s: %w", importMetaKey, err)
	}
	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("commit import tx: %w", err)
	}
	stats.Imported = true
	logger.Info("store", "legacy JSON imported to sqlite: backup=%s rows=%v", backupDir, rows)
	return stats, nil
}

// backupJSONDir 复制 dataDir 下存在的存量 JSON 到 backup/pre-sqlite-<ts>/。
func backupJSONDir(dataDir string) (string, error) {
	backupRoot := filepath.Join(dataDir, "backup")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format("20060102150405")
	dir := filepath.Join(backupRoot, "pre-sqlite-"+ts)
	if _, err := os.Stat(dir); err == nil { // 同秒重复（快速重启/测试）：追加纳秒后缀
		dir = filepath.Join(backupRoot, fmt.Sprintf("pre-sqlite-%s-%d", ts, time.Now().UnixNano()%1000000))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, name := range jsonFiles {
		src := filepath.Join(dataDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", src, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return "", fmt.Errorf("write backup %s: %w", name, err)
		}
	}
	return dir, nil
}

// importAllLocked 在单事务内导入全部 12 类存量 JSON。
func importAllLocked(tx *sql.Tx, dataDir string, now int64) (map[string]int, error) {
	rows := map[string]int{}

	// ===== keyed 表：id 为主键，data 为整行实体 JSON（与 sqlite.go 表结构一致）=====

	// tasks（解析失败时 Load 的 loadTasksLocked 已拒绝启动，此处不可达）
	tf := loadJSON[model.TasksFile](filepath.Join(dataDir, "tasks.json"), model.TasksFile{Version: 1, Tasks: []model.Task{}})
	for _, t := range tf.Tasks {
		if err := insertKeyedRow(tx, "tasks", t.ID, t, now); err != nil {
			return nil, err
		}
	}
	rows["tasks"] = len(tf.Tasks)

	// history_tasks
	hf := loadJSON[model.HistoryFile](filepath.Join(dataDir, "history_tasks.json"), model.HistoryFile{Version: 1, Tasks: []model.Task{}})
	for _, t := range hf.Tasks {
		if err := insertKeyedRow(tx, "history_tasks", t.ID, t, now); err != nil {
			return nil, err
		}
	}
	rows["history_tasks"] = len(hf.Tasks)

	// servers（AuthKey 等为密文，原样导入，语义与文件一致）
	sf := loadJSON[model.ServersFile](filepath.Join(dataDir, "server.json"), model.ServersFile{Version: 1, Servers: []model.Server{}})
	for _, sv := range sf.Servers {
		if err := insertKeyedRow(tx, "servers", sv.ID, sv, now); err != nil {
			return nil, err
		}
	}
	rows["servers"] = len(sf.Servers)

	// profiles
	pf := loadJSON[model.ProfilesFile](filepath.Join(dataDir, "transcode_profile.json"), model.ProfilesFile{Version: 1, Profiles: []model.Profile{}})
	for _, p := range pf.Profiles {
		if err := insertKeyedRow(tx, "profiles", p.ID, p, now); err != nil {
			return nil, err
		}
	}
	rows["profiles"] = len(pf.Profiles)

	// locks（每 server 一条，主键 ServerID；INSERT OR REPLACE 防重）
	lf := loadJSON[model.LocksFile](filepath.Join(dataDir, "locks.json"), model.LocksFile{Version: 1, Locks: []model.Lock{}})
	for _, l := range lf.Locks {
		if err := insertKeyedRow(tx, "locks", l.ServerID, l, now); err != nil {
			return nil, err
		}
	}
	rows["locks"] = len(lf.Locks)

	// settings：单对象固定 id='default'。文件不存在时不导入（与 Load 语义一致：
	// 不存在时内存用 DefaultSettings 兜底，但不应把兜底值写入持久化）。
	if _, err := os.Stat(filepath.Join(dataDir, "settings.json")); err == nil {
		setf := loadJSON[model.SettingsFile](filepath.Join(dataDir, "settings.json"), model.SettingsFile{Version: 1, Settings: model.DefaultSettings()})
		if err := insertKeyedRow(tx, "settings", "default", setf.Settings, now); err != nil {
			return nil, err
		}
		rows["settings"] = 1
	}

	// video_cache（主键 Path）
	vcf := loadJSON[model.VideoCacheFile](filepath.Join(dataDir, "video_cache.json"), model.VideoCacheFile{Version: 1, Entries: []model.VideoInfoCache{}})
	for _, c := range vcf.Entries {
		if err := insertKeyedRow(tx, "video_cache", c.Path, c, now); err != nil {
			return nil, err
		}
	}
	rows["video_cache"] = len(vcf.Entries)

	// projects
	pjf := loadJSON[model.ProjectsFile](filepath.Join(dataDir, "projects.json"), model.ProjectsFile{Version: 1, Projects: []model.Project{}})
	for _, p := range pjf.Projects {
		if err := insertKeyedRow(tx, "projects", p.ID, p, now); err != nil {
			return nil, err
		}
	}
	rows["projects"] = len(pjf.Projects)

	// node_caps（主键 ServerID）
	ncf := loadJSON[model.NodeCapsFile](filepath.Join(dataDir, "node_caps.json"), model.NodeCapsFile{Version: 1, Items: []model.NodeCaps{}})
	for _, n := range ncf.Items {
		if err := insertKeyedRow(tx, "node_caps", n.ServerID, n, now); err != nil {
			return nil, err
		}
	}
	rows["node_caps"] = len(ncf.Items)

	// asset_proxies（主键 AssetKey）
	apf := loadJSON[model.AssetProxiesFile](filepath.Join(dataDir, "asset_proxies.json"), model.AssetProxiesFile{Version: 1, Items: []model.AssetProxy{}})
	for _, a := range apf.Items {
		if err := insertKeyedRow(tx, "asset_proxies", a.AssetKey, a, now); err != nil {
			return nil, err
		}
	}
	rows["asset_proxies"] = len(apf.Items)

	// ===== seq 表：追加型（插入顺序 = 追加顺序），无主键 =====

	// node_health_samples
	nhf := loadJSON[model.NodeHealthFile](filepath.Join(dataDir, "node_health_samples.json"), model.NodeHealthFile{Version: 1, Samples: []model.NodeHealthSample{}})
	for _, sm := range nhf.Samples {
		if err := insertSeqRow(tx, "node_health_samples", sm, now); err != nil {
			return nil, err
		}
	}
	rows["node_health_samples"] = len(nhf.Samples)

	// audit_log
	alf := loadJSON[model.AuditLogFile](filepath.Join(dataDir, "audit_log.json"), model.AuditLogFile{Version: 1, Entries: []model.AuditEntry{}})
	for _, e := range alf.Entries {
		if err := insertSeqRow(tx, "audit_log", e, now); err != nil {
			return nil, err
		}
	}
	rows["audit_log"] = len(alf.Entries)

	return rows, nil
}

// insertKeyedRow 向 keyed 表插入单行（id + 实体 JSON）。
func insertKeyedRow(tx *sql.Tx, table, id string, v any, now int64) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s/%s: %w", table, id, err)
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT OR REPLACE INTO %s (id, data, updated_at) VALUES (?, ?, ?)`, table),
		id, string(data), now); err != nil {
		return fmt.Errorf("insert %s/%s: %w", table, id, err)
	}
	return nil
}

// insertSeqRow 向 seq 表追加单行（无主键，插入顺序即追加顺序）。
func insertSeqRow(tx *sql.Tx, table string, v any, now int64) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", table, err)
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s (data, updated_at) VALUES (?, ?)`, table),
		string(data), now); err != nil {
		return fmt.Errorf("insert %s: %w", table, err)
	}
	return nil
}
