// restore.go — P3-5 阶段 C：快照回滚（整包恢复）。
// 方案依据：FVCC/docs/STORE_PERSISTENCE_ROADMAP.md §4（评审关闭：仅整包恢复、禁止部分回滚、
// 恢复后运行中任务重置 QUEUE（同崩溃恢复语义）、secret.key 不入备份沿用现密钥）。
//
// 与 §4 原稿的差异（SQLite 主载体落地后的适配）：
//   - §4 成文于 JSON 时代，"恢复全部 json 后统一 Rename 换入 + 重新 Load"在 SQLite 主模式下
//     会撞上「SQLite 数据比 JSON 新」的两源问题。故收口为：zip 全量解析校验（换入前，
//     任一步失败内存零改动）→ 持写锁一次性整包替换内存态 → postLoadLocked 收尾
//     （崩溃恢复/派生字段/索引重建，与 Load 同语义）→ persist* 全量落主载体。
//     "任一步失败即回退"由「先验签、后换入」保证：验签/解析失败发生在任何持久化写入之前。
//   - data_version 锚点不随备份走（阶段 A 备份的内存态必经迁移链，内容天然 ≥ 当前链尾）；
//     换入后再跑一次 applyDataMigrations 作幂等防线（锚点未动时立即返回，零 IO）。
//
// 安全边界（zip-slip / zip 炸弹防御，阶段 A 遗留收口）：
//   - zip 条目名必须精确命中白名单（12 个实体 JSON + manifest.json），含路径分隔符或
//     清单外条目一律拒绝——条目只在内存解析，从不落盘，白名单外无写入面；
//   - 单条目解压尺寸上限 restoreEntryMaxBytes；
//   - manifest 逐文件 SHA-256 验签 + 尺寸比对，zip 内不得有 manifest 未声明的条目。

package store

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fvcc/internal/pathutil"
	"fvcc/internal/security"
	"fvcc/internal/store/model"
	"fvcc/logger"
)

// restoreEntryMaxBytes 单个 zip 条目的解压尺寸上限（备份内容为配置/索引级数据，
// 正常远小于此；超限视为异常包拒绝，防 zip 炸弹与内存挤压）。
const restoreEntryMaxBytes = 64 << 20

// backupJSONFiles 备份包内实体 JSON 全集（与 snapshotForBackup 的写入清单同源，
// 亦即恢复入口白名单——新增集合时两处必须同时更新）。
var backupJSONFiles = []string{
	"tasks.json", "history_tasks.json", "server.json", "transcode_profile.json",
	"locks.json", "settings.json", "video_cache.json", "projects.json",
	"node_caps.json", "node_health_samples.json", "audit_log.json", "asset_proxies.json",
}

// RestoreResult 单次回滚结果摘要（API 返回值）。
type RestoreResult struct {
	BackupID     string         `json:"backupId"`
	Kind         string         `json:"kind"`
	SnapshotAt   time.Time      `json:"snapshotAt"`   // 备份生成时刻（UTC，来自文件名/manifest）
	RestoredAt   time.Time      `json:"restoredAt"`   // 回滚完成时刻
	PreRestoreID string         `json:"preRestoreId"` // 回滚前自动备份的 ID（回滚的回滚点）
	Counts       map[string]int `json:"counts"`       // 恢复后的集合计数
}

// restoreSnapshot 备份包解析出的完整内存态（servers/settings 已解密为明文视图，
// 与 Load JSON 回退路径的落内存形态一致）。
type restoreSnapshot struct {
	tasks      []model.Task
	history    []model.Task
	servers    []model.Server
	profiles   []model.Profile
	locks      []model.Lock
	settings   model.Settings
	videoCache []model.VideoInfoCache
	projects   []model.Project
	nodeCaps   []model.NodeCaps
	samples    []model.NodeHealthSample
	audit      []model.AuditEntry
	proxies    []model.AssetProxy
}

// Restore 从指定备份整包恢复全部集合（禁止部分回滚——跨文件一致性）。
// 流程：定位备份 → 验签解析（失败即返回，内存零改动）→ Flush 当前脏数据 →
// prerestore 自动备份（失败中止回滚）→ 持锁整包换入 + postLoadLocked → 全量落库 → 审计。
// 备份生成程序版本（manifest.SchemaVersion）高于本程序 → 拒绝恢复。
func (s *Store) Restore(backupID string) (RestoreResult, error) {
	res, err := s.restore(backupID)
	if err != nil {
		// §4：回滚失败记 security.alert（成功审计在 restore 内追加）。
		logger.Error("store", "snapshot.restore 失败 backup=%s: %v", backupID, err)
		s.AppendAudit(model.AuditEntry{
			Actor: "system", Action: security.AuditActionSecurityAlert, Target: backupID,
			Detail: "snapshot.restore failed: " + err.Error(), Result: "error",
		})
		return RestoreResult{}, err
	}
	return res, nil
}

func (s *Store) restore(backupID string) (RestoreResult, error) {
	backupID = strings.TrimSpace(backupID)
	if backupID == "" {
		return RestoreResult{}, errors.New("restore: 缺少备份 ID")
	}
	s.mu.RLock()
	loaded := s.loaded
	s.mu.RUnlock()
	if !loaded {
		return RestoreResult{}, errors.New("restore: store 尚未完成 Load")
	}

	// 定位备份文件：ID → "backup-<id>.zip"，双重护栏——名内不得含路径分隔（Base 回推）
	// + 解析结果必须仍在 backup 目录内（pathutil.WithinRoot）+ 文件名可解析出时间/种类。
	dir := filepath.Join(s.dataDir, backupDirName)
	name := "backup-" + backupID + ".zip"
	abs := filepath.Clean(filepath.Join(dir, name))
	if filepath.Base(abs) != name {
		return RestoreResult{}, fmt.Errorf("restore: 非法备份 ID %q", backupID)
	}
	if !pathutil.WithinRoot(dir, abs) {
		return RestoreResult{}, fmt.Errorf("restore: 备份路径越界: %s", abs)
	}
	if _, _, ok := parseBackupName(name); !ok {
		return RestoreResult{}, fmt.Errorf("restore: 备份名不符合命名规范: %s", name)
	}
	if _, err := os.Stat(abs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RestoreResult{}, fmt.Errorf("restore: 备份不存在: %s", name)
		}
		return RestoreResult{}, fmt.Errorf("restore: stat %s: %w", name, err)
	}

	files, manifest, err := readBackupZip(abs)
	if err != nil {
		return RestoreResult{}, err
	}
	if manifest.SchemaVersion > schemaVersion {
		return RestoreResult{}, fmt.Errorf("restore: 备份结构版本 %d 高于本程序支持的 %d，请升级 FVCC 后再恢复",
			manifest.SchemaVersion, schemaVersion)
	}
	snap, err := parseRestoreSnapshot(files, s.secretKey)
	if err != nil {
		return RestoreResult{}, err
	}

	// 前置 Flush（§4）：把当前内存脏数据先落盘，再做破坏性替换。
	s.Flush()

	// 回滚前自动备份：本次回滚的回滚点。失败即中止——破坏性操作必须有恢复材料，
	// 与迁移链的"备份失败仅告警"不同级容错。
	pre, err := s.Backup(BackupKindPreRestore)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore: 回滚前备份失败，中止回滚: %w", err)
	}

	// 整包换入：持写锁一次性替换全部集合，收尾复用 postLoadLocked
	//（崩溃恢复语义：运行中任务重置 QUEUE；派生字段重算；内存索引重建）。
	now := time.Now()
	s.mu.Lock()
	s.tasks = snap.tasks
	s.history = snap.history
	s.servers = snap.servers
	s.profiles = snap.profiles
	s.locks = snap.locks
	s.settings = snap.settings
	s.videoCache = snap.videoCache
	s.projects = snap.projects
	s.nodeCaps = snap.nodeCaps
	s.healthSamples = snap.samples
	s.auditLog = snap.audit
	s.assetProxies = snap.proxies
	s.postLoadLocked()
	s.mu.Unlock()

	// 全量落主载体（persist* 无锁约定：SQLite 短连接 / JSON 回退，内部自兜错误日志）。
	// tasks/history/locks 走脏标记 + Flush 合并单事务（与 P1-1/P0-2 提交 3 落盘路径一致）。
	s.markTasksDirty()
	s.markHistoryDirty()
	s.markLocksDirty()
	s.Flush()
	s.persistServers()
	s.persistProfiles()
	s.persistSettings()
	s.persistVideoCache()
	s.persistProjects()
	s.persistNodeCaps()
	s.persistHealthSamples()
	s.persistAuditLog()
	s.persistAssetProxies()

	// 幂等防线：锚点不随备份走，恢复后数据本应 ≥ 链尾；若备份出自迁移框架之前的
	// 程序（无 premigration 链），此处补齐缺失列（各步幂等）。
	if err := s.applyDataMigrations(); err != nil {
		return RestoreResult{}, fmt.Errorf("restore: 恢复后迁移链校验失败: %w", err)
	}

	counts := map[string]int{
		"tasks": len(snap.tasks), "history": len(snap.history), "servers": len(snap.servers),
		"profiles": len(snap.profiles), "locks": len(snap.locks),
		"videoCache": len(snap.videoCache), "projects": len(snap.projects),
		"nodeCaps": len(snap.nodeCaps), "healthSamples": len(snap.samples),
		"audit": len(snap.audit), "assetProxies": len(snap.proxies),
	}
	ts, _, _ := parseBackupName(name)
	res := RestoreResult{
		BackupID: backupID, Kind: manifest.Kind, SnapshotAt: ts.UTC(),
		RestoredAt: now, PreRestoreID: pre.ID, Counts: counts,
	}

	logger.Info("store", "snapshot.restore ok: backup=%s prerestore=%s tasks=%d servers=%d projects=%d",
		backupID, pre.ID, len(snap.tasks), len(snap.servers), len(snap.projects))
	s.AppendAudit(model.AuditEntry{
		Actor: "system", Action: security.AuditActionSnapshotRestore, Target: backupID,
		Detail: fmt.Sprintf("prerestore=%s tasks=%d history=%d servers=%d projects=%d audit=%d",
			pre.ID, len(snap.tasks), len(snap.history), len(snap.servers), len(snap.projects), len(snap.audit)),
		Result: "ok",
	})
	return res, nil
}

// readBackupZip 读取并验签备份包，返回白名单内文件内容 + manifest。
// 任何不一致（白名单外条目 / 尺寸超限 / manifest 缺失或验签不过 / 缺文件 / 多余文件）都报错，
// 调用方在内存改动之前拿到错误 → 天然"失败无副作用"。
func readBackupZip(path string) (map[string][]byte, BackupManifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, BackupManifest{}, fmt.Errorf("restore: 打开备份包失败: %w", err)
	}
	defer zr.Close()

	allowed := make(map[string]bool, len(backupJSONFiles)+1)
	for _, n := range backupJSONFiles {
		allowed[n] = true
	}
	allowed[backupManifest] = true

	files := make(map[string][]byte, len(backupJSONFiles))
	var manifestRaw []byte
	for _, f := range zr.File {
		// zip-slip 防线：条目名精确命中白名单（不含任何路径成分）。
		if !allowed[f.Name] {
			return nil, BackupManifest{}, fmt.Errorf("restore: 备份包含白名单外条目: %q", f.Name)
		}
		if f.UncompressedSize64 > restoreEntryMaxBytes {
			return nil, BackupManifest{}, fmt.Errorf("restore: 条目 %s 声明尺寸 %d 超上限 %d",
				f.Name, f.UncompressedSize64, restoreEntryMaxBytes)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, BackupManifest{}, fmt.Errorf("restore: 打开条目 %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, restoreEntryMaxBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, BackupManifest{}, fmt.Errorf("restore: 读取条目 %s: %w", f.Name, err)
		}
		if int64(len(data)) > restoreEntryMaxBytes {
			return nil, BackupManifest{}, fmt.Errorf("restore: 条目 %s 解压后超尺寸上限", f.Name)
		}
		if f.Name == backupManifest {
			manifestRaw = data
			continue
		}
		files[f.Name] = data
	}

	if manifestRaw == nil {
		return nil, BackupManifest{}, errors.New("restore: 备份缺少 manifest.json")
	}
	var manifest BackupManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, BackupManifest{}, fmt.Errorf("restore: manifest 解析失败: %w", err)
	}
	if len(manifest.Files) == 0 {
		return nil, BackupManifest{}, errors.New("restore: manifest 文件清单为空")
	}

	// 验签：manifest 声明的每个文件必须存在、尺寸与 SHA-256 一致。
	declared := make(map[string]bool, len(manifest.Files))
	for _, mf := range manifest.Files {
		declared[mf.Name] = true
		content, ok := files[mf.Name]
		if !ok {
			return nil, BackupManifest{}, fmt.Errorf("restore: 缺少备份文件 %s", mf.Name)
		}
		if mf.Size != int64(len(content)) {
			return nil, BackupManifest{}, fmt.Errorf("restore: %s 尺寸不符（manifest=%d 实际=%d）", mf.Name, mf.Size, len(content))
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != strings.ToLower(mf.SHA256) {
			return nil, BackupManifest{}, fmt.Errorf("restore: %s SHA-256 验签失败（备份包已损坏或被篡改）", mf.Name)
		}
	}
	// 完整性：zip 内不得有 manifest 未声明的条目。
	for name := range files {
		if !declared[name] {
			return nil, BackupManifest{}, fmt.Errorf("restore: 条目 %s 未在 manifest 中声明", name)
		}
	}
	// 必需文件全集（整包恢复语义：缺一文件即拒绝，不做部分回滚）。
	for _, n := range backupJSONFiles {
		if _, ok := files[n]; !ok {
			return nil, BackupManifest{}, fmt.Errorf("restore: 备份缺少必需文件 %s", n)
		}
	}
	return files, manifest, nil
}

// parseRestoreSnapshot 将备份 JSON 解析为内存态快照。
// servers/settings 为磁盘密文视图（enc:v1:），复用 Load 的解密入口（沿用现 secret.key）。
func parseRestoreSnapshot(files map[string][]byte, key []byte) (restoreSnapshot, error) {
	var snap restoreSnapshot
	unmarshal := func(name string, v any) error {
		if err := json.Unmarshal(files[name], v); err != nil {
			return fmt.Errorf("restore: 解析 %s 失败: %w", name, err)
		}
		return nil
	}

	var tf model.TasksFile
	if err := unmarshal("tasks.json", &tf); err != nil {
		return snap, err
	}
	snap.tasks = nonNil(tf.Tasks)
	var hf model.HistoryFile
	if err := unmarshal("history_tasks.json", &hf); err != nil {
		return snap, err
	}
	snap.history = nonNil(hf.Tasks)
	var sf model.ServersFile
	if err := unmarshal("server.json", &sf); err != nil {
		return snap, err
	}
	snap.servers = security.DecryptServersOnLoad(key, nonNil(sf.Servers))
	var pf model.ProfilesFile
	if err := unmarshal("transcode_profile.json", &pf); err != nil {
		return snap, err
	}
	snap.profiles = nonNil(pf.Profiles)
	var lf model.LocksFile
	if err := unmarshal("locks.json", &lf); err != nil {
		return snap, err
	}
	snap.locks = nonNil(lf.Locks)
	var stf model.SettingsFile
	stf.Settings = model.DefaultSettings()
	if err := unmarshal("settings.json", &stf); err != nil {
		return snap, err
	}
	snap.settings = security.DecryptSettingsOnLoad(key, stf.Settings)
	var vf model.VideoCacheFile
	if err := unmarshal("video_cache.json", &vf); err != nil {
		return snap, err
	}
	snap.videoCache = nonNil(vf.Entries)
	var jf model.ProjectsFile
	if err := unmarshal("projects.json", &jf); err != nil {
		return snap, err
	}
	snap.projects = nonNil(jf.Projects)
	var nf model.NodeCapsFile
	if err := unmarshal("node_caps.json", &nf); err != nil {
		return snap, err
	}
	snap.nodeCaps = nonNil(nf.Items)
	var hf2 model.NodeHealthFile
	if err := unmarshal("node_health_samples.json", &hf2); err != nil {
		return snap, err
	}
	snap.samples = nonNil(hf2.Samples)
	var af model.AuditLogFile
	if err := unmarshal("audit_log.json", &af); err != nil {
		return snap, err
	}
	snap.audit = nonNil(af.Entries)
	var xf model.AssetProxiesFile
	if err := unmarshal("asset_proxies.json", &xf); err != nil {
		return snap, err
	}
	snap.proxies = nonNil(xf.Items)
	return snap, nil
}

// nonNil 空切片归一为非 nil（持久化与 API 序列化语义与 Load 各路径一致）。
func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
