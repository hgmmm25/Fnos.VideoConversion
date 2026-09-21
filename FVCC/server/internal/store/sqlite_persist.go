// sqlite_persist.go — P0-2 提交 3：persist 层切换 SQLite 载体（短连接 + 失败回退 JSON）。
//
// 设计决策（依据《P0-2_SQLite迁移成本评估.md》提交 3 拆分）：
//   - 连接生命周期延续提交 2 的短连接模式：每个 persist 单独 openSQLite → 写入 → Close。
//     不长期持有句柄，避免 Windows 上测试 TempDir 清理失败 / 优雅退出时文件占用。
//   - 全量替换语义（与 saveJSON 一致）：DELETE FROM <table> 后逐条 INSERT，
//     单事务内完成（replaceKeyedTx / replaceSeqTx），任一步失败整体回滚。
//   - 主模式门闩：Load 成功走 SQLite 读回时置 sqliteActive=true，persist 才写 SQLite；
//     JSON 回退模式（SQLite 不可用/导入失败）下 persist 直接写 JSON，杜绝两源分裂。
//   - 失败回退：SQLite 写入失败 → 落 JSON 文件（原行为）+ 尽力清理导入标记
//     （DELETE import_meta.json_imported），下次启动 Load 以 JSON 为准重新导入，
//     避免「SQLite 有旧数据 + JSON 有新数据」的数据分裂。

package store

import (
	"database/sql"
	"fmt"
	"time"

	"fvcc/internal/store/model"
	"fvcc/logger"
)

// jsonRow keyed 表写入行（id + 整行实体 JSON，与 sqlite.go 表结构一致）。
type jsonRow struct {
	id string
	v  any
}

// replaceKeyedTx 在既有事务内全量替换 keyed 表（DELETE + INSERT，语义同 saveJSON 全量写盘）。
// 供单 persist 与 Flush 合并事务复用。
func (s *storeSQLite) replaceKeyedTx(tx *sql.Tx, table string, rows []jsonRow, now int64) error {
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s`, table)); err != nil {
		return fmt.Errorf("clear %s: %w", table, err)
	}
	for _, r := range rows {
		if err := insertKeyedRow(tx, table, r.id, r.v, now); err != nil {
			return err
		}
	}
	return nil
}

// replaceSeqTx 在既有事务内全量替换 seq 表（DELETE + INSERT，保插入顺序）。
func (s *storeSQLite) replaceSeqTx(tx *sql.Tx, table string, items []any, now int64) error {
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s`, table)); err != nil {
		return fmt.Errorf("clear %s: %w", table, err)
	}
	for _, v := range items {
		if err := insertSeqRow(tx, table, v, now); err != nil {
			return err
		}
	}
	return nil
}

// replaceKeyed 自开事务全量替换 keyed 表。
func (s *storeSQLite) replaceKeyed(table string, rows []jsonRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s tx: %w", table, err)
	}
	defer tx.Rollback() //nolint:errcheck // 提交成功后为 no-op
	if err := s.replaceKeyedTx(tx, table, rows, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// replaceSeq 自开事务全量替换 seq 表。
func (s *storeSQLite) replaceSeq(table string, items []any) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s tx: %w", table, err)
	}
	defer tx.Rollback() //nolint:errcheck
	if err := s.replaceSeqTx(tx, table, items, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// clearImportMark 删除存量 JSON 导入标记（persist 回退 JSON 时调用）：
// 使下次启动 Load 以 JSON 为准重新导入，避免 SQLite 旧数据与 JSON 新数据分裂。
func (s *storeSQLite) clearImportMark() error {
	_, err := s.db.Exec(`DELETE FROM `+importMetaTable+` WHERE key=?`, importMetaKey)
	return err
}

// tasksRows 构造 tasks/history 的 keyed 行（主键均为 model.Task.ID）。
func tasksRows(list []model.Task) []jsonRow {
	rows := make([]jsonRow, 0, len(list))
	for i := range list {
		rows = append(rows, jsonRow{id: list[i].ID, v: list[i]})
	}
	return rows
}

// persistKeyed 把集合全量写入 SQLite keyed 表；非主模式 / SQLite 不可用 / 写失败时回退 JSON。
// fallback 由调用方提供（原 saveJSON 包装），保证持久化行为与提交 2 之前完全一致。
func (s *Store) persistKeyed(table string, rows []jsonRow, fallback func()) {
	if !s.sqliteActive.Load() {
		fallback()
		return
	}
	sq, err := openSQLite(s.dataDir)
	if err != nil {
		logger.Warn("store", "SQLite 不可用，回退 JSON 持久化: %v", err)
		fallback()
		return
	}
	defer sq.Close()
	if err := sq.replaceKeyed(table, rows); err != nil {
		logger.Warn("store", "SQLite 写入 %s 失败，回退 JSON: %v", table, err)
		if merr := sq.clearImportMark(); merr != nil {
			logger.Warn("store", "清理导入标记失败: %v", merr)
		}
		fallback()
	}
}

// persistSeq 同 persistKeyed，用于追加型 seq 表。
func (s *Store) persistSeq(table string, items []any, fallback func()) {
	if !s.sqliteActive.Load() {
		fallback()
		return
	}
	sq, err := openSQLite(s.dataDir)
	if err != nil {
		logger.Warn("store", "SQLite 不可用，回退 JSON 持久化: %v", err)
		fallback()
		return
	}
	defer sq.Close()
	if err := sq.replaceSeq(table, items); err != nil {
		logger.Warn("store", "SQLite 写入 %s 失败，回退 JSON: %v", table, err)
		if merr := sq.clearImportMark(); merr != nil {
			logger.Warn("store", "清理导入标记失败: %v", merr)
		}
		fallback()
	}
}

// fallbackTasksJSON 写 tasks.json（persist 回退与 Flush 回退共用）。
func (s *Store) fallbackTasksJSON() {
	saveJSON(s.path("tasks.json"), model.TasksFile{Version: StoreSchemaVersion, Tasks: s.tasks})
}

// fallbackHistoryJSON 写 history_tasks.json（persist 回退与 Flush 回退共用）。
func (s *Store) fallbackHistoryJSON() {
	saveJSON(s.path("history_tasks.json"), model.HistoryFile{Version: 1, Tasks: s.history})
}

// keyedRows 由任意集合构造 keyed 行（主键由 idFn 提取，替代逐类手写辅助）。
func keyedRows[T any](list []T, idFn func(T) string) []jsonRow {
	rows := make([]jsonRow, 0, len(list))
	for i := range list {
		rows = append(rows, jsonRow{id: idFn(list[i]), v: list[i]})
	}
	return rows
}

// toAnySlice 将强类型切片转为 []any（seq 表写入用，避免每类手写转换）。
func toAnySlice[T any](list []T) []any {
	out := make([]any, 0, len(list))
	for i := range list {
		out = append(out, list[i])
	}
	return out
}

// flushSQLiteTx 以单事务合并写入 tasks/history/locks（提交 3：Flush 从两次全量 JSON 写
// 收敛为一次 SQLite 事务提交；tDirty/hDirty/lDirty 决定写入哪些集合）。
// P0-2 提交 3 补：locks 从"锁操作逐次写盘"改为脏标记 + Flush 合并落盘（见 store.go
// dirtyLocks 注释）——锁为易失状态，崩溃丢失仅导致重新调度，无数据损坏语义。
func flushSQLiteTx(sq *storeSQLite, s *Store, tDirty, hDirty, lDirty bool) error {
	tx, err := sq.db.Begin()
	if err != nil {
		return fmt.Errorf("begin flush tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	now := time.Now().Unix()
	if tDirty {
		if err := sq.replaceKeyedTx(tx, "tasks", keyedRows(s.tasks, func(t model.Task) string { return t.ID }), now); err != nil {
			return err
		}
	}
	if hDirty {
		if err := sq.replaceKeyedTx(tx, "history_tasks", keyedRows(s.history, func(t model.Task) string { return t.ID }), now); err != nil {
			return err
		}
	}
	if lDirty {
		if err := sq.replaceKeyedTx(tx, "locks", keyedRows(s.locks, func(v model.Lock) string { return v.ServerID }), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
