package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"fvcc/logger"
)

// Store 内存缓存 + 原子持久化的配置存储。
// MVP 实现：读多写少场景，内存缓存优先，变更时异步落地。
// TODO(P1): 定时备份、配置迁移、快照回滚。
type Store struct {
	mu           sync.RWMutex
	dataDir      string
	tasks        []Task
	history      []Task
	servers      []Server
	profiles     []Profile
	locks        []Lock
	settings     Settings
	videoCache   []VideoInfoCache
	loaded       bool
	orderCounter int64 // 任务顺序号计数器，保证任务按创建顺序处理
}

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

	s.tasks = loadJSON[TasksFile](s.path("tasks.json"), TasksFile{Version: 1, Tasks: []Task{}}).Tasks
	s.history = loadJSON[HistoryFile](s.path("history_tasks.json"), HistoryFile{Version: 1, Tasks: []Task{}}).Tasks
	s.servers = loadJSON[ServersFile](s.path("server.json"), ServersFile{Version: 1, Servers: []Server{}}).Servers
	s.profiles = loadJSON[ProfilesFile](s.path("transcode_profile.json"), ProfilesFile{Version: 1, Profiles: []Profile{}}).Profiles
	s.locks = loadJSON[LocksFile](s.path("locks.json"), LocksFile{Version: 1, Locks: []Lock{}}).Locks
	s.settings = loadJSON[SettingsFile](s.path("settings.json"), SettingsFile{Version: 1, Settings: DefaultSettings()}).Settings
	s.videoCache = loadJSON[VideoCacheFile](s.path("video_cache.json"), VideoCacheFile{Version: 1, Entries: []VideoInfoCache{}}).Entries

	// 启动崩溃恢复：非终态的中断任务重置为 QUEUE
	for i := range s.tasks {
		t := &s.tasks[i]
		if !t.Status.IsTerminal() && t.Status != StatusQueue {
			logger.Info("store", "recover task %s %s -> QUEUE (was interrupted)", t.ID, t.Status)
			t.Status = StatusQueue
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
	logger.Info("store", "loaded: tasks=%d history=%d servers=%d profiles=%d locks=%d",
		len(s.tasks), len(s.history), len(s.servers), len(s.profiles), len(s.locks))
	return nil
}

// ===== Tasks =====

func (s *Store) GetTasks() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, len(s.tasks))
	copy(out, s.tasks)
	sort.Slice(out, func(i, j int) bool {
		return out[i].OrderID < out[j].OrderID
	})
	return out
}

func (s *Store) GetTask(id string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

func (s *Store) UpsertTask(t Task) {
	s.mu.Lock()
	t.UpdatedAt = time.Now()
	if t.Status == StatusCompleted {
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
			s.tasks[i] = t
			s.mu.Unlock()
			if needPersist {
				s.persistTasks()
			}
			return
		}
	}
	s.tasks = append(s.tasks, t)
	s.mu.Unlock()
	s.persistTasks()
}

// NextOrderID 获取下一个任务顺序号，保证任务按创建顺序处理。
func (s *Store) NextOrderID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orderCounter++
	return s.orderCounter
}

func (s *Store) UpdateTaskStatus(id string, status TaskStatus, progress float64, errMsg string) {
	s.mu.Lock()
	needPersist := true
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

			if status == StatusCompleted {
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

			// 报错任务移到队列末尾
			if status == StatusError && oldStatus != StatusError {
				s.orderCounter++
				s.tasks[i].OrderID = s.orderCounter
			}
			break
		}
	}
	s.mu.Unlock()
	if needPersist {
		s.persistTasks()
	}
}

func (s *Store) MoveToHistory(id string) {
	s.mu.Lock()
	var moved *Task
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
	}
	s.mu.Unlock()
	s.persistTasks()
	s.persistHistory()
}

func (s *Store) DeleteTask(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tasks {
		if t.ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			s.persistTasks()
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
			s.persistHistory()
			return true
		}
	}
	return false
}

func (s *Store) ReorderTasks(taskIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskMap := make(map[string]*Task)
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

	s.persistTasks()
	return nil
}

func (s *Store) GetHistory() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, len(s.history))
	copy(out, s.history)
	return out
}

// ===== Servers =====

func (s *Store) GetServers() []Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Server, len(s.servers))
	copy(out, s.servers)
	return out
}

func (s *Store) GetServer(id string) (Server, bool) {
	if id == "_local_" {
		return Server{
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
	return Server{}, false
}

func (s *Store) UpsertServer(sv Server) {
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

func (s *Store) GetProfiles() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Profile, len(s.profiles))
	copy(out, s.profiles)
	return out
}

func (s *Store) GetProfile(id string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func (s *Store) UpsertProfile(p Profile) {
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

func (s *Store) GetLocks() []Lock {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Lock, len(s.locks))
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
			s.locks[i].TransLock = &LockEntry{TaskID: taskID, LockExpireAt: time.Now().Add(time.Duration(expireSec) * time.Second)}
			s.persistLocks()
			return true
		}
	}
	// 服务器还没有锁记录，新建
	s.locks = append(s.locks, Lock{
		ServerID:  serverID,
		TransLock: &LockEntry{TaskID: taskID, LockExpireAt: time.Now().Add(time.Duration(expireSec) * time.Second)},
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
			s.locks[i].CodeLock = &LockEntry{TaskID: taskID, LockExpireAt: time.Now().Add(time.Duration(expireSec) * time.Second)}
			s.persistLocks()
			return true
		}
	}
	s.locks = append(s.locks, Lock{
		ServerID: serverID,
		CodeLock: &LockEntry{TaskID: taskID, LockExpireAt: time.Now().Add(time.Duration(expireSec) * time.Second)},
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

// ===== 持久化 =====

func (s *Store) path(name string) string { return filepath.Join(s.dataDir, name) }

func (s *Store) persistTasks() {
	saveJSON(s.path("tasks.json"), TasksFile{Version: 1, Tasks: s.tasks})
}
func (s *Store) persistHistory() {
	saveJSON(s.path("history_tasks.json"), HistoryFile{Version: 1, Tasks: s.history})
}
func (s *Store) persistServers() {
	saveJSON(s.path("server.json"), ServersFile{Version: 1, Servers: s.servers})
}
func (s *Store) persistProfiles() {
	saveJSON(s.path("transcode_profile.json"), ProfilesFile{Version: 1, Profiles: s.profiles})
}
func (s *Store) persistLocks() {
	saveJSON(s.path("locks.json"), LocksFile{Version: 1, Locks: s.locks})
}

// ===== Settings =====

func (s *Store) GetSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) SaveSettings(v Settings) {
	s.mu.Lock()
	s.settings = v
	s.mu.Unlock()
	s.persistSettings()
}

func (s *Store) persistSettings() {
	saveJSON(s.path("settings.json"), SettingsFile{Version: 1, Settings: s.settings})
}

// ===== VideoCache =====

func (s *Store) GetVideoCache(path string) (VideoInfoCache, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.videoCache {
		if c.Path == path {
			return c, true
		}
	}
	return VideoInfoCache{}, false
}

func (s *Store) UpsertVideoCache(c VideoInfoCache) {
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
	s.videoCache = []VideoInfoCache{}
	s.mu.Unlock()
	s.persistVideoCache()
}

func (s *Store) GetVideoCacheCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.videoCache)
}

func (s *Store) persistVideoCache() {
	saveJSON(s.path("video_cache.json"), VideoCacheFile{Version: 1, Entries: s.videoCache})
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
