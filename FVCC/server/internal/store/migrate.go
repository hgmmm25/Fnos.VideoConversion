// migrate.go — P3-5 阶段 B：版本化数据迁移框架。
//
// 背景（依据 FVCC/docs/STORE_PERSISTENCE_ROADMAP.md §3）：此前加载路径里散落两处
// 「隐式兼容补丁」——
//   - tasks 列补齐：loadTasksLocked / loadTasksFromSQLiteLocked 内联 migrateTaskColumns；
//   - 项目 schema_ver 补齐：postLoadLocked 内 SchemaVer 0→1 分支。
// 它们没有版本锚点语义：每次启动都全表幂等补齐（靠"只补零值"自身幂等避免重复改动），
// 但无法记录"迁移是否已执行"、无法迁移前留备份、也无法承载有语义的新增列操作。
// 本文件收口为单一线性数据迁移链：
//
//	data_version 锚点（SQLite：import_meta key=data_version / JSON：data_version.json）
//	  → 锚点缺失且有数据：按起始版本走完整链 → 迁移前 premigration 备份
//	  → 每步：持锁改内存 → 解锁后落库受影响集合 → 递增抬锚点 → 审计 schema.migrate
//
// 与 SQLite DDL 版本正交：
//   - sqlite.go 的 schema_version 管「存储表结构」（建表/加列 DDL）；
//   - 本文件的 data_version 管「实体数据语义」（JSON 行内容补列、字段归一）。
//
// 锚点 bootstrap 语义（区分全新安装与旧数据）：
//   - 锚点缺失 + 业务集合全空 → 全新安装：直接抬锚到链尾，不迁移、不备份；
//   - 锚点缺失 + 有数据 → 旧版数据（迁移框架之前写入）：视为 dataVersionInitial 走全链。
//     各步 Apply 幂等（只补零值），对已补齐数据重复执行无副作用；
//   - 锚点高于链尾 → 拒绝启动（与 tasks.json "版本过高"纪律一致，不静默降级，
//     修复手段为升级程序或从 premigration 备份恢复）。

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"fvcc/internal/store/model"
	"fvcc/logger"
)

// dataVersionInitial 无锚点旧数据的视为版本（即"未执行过任何链步"）。
const dataVersionInitial = 1

// dataVersionMetaKey SQLite import_meta 表中数据锚点的 key（与 json_imported 共用键值表）。
const dataVersionMetaKey = "data_version"

// dataVersionFile JSON 回退模式的锚点文件（内容为十进制版本号）。
const dataVersionFile = "data_version.json"

// Migration 单步数据迁移：Apply 在持有 s.mu 写锁时被调用（改内存，返回内容是否变化），
// Persist 在解锁后落库受影响集合（沿用 persist* 无锁约定：内部 SQLite 短连接 / JSON 回退）。
// Apply 必须幂等（只补零值、不覆盖既有数据），崩溃后重放安全。
type Migration struct {
	From    int
	To      int
	Name    string
	Apply   func(s *Store) bool
	Persist func(s *Store)
}

// dataMigrations 数据迁移链（From 升序且连续）。历史两处内联兼容收编为前两步；
// 未来新增列/字段语义时在此追加 {From: n, To: n+1} 即可（旧内联分支禁止再新增）。
var dataMigrations = []Migration{
	// 1→2：任务新列默认值补齐（原 loadTasksLocked / loadTasksFromSQLiteLocked 内联迁移）。
	{
		From: 1, To: 2, Name: "task-columns-default",
		Apply: func(s *Store) bool {
			changed := false
			for i := range s.tasks {
				if migrateTaskColumns(&s.tasks[i]) {
					changed = true
				}
			}
			return changed
		},
		Persist: func(s *Store) { s.persistTasks() },
	},
	// 2→3：项目 schema_ver 缺省补齐（原 postLoadLocked 内联兼容分支，03 §7）。
	{
		From: 2, To: 3, Name: "project-schema-ver",
		Apply: func(s *Store) bool {
			changed := false
			for i := range s.projects {
				if s.projects[i].SchemaVer == 0 {
					s.projects[i].SchemaVer = 1
					changed = true
				}
			}
			return changed
		},
		Persist: func(s *Store) { s.persistProjects() },
	},
}

// targetDataVersion 链目标版本 = 最后一步的 To（空链退化为起始版本）。
func targetDataVersion() int {
	if len(dataMigrations) == 0 {
		return dataVersionInitial
	}
	return dataMigrations[len(dataMigrations)-1].To
}

// applyDataMigrations 在 Load 末尾执行数据迁移链。
// 必须在 s.mu 释放后调用（Backup / AppendAudit 内部自行取锁）。
// 返回错误 → Load 上抛 → main 拒绝启动（纪律：失败报错不静默降级；
// 恢复手段为本次 premigration 备份或既有 JSON 回滚）。
func (s *Store) applyDataMigrations() error {
	target := targetDataVersion()
	cur, anchored := s.readDataVersion()
	if !anchored {
		if s.emptyForBootstrap() {
			s.stampDataVersion(target)
			logger.Info("store", "data_version 锚点初始化：%d（全新安装，无需迁移）", target)
			return nil
		}
		cur = dataVersionInitial
		logger.Info("store", "旧数据无 data_version 锚点，按版本 %d 起执行迁移链", cur)
	}
	if cur > target {
		err := fmt.Errorf("data_version=%d 高于本程序支持的 %d，请升级 FVCC 后再启动", cur, target)
		logger.Error("store", "%v", err)
		s.AppendAudit(model.AuditEntry{
			Actor: "system", Action: "schema.migrate", Target: "data_version",
			Detail: fmt.Sprintf("current=%d target=%d", cur, target), Result: "error",
		})
		return err
	}
	if cur == target {
		return nil // 已最新：零 IO、零备份（重启幂等）
	}

	// 迁移前备份：整链的回滚材料。失败仅告警不阻断（与写前备份同语义——备份是尽力保障，
	// 链本身幂等，最坏情况可从 JSON 回滚脚本恢复）。
	if _, err := s.Backup(BackupKindPreMigration); err != nil {
		logger.Warn("store", "迁移前备份失败（继续执行迁移，各步幂等）: %v", err)
	}

	ver := cur
	steps := make([]string, 0, len(dataMigrations))
	for _, m := range dataMigrations {
		if m.To <= ver {
			continue // 已应用
		}
		if m.From != ver {
			err := fmt.Errorf("迁移链断裂：current=%d next.From=%d", ver, m.From)
			logger.Error("store", "%v", err)
			s.AppendAudit(model.AuditEntry{
				Actor: "system", Action: "schema.migrate", Target: "data_version",
				Detail: fmt.Sprintf("from=%d 断链：%s steps=%s", cur, err, strings.Join(steps, ",")), Result: "error",
			})
			return err
		}
		s.mu.Lock()
		changed := m.Apply(s)
		s.mu.Unlock()
		if changed {
			m.Persist(s)
		}
		s.stampDataVersion(m.To) // 递增抬锚：崩溃重放只重复幂等的当前步，不会跳步
		ver = m.To
		steps = append(steps, m.Name)
	}
	if ver != target {
		err := fmt.Errorf("迁移链未覆盖到目标版本：reached=%d target=%d", ver, target)
		logger.Error("store", "%v", err)
		s.AppendAudit(model.AuditEntry{
			Actor: "system", Action: "schema.migrate", Target: "data_version",
			Detail: fmt.Sprintf("from=%d %s steps=%s", cur, err, strings.Join(steps, ",")), Result: "error",
		})
		return err
	}

	logger.Info("store", "数据迁移链完成：%d→%d steps=[%s]", cur, target, strings.Join(steps, ", "))
	s.AppendAudit(model.AuditEntry{
		Actor: "system", Action: "schema.migrate", Target: "data_version",
		Detail: fmt.Sprintf("from=%d to=%d steps=%s", cur, target, strings.Join(steps, ",")),
	})
	return nil
}

// emptyForBootstrap 判定"全新安装"：业务集合全空。
// locks/healthSamples/auditLog 不计——锁为易失状态、采样与审计为旁路数据，
// 单独存在不构成"旧数据需要迁移"的信号。
func (s *Store) emptyForBootstrap() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks) == 0 && len(s.history) == 0 && len(s.servers) == 0 &&
		len(s.profiles) == 0 && len(s.projects) == 0 && len(s.videoCache) == 0 &&
		len(s.nodeCaps) == 0 && len(s.assetProxies) == 0
}

// ===== data_version 锚点读写 =====

// readDataVersion 读取数据锚点。SQLite 主模式优先 meta；缺行/失败回看 JSON 锚文件
// （JSON 可能滞后于 SQLite，但链幂等，最坏一次 no-op 重放，无数据风险）。
func (s *Store) readDataVersion() (int, bool) {
	if s.sqliteActive.Load() {
		v, ok, err := readSQLiteDataVersion(s.dataDir)
		if err != nil {
			logger.Warn("store", "SQLite 读取 data_version 失败，回看 JSON 锚点: %v", err)
		} else if ok {
			return v, true
		}
	}
	return readJSONDataVersion(s.path(dataVersionFile))
}

// stampDataVersion 抬升锚点：JSON 锚文件恒写（JSON 模式为权威、SQLite 模式为镜像，
// 供 SQLite 不可用降级时参考），主模式下另写 SQLite meta。
func (s *Store) stampDataVersion(v int) {
	saveJSON(s.path(dataVersionFile), v)
	if s.sqliteActive.Load() {
		if err := writeSQLiteDataVersion(s.dataDir, v); err != nil {
			logger.Warn("store", "SQLite 写入 data_version 失败（JSON 锚点已抬升，下次启动至多重放一次幂等链）: %v", err)
		}
	}
}

// readJSONDataVersion 读 JSON 锚文件；不存在/不可解析 → (0, false) 视为无锚点。
func readJSONDataVersion(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		logger.Warn("store", "解析 %s 失败，视为无锚点: %v", path, err)
		return 0, false
	}
	return v, true
}

// readSQLiteDataVersion 读 import_meta 中的 data_version；无行 → (0, false, nil)。
// 短连接模式与 persist* 一致；表由 importLegacyJSONLocked 建出，此处兜底 IF NOT EXISTS。
func readSQLiteDataVersion(dataDir string) (int, bool, error) {
	sq, err := openSQLite(dataDir)
	if err != nil {
		return 0, false, err
	}
	defer sq.Close()
	if _, err := sq.db.Exec(fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		importMetaTable)); err != nil {
		return 0, false, err
	}
	var raw string
	err = sq.db.QueryRow(`SELECT value FROM `+importMetaTable+` WHERE key=?`, dataVersionMetaKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false, fmt.Errorf("data_version %q 解析失败: %w", raw, err)
	}
	return v, true, nil
}

// writeSQLiteDataVersion 写 import_meta 锚点行（INSERT OR REPLACE，幂等）。
func writeSQLiteDataVersion(dataDir string, v int) error {
	sq, err := openSQLite(dataDir)
	if err != nil {
		return err
	}
	defer sq.Close()
	if _, err := sq.db.Exec(fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		importMetaTable)); err != nil {
		return err
	}
	_, err = sq.db.Exec(`INSERT OR REPLACE INTO `+importMetaTable+` (key, value) VALUES (?, ?)`,
		dataVersionMetaKey, strconv.Itoa(v))
	return err
}
