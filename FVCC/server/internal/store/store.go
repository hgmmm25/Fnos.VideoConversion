package store

import (
	"fvcc/internal/store/model"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"fvcc/logger"
	"fvcc/internal/security"
)

// Store 内存缓存 + 原子持久化的配置存储。
// MVP 实现：读多写少场景，内存缓存优先，变更时异步落地。
// TODO(P1): 定时备份、配置迁移、快照回滚。
type Store struct {
	mu         sync.RWMutex
	dataDir    string
	secretKey  []byte // P0-1 凭据主密钥（AES-256，0600 落盘 <dataDir>/secret.key）
	tasks      []model.Task
	history    []model.Task
	servers    []model.Server
	profiles   []model.Profile
	locks      []model.Lock
	settings   model.Settings
	videoCache []model.VideoInfoCache
	// ===== B-01：调度与持久化扩展（06 §2）=====
	projects      []model.Project          // EDL 项目
	nodeCaps      []model.NodeCaps         // 渲染节点能力快照
	healthSamples []model.NodeHealthSample // 节点健康采样
	auditLog      []model.AuditEntry       // 审计日志
	// ===== M4：代理映射（04 §4.2）=====
	assetProxies []model.AssetProxy // 素材代理映射（asset_proxies.json）
	loaded       bool
	orderCounter int64 // 任务顺序号计数器，保证任务按创建顺序处理

	// ===== P1-1：调度器事件驱动优化（2026-09-21）=====
	// 内存索引：taskByID 提供 O(1) 定位（替代 GetTask 全量遍历），checksumIdx 供
	// 渲染幂等去重（06 §4.4）。索引惰性重建：任何 slice 结构变动（追加/删除/移历史）
	// 置 indexDirty，下次查询时一次重建，避免每次 Upsert 维护索引的额外开销。
	taskByID    map[string]int
	checksumIdx map[string][]int
	indexDirty  bool
	// 脏标记：高频任务状态变更仅标记，由 Flush 定时落盘，替代每次变更即时全量写盘
	// （降低 IO；锁/服务器/项目等低频敏感集合保持即时落盘）。
	dirtyTasks   atomic.Bool
	dirtyHistory atomic.Bool
	// 任务变更钩子（解锁后回调）：调度器经此钩子即时感知任务创建/状态变更，
	// 驱动事件循环替代 1s tick 全量扫描。
	taskHook func(taskID string, status model.TaskStatus)
}

// DataDir 返回数据目录（供日志等旁路文件定位）。
func (s *Store) DataDir() string { return s.dataDir }

// NewStore 创建存储实例，首次调用 Load 加载数据。
func NewStore(dataDir string) *Store {
	return &Store{dataDir: dataDir}
}

// Load 从磁盘加载全部配置到内存。
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}

	// P0-1：加载凭据主密钥（不存在则生成），后续 settings/server 解密依赖它。
	key, err := security.LoadOrCreateSecretKey(s.dataDir)
	if err != nil {
		logger.Error("store", "加载凭据主密钥失败: %v", err)
		return err
	}
	s.secretKey = key

	// tasks.json 采用严格加载 + 列迁移（06 §2.2）：解析失败或版本过高直接拒绝启动，
	// 禁止静默降级为"空任务列表"而丢失用户任务。
	if err := s.loadTasksLocked(); err != nil {
		return err
	}
	s.history = loadJSON[model.HistoryFile](s.path("history_tasks.json"), model.HistoryFile{Version: 1, Tasks: []model.Task{}}).Tasks
	s.servers = security.DecryptServersOnLoad(key, loadJSON[model.ServersFile](s.path("server.json"), model.ServersFile{Version: 1, Servers: []model.Server{}}).Servers)
	s.profiles = loadJSON[model.ProfilesFile](s.path("transcode_profile.json"), model.ProfilesFile{Version: 1, Profiles: []model.Profile{}}).Profiles
	s.locks = loadJSON[model.LocksFile](s.path("locks.json"), model.LocksFile{Version: 1, Locks: []model.Lock{}}).Locks
	s.settings = security.DecryptSettingsOnLoad(key, loadJSON[model.SettingsFile](s.path("settings.json"), model.SettingsFile{Version: 1, Settings: model.DefaultSettings()}).Settings)
	s.videoCache = loadJSON[model.VideoCacheFile](s.path("video_cache.json"), model.VideoCacheFile{Version: 1, Entries: []model.VideoInfoCache{}}).Entries
	// B-01 新增集合（06 §2.1）：不存在时按空集合初始化，不阻断启动。
	s.projects = loadJSON[model.ProjectsFile](s.path("projects.json"), model.ProjectsFile{Version: 1, Projects: []model.Project{}}).Projects
	// 派生字段不落盘（03 §2.2）：加载后统一重算，并补齐 schema_ver 缺省（03 §7）。
	for i := range s.projects {
		if s.projects[i].SchemaVer == 0 {
			s.projects[i].SchemaVer = 1
		}
		normalizeProject(&s.projects[i])
	}
	s.nodeCaps = loadJSON[model.NodeCapsFile](s.path("node_caps.json"), model.NodeCapsFile{Version: 1, Items: []model.NodeCaps{}}).Items
	s.healthSamples = loadJSON[model.NodeHealthFile](s.path("node_health_samples.json"), model.NodeHealthFile{Version: 1, Samples: []model.NodeHealthSample{}}).Samples
	s.auditLog = loadJSON[model.AuditLogFile](s.path("audit_log.json"), model.AuditLogFile{Version: 1, Entries: []model.AuditEntry{}}).Entries
	// M4 新增集合（04 §4.2）：不存在时按空集合初始化，不阻断启动。
	s.assetProxies = loadJSON[model.AssetProxiesFile](s.path("asset_proxies.json"), model.AssetProxiesFile{Version: 1, Items: []model.AssetProxy{}}).Items

	// 启动崩溃恢复：非终态的中断任务重置为 QUEUE
	for i := range s.tasks {
		t := &s.tasks[i]
		if !t.Status.IsTerminal() && t.Status != model.StatusQueue {
			logger.Info("store", "recover task %s %s -> QUEUE (was interrupted)", t.ID, t.Status)
			t.Status = model.StatusQueue
			t.Progress = 0
			t.UploadChunkIdx = 0
			t.RemoteTaskID = ""
			t.DownloadOffset = 0
			t.CoolDownUntil = nil
			t.UpdatedAt = time.Now()
		}
		// 补全服务器名称和方案名称（旧任务可能没有这些字段）
		if t.ServerName == "" {
			if t.ServerID == "_local_" {
				t.ServerName = "fnNAS 自转码"
			} else {
				for _, sv := range s.servers {
					if sv.ID == t.ServerID {
						t.ServerName = sv.Name
						break
					}
				}
			}
		}
		if t.ProfileName == "" {
			for _, pf := range s.profiles {
				if pf.ID == t.ProfileID {
					t.ProfileName = pf.Name
					break
				}
			}
		}
		// 恢复 orderCounter 到最大 OrderID，避免重复
		if t.OrderID > s.orderCounter {
			s.orderCounter = t.OrderID
		}
	}

	s.loaded = true
	// P1-1：加载完成后重建任务内存索引（taskByID / checksumIdx）。
	s.ensureIndexLocked()
	logger.Info("store", "loaded: tasks=%d history=%d servers=%d profiles=%d locks=%d projects=%d nodeCaps=%d healthSamples=%d audit=%d proxies=%d",
		len(s.tasks), len(s.history), len(s.servers), len(s.profiles), len(s.locks),
		len(s.projects), len(s.nodeCaps), len(s.healthSamples), len(s.auditLog), len(s.assetProxies))
	return nil
}

// ===== Tasks =====

func (s *Store) GetTasks() []model.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Task, len(s.tasks))
	copy(out, s.tasks)
	sort.Slice(out, func(i, j int) bool {
		return out[i].OrderID < out[j].OrderID
	})
	return out
}

// GetTask 按 ID 定位任务（P1-1：优先走内存索引，索引失效时惰性重建）。
func (s *Store) GetTask(id string) (model.Task, bool) {
	s.mu.RLock()
	if s.indexReadyLocked() {
		if idx, ok := s.taskByID[id]; ok && idx >= 0 && idx < len(s.tasks) && s.tasks[idx].ID == id {
			t := s.tasks[idx]
			s.mu.RUnlock()
			return t, true
		}
		s.mu.RUnlock()
		return model.Task{}, false
	}
	s.mu.RUnlock()

	// 索引未就绪：升级写锁重建（读多写少，重建频率低可接受）。
	s.mu.Lock()
	s.ensureIndexLocked()
	idx, ok := s.taskByID[id]
	if !ok || idx < 0 || idx >= len(s.tasks) || s.tasks[idx].ID != id {
		s.mu.Unlock()
		return model.Task{}, false
	}
	t := s.tasks[idx]
	s.mu.Unlock()
	return t, true
}

func (s *Store) UpsertTask(t model.Task) {
	s.mu.Lock()
	t.UpdatedAt = time.Now()
	if t.Status == model.StatusCompleted {
		if t.Progress < 100 {
			t.Progress = 100
		}
	}
	if t.Progress < 0 {
		t.Progress = 0
	} else if t.Progress > 100 {
		t.Progress = 100
	}

	needPersist := true
	for i, ex := range s.tasks {
		if ex.ID == t.ID {
			if t.Status == ex.Status {
				if t.Progress < ex.Progress {
					t.Progress = ex.Progress
				}
			}
			if t.Status == ex.Status && t.Progress == ex.Progress && t.ErrorMsg == ex.ErrorMsg {
				needPersist = false
			}
			// P1-1：元素下标不变，仅当校验码变化时置脏索引（下次查询重建）。
			if s.indexReadyLocked() && t.Checksum != ex.Checksum {
				s.indexDirty = true
			}
			s.tasks[i] = t
			s.mu.Unlock()
			if needPersist {
				s.markTasksDirty()
			}
			s.fireTaskChange(t.ID, t.Status)
			return
		}
	}
	s.tasks = append(s.tasks, t)
	// P1-1：索引就绪时增量维护（追加不改变既有下标）；未就绪则整体重建。
	if s.indexReadyLocked() {
		s.taskByID[t.ID] = len(s.tasks) - 1
		if t.Checksum != "" {
			s.checksumIdx[t.Checksum] = append(s.checksumIdx[t.Checksum], len(s.tasks)-1)
		}
	} else {
		s.indexDirty = true
	}
	s.mu.Unlock()
	s.markTasksDirty()
	s.fireTaskChange(t.ID, t.Status)
}

// SetTaskFinishedAt 记录任务终态时刻（P2-1：FinishedAt 由成功/失败终态写入）。
func (s *Store) SetTaskFinishedAt(id string, ts time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].FinishedAt = &ts
			s.tasks[i].UpdatedAt = time.Now()
			s.persistTasks()
			return
		}
	}
}

// SetTaskStartedAt 记录任务首次进入执行态时刻（P2-1：StartedAt 由派发/启动成功写入，nil=未开始）。
func (s *Store) SetTaskStartedAt(id string, ts time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id && s.tasks[i].StartedAt == nil {
			s.tasks[i].StartedAt = &ts
			s.tasks[i].UpdatedAt = time.Now()
			s.persistTasks()
			return
		}
	}
}

// NextOrderID 获取下一个任务顺序号，保证任务按创建顺序处理。
func (s *Store) NextOrderID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orderCounter++
	return s.orderCounter
}

func (s *Store) UpdateTaskStatus(id string, status model.TaskStatus, progress float64, errMsg string) {
	s.mu.Lock()
	needPersist := true
	statusChanged := false
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			oldStatus := s.tasks[i].Status
			oldProgress := s.tasks[i].Progress
			oldErrMsg := s.tasks[i].ErrorMsg

			// 终态任务（已完成/已取消）不允许被覆盖回非终态
			if oldStatus.IsTerminal() && !status.IsTerminal() {
				needPersist = false
				break
			}

			if status == model.StatusCompleted {
				progress = 100
			} else if status == oldStatus {
				if progress < oldProgress {
					progress = oldProgress
				}
			}
			if progress < 0 {
				progress = 0
			} else if progress > 100 {
				progress = 100
			}

			if status == oldStatus && progress == oldProgress && (errMsg == "" || errMsg == oldErrMsg) {
				needPersist = false
			}

			s.tasks[i].Status = status
			s.tasks[i].Progress = progress
			if errMsg != "" {
				s.tasks[i].ErrorMsg = errMsg
			}
			s.tasks[i].UpdatedAt = time.Now()
			statusChanged = status != oldStatus

			// 报错任务移到队列末尾
			if status == model.StatusError && oldStatus != model.StatusError {
				s.orderCounter++
				s.tasks[i].OrderID = s.orderCounter
			}
			break
		}
	}
	s.mu.Unlock()
	if needPersist {
		s.markTasksDirty()
	}
	// P1-1：状态变更时触发事件（进度更新不触发，避免事件风暴）。
	if statusChanged {
		s.fireTaskChange(id, status)
	}
}

func (s *Store) MoveToHistory(id string) {
	s.mu.Lock()
	var moved *model.Task
	for i, t := range s.tasks {
		if t.ID == id {
			moved = &t
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			break
		}
	}
	if moved != nil {
		// 历史保留最近 1000 条
		if len(s.history) >= 1000 {
			s.history = s.history[1:]
		}
		s.history = append(s.history, *moved)
		// P1-1：slice 结构变动，索引待重建。
		s.indexDirty = true
	}
	s.mu.Unlock()
	if moved != nil {
		s.markTasksDirty()
		s.markHistoryDirty()
	}
}

func (s *Store) DeleteTask(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tasks {
		if t.ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			// P1-1：slice 结构变动，索引待重建。
			s.indexDirty = true
			s.markTasksDirty()
			return true
		}
	}
	return false
}

func (s *Store) DeleteHistoryTask(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.history {
		if t.ID == id {
			s.history = append(s.history[:i], s.history[i+1:]...)
			s.markHistoryDirty()
			return true
		}
	}
	return false
}

func (s *Store) ReorderTasks(taskIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskMap := make(map[string]*model.Task)
	for i := range s.tasks {
		taskMap[s.tasks[i].ID] = &s.tasks[i]
	}

	var newOrder int64 = 1
	maxOrder := int64(len(taskIDs) + 1)
	for _, id := range taskIDs {
		if t, ok := taskMap[id]; ok {
			t.OrderID = newOrder
			t.UpdatedAt = time.Now()
			newOrder++
		}
	}

	for _, t := range s.tasks {
		if t.OrderID == 0 {
			t.OrderID = maxOrder
			maxOrder++
		}
	}

	s.orderCounter = maxOrder - 1

	// P1-1：仅 OrderID 变化（元素顺序不变），索引无需重建，标记落盘即可。
	s.markTasksDirty()
	return nil
}

func (s *Store) GetHistory() []model.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Task, len(s.history))
	copy(out, s.history)
	return out
}

// ===== Servers =====

func (s *Store) GetServers() []model.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Server, len(s.servers))
	copy(out, s.servers)
	return out
}

func (s *Store) GetServer(id string) (model.Server, bool) {
	if id == "_local_" {
		return model.Server{
			ID:      "_local_",
			Name:    "fnNAS 自转码",
			Status:  "online",
			IsLocal: true,
		}, true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sv := range s.servers {
		if sv.ID == id {
			return sv, true
		}
	}
	return model.Server{}, false
}

func (s *Store) UpsertServer(sv model.Server) {
	s.mu.Lock()
	for i, ex := range s.servers {
		if ex.ID == sv.ID {
			s.servers[i] = sv
			s.mu.Unlock()
			s.persistServers()
			return
		}
	}
	s.servers = append(s.servers, sv)
	s.mu.Unlock()
	s.persistServers()
}

func (s *Store) DeleteServer(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sv := range s.servers {
		if sv.ID == id {
			s.servers = append(s.servers[:i], s.servers[i+1:]...)
			// 同时清理锁
			for j, l := range s.locks {
				if l.ServerID == id {
					s.locks = append(s.locks[:j], s.locks[j+1:]...)
					break
				}
			}
			s.persistServers()
			s.persistLocks()
			return true
		}
	}
	return false
}

func (s *Store) UpdateServerStatus(id, status string) {
	s.mu.Lock()
	for i := range s.servers {
		if s.servers[i].ID == id {
			s.servers[i].Status = status
			break
		}
	}
	s.mu.Unlock()
	s.persistServers()
}

// ===== Profiles =====

func (s *Store) GetProfiles() []model.Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Profile, len(s.profiles))
	copy(out, s.profiles)
	return out
}

func (s *Store) GetProfile(id string) (model.Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.profiles {
		if p.ID == id {
			return p, true
		}
	}
	return model.Profile{}, false
}

func (s *Store) UpsertProfile(p model.Profile) {
	s.mu.Lock()
	for i, ex := range s.profiles {
		if ex.ID == p.ID {
			s.profiles[i] = p
			s.mu.Unlock()
			s.persistProfiles()
			return
		}
	}
	s.profiles = append(s.profiles, p)
	s.mu.Unlock()
	s.persistProfiles()
}

func (s *Store) DeleteProfile(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.profiles {
		if p.ID == id {
			s.profiles = append(s.profiles[:i], s.profiles[i+1:]...)
			s.persistProfiles()
			return true
		}
	}
	return false
}

// ===== Locks =====

func (s *Store) GetLocks() []model.Lock {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Lock, len(s.locks))
	copy(out, s.locks)
	return out
}

// AcquireTransLock 尝试获取传输锁，成功返回 true。
func (s *Store) AcquireTransLock(serverID, taskID string, expireSec int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.locks {
		if s.locks[i].ServerID == serverID {
			if s.locks[i].TransLock != nil && time.Now().Before(s.locks[i].TransLock.LockExpireAt) {
				if s.locks[i].TransLock.TaskID != taskID {
					return false // 被其他任务占用
				}
			}
			s.locks[i].TransLock = &model.LockEntry{TaskID: taskID, LockExpireAt: time.Now().Add(time.Duration(expireSec) * time.Second)}
			s.persistLocks()
			return true
		}
	}
	// 服务器还没有锁记录，新建
	s.locks = append(s.locks, model.Lock{
		ServerID:  serverID,
		TransLock: &model.LockEntry{TaskID: taskID, LockExpireAt: time.Now().Add(time.Duration(expireSec) * time.Second)},
	})
	s.persistLocks()
	return true
}

// AcquireCodeLock 尝试获取转码锁。
func (s *Store) AcquireCodeLock(serverID, taskID string, expireSec int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.locks {
		if s.locks[i].ServerID == serverID {
			if s.locks[i].CodeLock != nil && time.Now().Before(s.locks[i].CodeLock.LockExpireAt) {
				if s.locks[i].CodeLock.TaskID != taskID {
					return false
				}
			}
			s.locks[i].CodeLock = &model.LockEntry{TaskID: taskID, LockExpireAt: time.Now().Add(time.Duration(expireSec) * time.Second)}
			s.persistLocks()
			return true
		}
	}
	s.locks = append(s.locks, model.Lock{
		ServerID: serverID,
		CodeLock: &model.LockEntry{TaskID: taskID, LockExpireAt: time.Now().Add(time.Duration(expireSec) * time.Second)},
	})
	s.persistLocks()
	return true
}

// ReleaseTransLock 释放传输锁（仅当持有者是 taskID）。
func (s *Store) ReleaseTransLock(serverID, taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.locks {
		if s.locks[i].ServerID == serverID && s.locks[i].TransLock != nil && s.locks[i].TransLock.TaskID == taskID {
			s.locks[i].TransLock = nil
			s.persistLocks()
			return
		}
	}
}

// ReleaseCodeLock 释放转码锁。
func (s *Store) ReleaseCodeLock(serverID, taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.locks {
		if s.locks[i].ServerID == serverID && s.locks[i].CodeLock != nil && s.locks[i].CodeLock.TaskID == taskID {
			s.locks[i].CodeLock = nil
			s.persistLocks()
			return
		}
	}
}

// SweepExpiredLocks 回收所有过期锁，返回被释放的任务 ID 集合。
func (s *Store) SweepExpiredLocks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range s.locks {
		if s.locks[i].TransLock != nil && !now.Before(s.locks[i].TransLock.LockExpireAt) {
			s.locks[i].TransLock = nil
		}
		if s.locks[i].CodeLock != nil && !now.Before(s.locks[i].CodeLock.LockExpireAt) {
			s.locks[i].CodeLock = nil
		}
	}
	s.persistLocks()
}

// ReleaseAllLocks 释放所有锁（程序退出时调用）。
func (s *Store) ReleaseAllLocks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.locks {
		s.locks[i].TransLock = nil
		s.locks[i].CodeLock = nil
	}
	s.persistLocks()
}

// ===== P1-1：内存索引 + 事件钩子 + 定时落盘（2026-09-21）=====

// ensureIndexLocked 惰性重建任务内存索引（需持有写锁）：
// taskByID 按 ID 定位任务下标（GetTask O(1)），checksumIdx 供渲染幂等去重（06 §4.4）。
func (s *Store) ensureIndexLocked() {
	if s.indexReadyLocked() {
		return
	}
	s.taskByID = make(map[string]int, len(s.tasks))
	s.checksumIdx = make(map[string][]int)
	for i := range s.tasks {
		s.taskByID[s.tasks[i].ID] = i
		if s.tasks[i].Checksum != "" {
			s.checksumIdx[s.tasks[i].Checksum] = append(s.checksumIdx[s.tasks[i].Checksum], i)
		}
	}
	s.indexDirty = false
}

// indexReadyLocked 索引是否已就绪（需持有读锁）。
func (s *Store) indexReadyLocked() bool { return !s.indexDirty && s.taskByID != nil }

// SetTaskChangeHook 注册任务变更钩子（解锁后回调）。P1-1 事件驱动：
// 调度器经此钩子即时感知任务创建/状态变更，替代 1s tick 全量扫描。
func (s *Store) SetTaskChangeHook(h func(taskID string, status model.TaskStatus)) {
	s.mu.Lock()
	s.taskHook = h
	s.mu.Unlock()
}

// fireTaskChange 在解锁后调用任务变更钩子（调用方须保证已释放写锁，避免回调死锁）。
func (s *Store) fireTaskChange(taskID string, status model.TaskStatus) {
	if h := s.taskHook; h != nil {
		h(taskID, status)
	}
}

// markTasksDirty 标记任务集合待落盘（P1-1 定时落盘：高频状态变更不再逐次写盘）。
func (s *Store) markTasksDirty()   { s.dirtyTasks.Store(true) }
func (s *Store) markHistoryDirty() { s.dirtyHistory.Store(true) }

// Flush 将脏标记的 tasks/history 立即落盘（调度器定时触发与优雅退出时调用）。
// 无脏数据时直接返回，不产生任何 IO。
func (s *Store) Flush() {
	if !s.dirtyTasks.Load() && !s.dirtyHistory.Load() {
		return
	}
	s.mu.Lock()
	if s.dirtyTasks.Load() {
		s.persistTasks()
		s.dirtyTasks.Store(false)
	}
	if s.dirtyHistory.Load() {
		s.persistHistory()
		s.dirtyHistory.Store(false)
	}
	s.mu.Unlock()
}

// ===== 持久化 =====

func (s *Store) path(name string) string { return filepath.Join(s.dataDir, name) }

func (s *Store) persistTasks() {
	saveJSON(s.path("tasks.json"), model.TasksFile{Version: StoreSchemaVersion, Tasks: s.tasks})
}
func (s *Store) persistHistory() {
	saveJSON(s.path("history_tasks.json"), model.HistoryFile{Version: 1, Tasks: s.history})
}
func (s *Store) persistServers() {
	saveJSON(s.path("server.json"), model.ServersFile{Version: 1, Servers: security.EncryptServersForDisk(s.secretKey, s.servers)})
}
func (s *Store) persistProfiles() {
	saveJSON(s.path("transcode_profile.json"), model.ProfilesFile{Version: 1, Profiles: s.profiles})
}
func (s *Store) persistLocks() {
	saveJSON(s.path("locks.json"), model.LocksFile{Version: 1, Locks: s.locks})
}

// ===== Settings =====

func (s *Store) GetSettings() model.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) SaveSettings(v model.Settings) {
	s.mu.Lock()
	s.settings = v
	s.mu.Unlock()
	s.persistSettings()
}

func (s *Store) persistSettings() {
	saveJSON(s.path("settings.json"), model.SettingsFile{Version: 1,Settings: security.EncryptSettingsForDisk(s.secretKey, s.settings)})
}

// ===== VideoCache =====

func (s *Store) GetVideoCache(path string) (model.VideoInfoCache, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.videoCache {
		if c.Path == path {
			return c, true
		}
	}
	return model.VideoInfoCache{}, false
}

func (s *Store) UpsertVideoCache(c model.VideoInfoCache) {
	s.mu.Lock()
	c.UpdatedAt = time.Now()
	for i, ex := range s.videoCache {
		if ex.Path == c.Path {
			s.videoCache[i] = c
			s.mu.Unlock()
			s.persistVideoCache()
			return
		}
	}
	s.videoCache = append(s.videoCache, c)
	s.mu.Unlock()
	s.persistVideoCache()
}

func (s *Store) DeleteVideoCache(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.videoCache {
		if c.Path == path {
			s.videoCache = append(s.videoCache[:i], s.videoCache[i+1:]...)
			s.persistVideoCache()
			return true
		}
	}
	return false
}

func (s *Store) ClearVideoCache() {
	s.mu.Lock()
	s.videoCache = []model.VideoInfoCache{}
	s.mu.Unlock()
	s.persistVideoCache()
}

func (s *Store) GetVideoCacheCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.videoCache)
}

func (s *Store) persistVideoCache() {
	saveJSON(s.path("video_cache.json"), model.VideoCacheFile{Version: 1, Entries: s.videoCache})
}

// loadJSON 从文件加载 JSON，不存在或解析失败时返回默认值。
func loadJSON[T any](path string, def T) T {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("store", "read %s: %v", path, err)
		}
		return def
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		logger.Warn("store", "parse %s: %v, using default", path, err)
		return def
	}
	return v
}

// saveJSON 原子写入 JSON 文件：tmp 临时文件 + Rename，防止写入中途崩溃损坏。
func saveJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		logger.Warn("store", "marshal %s: %v", path, err)
		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("store", "mkdir %s: %v", dir, err)
		return
	}

	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		logger.Warn("store", "create temp file in %s: %v", dir, err)
		return
	}
	tmp := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmp)
		logger.Warn("store", "write tmp %s: %v", tmp, err)
		return
	}
	tmpFile.Close()

	if err := os.Rename(tmp, path); err != nil {
		logger.Warn("store", "rename %s -> %s: %v, trying fallback", tmp, path, err)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("store", "remove old %s: %v", path, err)
			os.Remove(tmp)
			return
		}
		if err := os.Rename(tmp, path); err != nil {
			logger.Warn("store", "retry rename %s -> %s: %v, trying copy-delete", tmp, path, err)
			tmpData, readErr := os.ReadFile(tmp)
			if readErr != nil {
				logger.Warn("store", "read tmp %s: %v", tmp, readErr)
				return
			}
			if err := os.WriteFile(path, tmpData, 0o644); err != nil {
				logger.Warn("store", "write %s: %v", path, err)
				os.Remove(tmp)
				return
			}
			os.Remove(tmp)
		}
	}
}
