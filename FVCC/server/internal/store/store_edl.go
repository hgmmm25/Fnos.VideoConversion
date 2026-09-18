package store

// B-01：调度持久化扩展（设计文档 06 §2 持久化模型 / §4.4 幂等 / §4.5 分布式锁 / §5 节点管理）
//
// 后端映射说明：设计文档以 SQLite 表描述持久化模型，本实现沿用 Store 既有
// "内存缓存 + JSON 原子落盘" 形态，一一映射如下：
//
//	projects            → projects.json            （UNIQUE(name) 由 CreateProject 显式校验）
//	node_caps           → node_caps.json           （主键 server_id 由 UpsertNodeCaps 保证）
//	node_health_samples → node_health_samples.json （保留最近 N 条由 PruneNodeHealthSamples 保证）
//	audit_log           → audit_log.json
//	tasks 列迁移        → tasks.json version 1 → 2（loadTasksLocked 幂等迁移，失败拒绝启动）
//
// 自增主键（health_samples.id / audit_log.id）以纳秒时间戳实现，语义等价且无需跨重启维护序列。
// 后续若替换为 SQLite，仅需替换本文件的读写实现，上层调用签名保持不变。

import (
	"fvcc/internal/store/model"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"fvcc/logger"
)

// StoreSchemaVersion 当前 tasks.json 结构版本（06 §2.2 列迁移完成后的版本）。
const StoreSchemaVersion = 2

// healthSampleRetainPerServer 每个节点保留的健康采样条数（06 §2.1：保留最近 N 条）。
const healthSampleRetainPerServer = 288

// auditLogMaxEntries 审计日志内存/落盘上限，超出后丢弃最旧条目。
const auditLogMaxEntries = 5000

// B-01 领域错误（供 handlers 映射为对应错误码）。
var (
	ErrProjectNotFound = errors.New("E_NOT_FOUND: 项目不存在")
	ErrProjectNameUsed = errors.New("E_NAME_EXISTS: 项目名称已存在")
	ErrRevConflict     = errors.New("E_REV_CONFLICT: 项目版本冲突，请刷新后重试")
)

// CooldownBackoffSec 计算冷却退避时长（06 §4.3）：min(30 × 2^attempt, 600)。
// attempt 为已重试次数（0 起），结果为 30/60/120/240/480/600...
func CooldownBackoffSec(attempt int) int {
	if attempt < 0 {
		attempt = 0
	}
	sec := 30 << uint(attempt)
	if sec <= 0 || sec > 600 {
		return 600
	}
	return sec
}

// ===== tasks 严格加载与列迁移 =====

// loadTasksLocked 读取 tasks.json 并执行幂等列迁移（06 §2.2）。
// 调用方必须已持有 s.mu。任一环节失败都返回错误，由 Load 上抛至 main 拒绝启动。
func (s *Store) loadTasksLocked() error {
	path := s.path("tasks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.tasks = []model.Task{}
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", path, err)
	}

	var f model.TasksFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("%s 解析失败，拒绝启动以免静默丢弃任务: %w", path, err)
	}
	if f.Version > StoreSchemaVersion {
		return fmt.Errorf("%s 结构版本为 %d，高于本程序支持的 %d，请升级 FVCC 后再启动", path, f.Version, StoreSchemaVersion)
	}

	migrated := f.Version != StoreSchemaVersion
	for i := range f.Tasks {
		if migrateTaskColumns(&f.Tasks[i]) {
			migrated = true
		}
	}
	f.Version = StoreSchemaVersion
	s.tasks = f.Tasks

	if migrated {
		logger.Info("store", "tasks.json 列迁移完成：version→%d，共 %d 条任务", StoreSchemaVersion, len(s.tasks))
		saveJSON(path, model.TasksFile{Version: StoreSchemaVersion, Tasks: s.tasks})
	}
	return nil
}

// migrateTaskColumns 为单条任务补齐缺失的新列（等价 SQLite: ALTER TABLE ADD COLUMN ... DEFAULT），
// 返回是否发生变更。只对零值赋默认，幂等，不覆盖既有数据。
func migrateTaskColumns(t *model.Task) bool {
	changed := false
	if t.TaskType == "" {
		t.TaskType = model.TaskTypeTranscode
		changed = true
	}
	if t.Status == "" {
		t.Status = model.StatusQueue
		changed = true
	}
	if t.SegIndex < 0 {
		t.SegIndex = 0
		changed = true
	}
	if t.SegTotal < 0 {
		t.SegTotal = 0
		changed = true
	}
	return changed
}

// ===== 渲染进度落库（B-07，06 §4.2 进度 loop / §6 事件聚合）=====

// RenderProgress 渲染任务进度补丁，由 FVCS 经 WS 回传的 Progress 映射而来。
type RenderProgress struct {
	Progress  float64 // 0-100
	Stage     string  // prepare|segment|concat|mux|finalize
	SegIndex  int     // 当前分段序号（1-based，<=0 表示本次不上报）
	SegTotal  int     // 分段总数（<=0 表示本次不上报）
	OutTimeMs int64   // 输出时间（毫秒，<=0 表示本次不上报）
	TotalMs   int64   // 段级进度加权基准（<=0 表示本次不上报）
	Speed     string  // ffmpeg 倍速
}

// ApplyRenderProgress 落库渲染任务的阶段/分段/输出时间进度（06 §4.2）。
//
// 语义约束：
//   - 进度单调不回退（同一任务迟到的低进度上报被忽略）；
//   - 已收口任务（完成/取消/失败/暂停）不再被进度上报覆盖；
//   - 空值字段（stage 为空、序号<=0、时间<=0、speed 为空）保持原值，避免被清空。
//
// 返回更新后的任务快照与是否命中；未命中（任务不存在或已收口）返回 false。
// 仅在内容确实变化时落盘。
func (s *Store) ApplyRenderProgress(id string, p RenderProgress) (model.Task, bool) {
	s.mu.Lock()
	var updated model.Task
	found := false
	needPersist := false

	for i := range s.tasks {
		if s.tasks[i].ID != id {
			continue
		}
		t := &s.tasks[i]
		if t.Status.IsTerminal() || t.Status == model.StatusError || t.Status == model.StatusPaused {
			break
		}

		prog := p.Progress
		if prog < 0 {
			prog = 0
		} else if prog > 100 {
			prog = 100
		}
		if prog > t.Progress {
			t.Progress = prog
			needPersist = true
		}
		if p.Stage != "" && p.Stage != t.Stage {
			t.Stage = p.Stage
			needPersist = true
		}
		if p.SegIndex > 0 && p.SegIndex != t.SegIndex {
			t.SegIndex = p.SegIndex
			needPersist = true
		}
		if p.SegTotal > 0 && p.SegTotal != t.SegTotal {
			t.SegTotal = p.SegTotal
			needPersist = true
		}
		if p.OutTimeMs > 0 && p.OutTimeMs != t.OutTimeMs {
			t.OutTimeMs = p.OutTimeMs
			needPersist = true
		}
		if p.TotalMs > 0 && p.TotalMs != t.TotalMs {
			t.TotalMs = p.TotalMs
			needPersist = true
		}
		if p.Speed != "" && p.Speed != t.Speed {
			t.Speed = p.Speed
			needPersist = true
		}
		if needPersist {
			t.UpdatedAt = time.Now()
		}
		updated = *t
		found = true
		break
	}
	s.mu.Unlock()

	if found && needPersist {
		s.persistTasks()
	}
	return updated, found
}

// ===== 持久化 =====

func (s *Store) persistProjects() {
	// 派生字段不落盘（03 §2.2）：写盘前清零副本，读取时由 normalizeProject 重算。
	out := make([]model.Project, len(s.projects))
	copy(out, s.projects)
	for i := range out {
		out[i].ClipCount = 0
		out[i].TotalMs = 0
	}
	saveJSON(s.path("projects.json"), model.ProjectsFile{Version: 1, Projects: out})
}
func (s *Store) persistNodeCaps() {
	saveJSON(s.path("node_caps.json"), model.NodeCapsFile{Version: 1, Items: s.nodeCaps})
}
func (s *Store) persistHealthSamples() {
	saveJSON(s.path("node_health_samples.json"), model.NodeHealthFile{Version: 1, Samples: s.healthSamples})
}
func (s *Store) persistAuditLog() {
	saveJSON(s.path("audit_log.json"), model.AuditLogFile{Version: 1, Entries: s.auditLog})
}

// ===== Projects（06 §2.1，03 §2.2 / §4.2）=====

// newProjectID 生成 'p_<8hex>' 形式的项目 ID。
func newProjectID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("p_%08x", uint32(time.Now().UnixNano()))
	}
	return "p_" + hex.EncodeToString(b[:])
}

// normalizeProject 统一收口派生字段与默认值：片段速度、片段数、总时长、时间线默认参数。
func normalizeProject(p *model.Project) {
	if p.Clips == nil {
		p.Clips = []model.EDLClip{}
	}
	var total int64
	for i := range p.Clips {
		if p.Clips[i].Speed == 0 {
			p.Clips[i].Speed = 1.0 // P0 恒为 1.0（03 §2.5）
		}
		if p.Clips[i].OutMs > p.Clips[i].InMs {
			total += p.Clips[i].OutMs - p.Clips[i].InMs
		}
	}
	p.ClipCount = len(p.Clips)
	p.TotalMs = total
	if p.Timeline.FPS <= 0 {
		p.Timeline.FPS = 30
	}
	if p.Timeline.SampleRate <= 0 {
		p.Timeline.SampleRate = 48000
	}
}

// GetProjects 返回全部项目的摘要视图，按更新时间倒序。
func (s *Store) GetProjects() []model.ProjectSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ProjectSummary, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, model.ProjectSummary{
			ID:        p.ID,
			Name:      p.Name,
			Rev:       p.Rev,
			ClipCount: p.ClipCount,
			UpdatedAt: p.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (s *Store) GetProject(id string) (model.Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.projects {
		if p.ID == id {
			return p, true
		}
	}
	return model.Project{}, false
}

// FindProjectByName 精确匹配项目名称（模拟 UNIQUE(name)）。
func (s *Store) FindProjectByName(name string) (model.Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.projects {
		if p.Name == name {
			return p, true
		}
	}
	return model.Project{}, false
}

// CreateProject 新建项目，rev 从 1 开始。名称重复返回 ErrProjectNameUsed。
func (s *Store) CreateProject(p model.Project) (model.Project, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return model.Project{}, errors.New("E_EDL_INVALID: 项目名称不能为空")
	}

	s.mu.Lock()
	for _, ex := range s.projects {
		if ex.Name == name {
			s.mu.Unlock()
			return model.Project{}, ErrProjectNameUsed
		}
	}
	now := time.Now()
	if p.ID == "" {
		p.ID = newProjectID()
	}
	p.Name = name
	p.Rev = 1
	if p.SchemaVer == 0 {
		p.SchemaVer = 1
	}
	p.CreatedAt = now
	p.UpdatedAt = now
	normalizeProject(&p)
	s.projects = append(s.projects, p)
	s.mu.Unlock()

	s.persistProjects()
	logger.Info("store", "project created: %s (%s) clips=%d", p.ID, p.Name, p.ClipCount)
	return p, nil
}

// UpdateProject 乐观锁更新：expectRev <= 0 表示跳过版本校验；
// rev 不匹配返回 (当前项目, ErrRevConflict)，由 handler 转为 409。
func (s *Store) UpdateProject(id string, expectRev int, p model.Project) (model.Project, error) {
	s.mu.Lock()
	idx := -1
	for i := range s.projects {
		if s.projects[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return model.Project{}, ErrProjectNotFound
	}

	cur := s.projects[idx]
	if expectRev > 0 && cur.Rev != expectRev {
		s.mu.Unlock()
		return cur, fmt.Errorf("%w: 期望 rev=%d，当前 rev=%d", ErrRevConflict, expectRev, cur.Rev)
	}

	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = cur.Name
	}
	for i := range s.projects {
		if i != idx && s.projects[i].Name == name {
			s.mu.Unlock()
			return cur, ErrProjectNameUsed
		}
	}

	cur.Name = name
	if p.Timeline != (model.Timeline{}) {
		cur.Timeline = p.Timeline
	}
	cur.Clips = p.Clips
	if p.SchemaVer > 0 {
		cur.SchemaVer = p.SchemaVer
	}
	cur.Rev++
	cur.UpdatedAt = time.Now()
	normalizeProject(&cur)
	s.projects[idx] = cur
	s.mu.Unlock()

	s.persistProjects()
	logger.Info("store", "project updated: %s rev=%d clips=%d", cur.ID, cur.Rev, cur.ClipCount)
	return cur, nil
}

// SetProjectLastRenderTask 仅登记最近一次渲染任务，不推进 rev。
func (s *Store) SetProjectLastRenderTask(projectID, taskID string) {
	s.mu.Lock()
	hit := false
	for i := range s.projects {
		if s.projects[i].ID == projectID {
			s.projects[i].LastRenderTaskID = taskID
			hit = true
			break
		}
	}
	s.mu.Unlock()
	if hit {
		s.persistProjects()
	}
}

func (s *Store) DeleteProject(id string) bool {
	s.mu.Lock()
	for i := range s.projects {
		if s.projects[i].ID == id {
			s.projects = append(s.projects[:i], s.projects[i+1:]...)
			s.mu.Unlock()
			s.persistProjects()
			return true
		}
	}
	s.mu.Unlock()
	return false
}

// ===== NodeCaps（06 §2.1 / §5.1）=====

func (s *Store) UpsertNodeCaps(c model.NodeCaps) {
	if c.GPU == nil {
		c.GPU = []model.GPUInfo{}
	}
	if c.Encoders == nil {
		c.Encoders = []string{}
	}
	c.UpdatedAt = time.Now()

	s.mu.Lock()
	for i := range s.nodeCaps {
		if s.nodeCaps[i].ServerID == c.ServerID {
			s.nodeCaps[i] = c
			s.mu.Unlock()
			s.persistNodeCaps()
			return
		}
	}
	s.nodeCaps = append(s.nodeCaps, c)
	s.mu.Unlock()
	s.persistNodeCaps()
}

func (s *Store) GetNodeCaps(serverID string) (model.NodeCaps, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.nodeCaps {
		if c.ServerID == serverID {
			return c, true
		}
	}
	return model.NodeCaps{}, false
}

func (s *Store) GetAllNodeCaps() []model.NodeCaps {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.NodeCaps, len(s.nodeCaps))
	copy(out, s.nodeCaps)
	return out
}

func (s *Store) DeleteNodeCaps(serverID string) bool {
	s.mu.Lock()
	for i := range s.nodeCaps {
		if s.nodeCaps[i].ServerID == serverID {
			s.nodeCaps = append(s.nodeCaps[:i], s.nodeCaps[i+1:]...)
			s.mu.Unlock()
			s.persistNodeCaps()
			return true
		}
	}
	s.mu.Unlock()
	return false
}

// ===== NodeHealthSamples（06 §2.1 / §5.2）=====

// AppendNodeHealthSample 追加一次采样，并按每节点上限裁剪历史。
func (s *Store) AppendNodeHealthSample(sm model.NodeHealthSample) {
	if sm.SampledAt.IsZero() {
		sm.SampledAt = time.Now()
	}
	sm.ID = time.Now().UnixNano()

	s.mu.Lock()
	s.healthSamples = append(s.healthSamples, sm)
	s.pruneHealthSamplesLocked(healthSampleRetainPerServer)
	s.mu.Unlock()

	s.persistHealthSamples()
}

// ListNodeHealthSamples 返回指定节点自 since 起的采样，按时间倒序；limit<=0 表示不限。
func (s *Store) ListNodeHealthSamples(serverID string, since time.Time, limit int) []model.NodeHealthSample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.NodeHealthSample{}
	for _, sm := range s.healthSamples {
		if serverID != "" && sm.ServerID != serverID {
			continue
		}
		if !since.IsZero() && sm.SampledAt.Before(since) {
			continue
		}
		out = append(out, sm)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SampledAt.After(out[j].SampledAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// PruneNodeHealthSamples 将每个节点的采样裁剪到最近 perServer 条，返回删除条数。
func (s *Store) PruneNodeHealthSamples(perServer int) int {
	if perServer <= 0 {
		perServer = healthSampleRetainPerServer
	}
	s.mu.Lock()
	removed := s.pruneHealthSamplesLocked(perServer)
	s.mu.Unlock()
	if removed > 0 {
		s.persistHealthSamples()
	}
	return removed
}

// pruneHealthSamplesLocked 需持有 s.mu。
func (s *Store) pruneHealthSamplesLocked(perServer int) int {
	if len(s.healthSamples) == 0 {
		return 0
	}
	byServer := make(map[string][]model.NodeHealthSample, len(s.nodeCaps)+1)
	for _, sm := range s.healthSamples {
		byServer[sm.ServerID] = append(byServer[sm.ServerID], sm)
	}
	kept := make([]model.NodeHealthSample, 0, len(s.healthSamples))
	for _, list := range byServer {
		sort.Slice(list, func(i, j int) bool { return list[i].SampledAt.After(list[j].SampledAt) })
		if len(list) > perServer {
			list = list[:perServer]
		}
		kept = append(kept, list...)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].SampledAt.Before(kept[j].SampledAt) })
	removed := len(s.healthSamples) - len(kept)
	s.healthSamples = kept
	return removed
}

// ===== AuditLog（06 §7 / 07 §5.4）=====

// AppendAudit 写入一条审计记录（凭据、权限相关操作必须调用）。
func (s *Store) AppendAudit(e model.AuditEntry) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	if e.Result == "" {
		e.Result = "ok"
	}
	e.ID = time.Now().UnixNano()

	s.mu.Lock()
	s.auditLog = append(s.auditLog, e)
	if len(s.auditLog) > auditLogMaxEntries {
		s.auditLog = s.auditLog[len(s.auditLog)-auditLogMaxEntries:]
	}
	s.mu.Unlock()

	s.persistAuditLog()
}

// ListAudit 返回审计记录，按时间倒序；action 非空时按动作前缀过滤，limit<=0 表示不限。
func (s *Store) ListAudit(limit int, action string) []model.AuditEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.AuditEntry{}
	for _, e := range s.auditLog {
		if action != "" && !strings.HasPrefix(e.Action, action) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ===== 任务查询：幂等与冷却（06 §4.3 / §4.4）=====

// FindActiveTaskByChecksum 查找 checksum 相同且仍在调度中的任务（状态 ∈ QUEUE/RUNNING/COOLDOWN 等非终态）。
func (s *Store) FindActiveTaskByChecksum(checksum string) (model.Task, bool) {
	if checksum == "" {
		return model.Task{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t.Checksum == checksum && t.Status.IsActive() {
			return t, true
		}
	}
	return model.Task{}, false
}

// FindSuccessTaskByChecksum 查找 checksum 相同且已完成的任务（先队列后历史），
// 调用方需再校验成品文件是否存在（fileExists(t.OutputFile)）后才可复用。
func (s *Store) FindSuccessTaskByChecksum(checksum string) (model.Task, bool) {
	if checksum == "" {
		return model.Task{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t.Checksum == checksum && t.Status == model.StatusCompleted {
			return t, true
		}
	}
	for _, t := range s.history {
		if t.Checksum == checksum && t.Status == model.StatusCompleted {
			return t, true
		}
	}
	return model.Task{}, false
}

// ListCooldownDue 返回冷却到期（next_retry_at <= now）的任务，按 OrderID 升序，供 tick 转回 QUEUE。
func (s *Store) ListCooldownDue(now time.Time) []model.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Task{}
	for _, t := range s.tasks {
		if t.Status != model.StatusCooldown {
			continue
		}
		if t.CoolDownUntil == nil || !now.Before(*t.CoolDownUntil) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OrderID < out[j].OrderID })
	return out
}
