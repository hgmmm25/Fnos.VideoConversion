// backup.go — P3-5 阶段 A：store 备份（定时 / 关键写前 / 手动）。
// 方案依据：FVCC/docs/STORE_PERSISTENCE_ROADMAP.md §2（评审关闭：30min 定时、
// 系统生成池保留 12 份、手动永久、secret.key 不入备份、仅后端 API）。
//
// 快照语义：备份 = 持 RLock 一次性从内存态序列化各集合（与 Flush 同源，内存是
// 唯一真相——SQLite 主模式下 JSON 文件可能滞后，不可作为备份源），zip 写在锁外。
// 凭据形态：servers/settings 走 EncryptServersForDisk/EncryptSettingsForDisk
// 密文视图入包（enc:v1: 前缀），secret.key 本身不备份，回滚时沿用现密钥解密。
// 落盘原子性：zip 写入用同目录 tmp + Rename（与 saveJSON 同语义），杜绝半写包。
// 保留策略：auto + prewrite（系统生成）为同一池，按时间倒序保留最近
// backupKeepTimed 份；manual 不参与轮转，永久保留直至用户删除。

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
	"sort"
	"strings"
	"time"

	"fvcc/internal/security"
	"fvcc/internal/store/model"
	"fvcc/logger"
)

// 备份种类（写入文件名后缀，参与轮转判定）。
const (
	BackupKindAuto     = "auto"     // 定时备份（调度器 30min tick）
	BackupKindManual   = "manual"   // 手动备份（POST /api/store/backup）
	BackupKindPrewrite = "prewrite" // 关键写前备份（节点/凭据/设置/方案变更前）
)

const (
	backupDirName  = "backup" // <dataDir>/backup/
	backupTimeFmt  = "20060102150405"
	backupManifest = "manifest.json"
	// backupKeepTimed 系统生成池（auto+prewrite）保留份数（评审点 1：12 份 ≈ 6h 窗口）。
	backupKeepTimed = 12
)

// prewriteMinInterval 关键写前备份去抖窗口：窗口内的连续关键写共享同一份快照，
// 防止节点同步等场景触发备份风暴。单测可临时缩小。
var prewriteMinInterval = 5 * time.Minute

// backupClock 备份命名时钟（单测注入，保证同秒多次备份可区分与轮转可控）。
var backupClock = time.Now

// BackupResult 单次备份的结果摘要（手动备份 API 返回值）。
type BackupResult struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	File      string    `json:"file"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

// BackupManifestFile zip 内单文件清单项（SHA-256 校验和，恢复前验签依据——阶段 C）。
type BackupManifestFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// BackupManifest 备份清单：文件列表 + 校验和 + 实体计数 + schema 版本锚点。
type BackupManifest struct {
	BackupID      string               `json:"backupId"`
	Kind          string               `json:"kind"`
	CreatedAtUTC  string               `json:"createdAtUtc"`
	SchemaVersion int                  `json:"schemaVersion"` // 与 SQLite schemaVersion 同源
	Files         []BackupManifestFile `json:"files"`
	Counts        map[string]int       `json:"counts"` // 各集合条数（诊断/恢复前置检查）
}

// backupFileName "backup-<UTC时间>-<kind>.zip"。
func backupFileName(ts time.Time, kind string) string {
	return fmt.Sprintf("backup-%s-%s.zip", ts.UTC().Format(backupTimeFmt), kind)
}

// parseBackupName 解析备份文件名，返回 (UTC 时间, kind, ok)。
func parseBackupName(name string) (time.Time, string, bool) {
	if !strings.HasPrefix(name, "backup-") || !strings.HasSuffix(name, ".zip") {
		return time.Time{}, "", false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(name, "backup-"), ".zip")
	i := strings.LastIndex(mid, "-")
	if i < 0 || i+1 >= len(mid) {
		return time.Time{}, "", false
	}
	ts, err := time.Parse(backupTimeFmt, mid[:i])
	if err != nil {
		return time.Time{}, "", false
	}
	return ts.UTC(), mid[i+1:], true
}

// Backup 对当前内存态做一次备份，kind ∈ {auto, manual, prewrite}。
// 返回备份摘要；zip 半写失败时清理 tmp 并返回错误，不影响既有备份。
func (s *Store) Backup(kind string) (BackupResult, error) {
	s.mu.RLock()
	loaded := s.loaded
	s.mu.RUnlock()
	if !loaded {
		return BackupResult{}, errors.New("backup: store 尚未完成 Load")
	}

	files, counts, err := s.snapshotForBackup()
	if err != nil {
		return BackupResult{}, fmt.Errorf("backup: 快照序列化失败: %w", err)
	}

	dir := filepath.Join(s.dataDir, backupDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BackupResult{}, fmt.Errorf("backup: mkdir %s: %w", dir, err)
	}

	// 命名去重：同秒同类备份时间戳顺延 1s（backupClock 注入点保证测试可控）。
	ts := backupClock().UTC()
	name := backupFileName(ts, kind)
	for i := 1; i < 120; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			break
		}
		ts = ts.Add(time.Second)
		name = backupFileName(ts, kind)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, "backup-"), ".zip")

	manifest := BackupManifest{
		BackupID:      id,
		Kind:          kind,
		CreatedAtUTC:  ts.Format(time.RFC3339),
		SchemaVersion: schemaVersion,
		Counts:        counts,
	}
	manifestBytes, err := buildBackupZip(filepath.Join(dir, name), files, manifest)
	if err != nil {
		return BackupResult{}, err
	}
	_ = manifestBytes // 清单已随 zip 落盘；返回值保留给阶段 C 验签复用

	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return BackupResult{}, fmt.Errorf("backup: stat %s: %w", name, err)
	}
	res := BackupResult{ID: id, Kind: kind, File: name, Size: info.Size(), CreatedAt: ts}

	// 轮转仅针对系统生成池（auto + prewrite）；manual 永久保留。
	if kind != BackupKindManual {
		s.rotateBackupPool(dir)
	}
	logger.Info("store", "backup ok: %s (kind=%s, files=%d, size=%d)", name, kind, len(files), info.Size())
	return res, nil
}

// snapshotForBackup 持 RLock 一次锁定序列化全部集合，返回 zip 内文件内容（JSON 字节）。
// servers/settings 以磁盘密文视图入包（含凭据密文，不含密钥本体）。
func (s *Store) snapshotForBackup() (map[string][]byte, map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	encServers := security.EncryptServersForDisk(s.secretKey, s.servers)
	encSettings := security.EncryptSettingsForDisk(s.secretKey, s.settings)

	payloads := map[string]any{
		"tasks.json":               model.TasksFile{Version: 1, Tasks: s.tasks},
		"history_tasks.json":       model.HistoryFile{Version: 1, Tasks: s.history},
		"server.json":              model.ServersFile{Version: 1, Servers: encServers},
		"transcode_profile.json":   model.ProfilesFile{Version: 1, Profiles: s.profiles},
		"locks.json":               model.LocksFile{Version: 1, Locks: s.locks},
		"settings.json":            model.SettingsFile{Version: 1, Settings: encSettings},
		"video_cache.json":         model.VideoCacheFile{Version: 1, Entries: s.videoCache},
		"projects.json":            model.ProjectsFile{Version: 1, Projects: s.projects},
		"node_caps.json":           model.NodeCapsFile{Version: 1, Items: s.nodeCaps},
		"node_health_samples.json": model.NodeHealthFile{Version: 1, Samples: s.healthSamples},
		"audit_log.json":           model.AuditLogFile{Version: 1, Entries: s.auditLog},
		"asset_proxies.json":       model.AssetProxiesFile{Version: 1, Items: s.assetProxies},
	}
	counts := map[string]int{
		"tasks": len(s.tasks), "history": len(s.history), "servers": len(s.servers),
		"profiles": len(s.profiles), "locks": len(s.locks),
		"videoCache": len(s.videoCache), "projects": len(s.projects),
		"nodeCaps": len(s.nodeCaps), "healthSamples": len(s.healthSamples),
		"audit": len(s.auditLog), "assetProxies": len(s.assetProxies),
	}

	files := make(map[string][]byte, len(payloads))
	for name, v := range payloads {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("marshal %s: %w", name, err)
		}
		files[name] = b
	}
	return files, counts, nil
}

// buildBackupZip 原子写出备份包：tmp + 逐文件（SHA-256 记录）+ manifest.json + Rename。
// 返回已写盘的 manifest JSON 字节（阶段 C 验签读取时复用同一结构）。
func buildBackupZip(finalPath string, files map[string][]byte, manifest BackupManifest) ([]byte, error) {
	// 稳定序：按文件名排序写 zip，manifest 文件列表可预期。
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	dir := filepath.Dir(finalPath)
	tmp, err := os.CreateTemp(dir, ".backup-*.zip.tmp")
	if err != nil {
		return nil, fmt.Errorf("backup: create tmp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	zw := zip.NewWriter(tmp)
	entries := make([]BackupManifestFile, 0, len(names))
	for _, name := range names {
		content := files[name]
		w, err := zw.Create(name)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("backup: zip create %s: %w", name, err)
		}
		if _, err := io.Copy(w, strings.NewReader(string(content))); err != nil {
			cleanup()
			return nil, fmt.Errorf("backup: zip write %s: %w", name, err)
		}
		sum := sha256.Sum256(content)
		entries = append(entries, BackupManifestFile{Name: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))})
	}

	manifest.Files = entries
	mb, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("backup: marshal manifest: %w", err)
	}
	mw, err := zw.Create(backupManifest)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("backup: zip create manifest: %w", err)
	}
	if _, err := mw.Write(mb); err != nil {
		cleanup()
		return nil, fmt.Errorf("backup: zip write manifest: %w", err)
	}

	if err := zw.Close(); err != nil {
		cleanup()
		return nil, fmt.Errorf("backup: zip close: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return nil, fmt.Errorf("backup: sync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("backup: close tmp: %w", err)
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("backup: rename -> %s: %w", filepath.Base(finalPath), err)
	}
	return mb, nil
}

// rotateBackupPool 系统生成池轮转：auto+prewrite 按时间倒序保留 backupKeepTimed 份，
// 其余移入删除（备份目录数据本身即冗余快照，直接删除，不再入 _trash 套娃）。
func (s *Store) rotateBackupPool(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.Warn("store", "backup rotate: readdir %s: %v", dir, err)
		return
	}
	type item struct {
		name string
		ts   time.Time
	}
	var timed []item
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ts, kind, ok := parseBackupName(e.Name())
		if !ok || kind == BackupKindManual {
			continue
		}
		timed = append(timed, item{name: e.Name(), ts: ts})
	}
	if len(timed) <= backupKeepTimed {
		return
	}
	sort.Slice(timed, func(i, j int) bool { return timed[i].ts.After(timed[j].ts) })
	for _, it := range timed[backupKeepTimed:] {
		if err := os.Remove(filepath.Join(dir, it.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("store", "backup rotate: remove %s: %v", it.name, err)
		} else {
			logger.Info("store", "backup rotate: removed %s (keep=%d)", it.name, backupKeepTimed)
		}
	}
}

// preCriticalBackup 关键写前备份（去抖：prewriteMinInterval 窗口内共享上一份）。
// 失败仅告警不阻断业务写入——备份是尽力保障，配置写入本身有原子写与 SQLite 兜底。
// 调用方须在改动内存态之前（未持锁）调用，保证快照为写前状态。
func (s *Store) preCriticalBackup() {
	now := backupClock()
	s.mu.Lock()
	if !s.lastPrewriteBackup.IsZero() && now.Sub(s.lastPrewriteBackup) < prewriteMinInterval {
		s.mu.Unlock()
		return
	}
	s.lastPrewriteBackup = now
	s.mu.Unlock()

	if _, err := s.Backup(BackupKindPrewrite); err != nil {
		logger.Warn("store", "关键写前备份失败（不阻断本次写入）: %v", err)
	}
}

// ListBackups 返回备份目录内的备份列表（按时间倒序；含 manual）。
// 只解析文件名与 stat 信息，不打开 zip；恢复前的验签由阶段 C 读取 manifest 完成。
func (s *Store) ListBackups() ([]BackupResult, error) {
	dir := filepath.Join(s.dataDir, backupDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []BackupResult{}, nil
		}
		return nil, err
	}
	out := make([]BackupResult, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ts, kind, ok := parseBackupName(e.Name())
		if !ok {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupResult{
			ID:        strings.TrimSuffix(strings.TrimPrefix(e.Name(), "backup-"), ".zip"),
			Kind:      kind,
			File:      e.Name(),
			Size:      fi.Size(),
			CreatedAt: ts,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
