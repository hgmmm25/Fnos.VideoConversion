package store

// M4：代理映射持久化与读写（04 §4.2）。
//
// 本仓库 Store 为 JSON 文件持久化（store.go / store_edl.go 风格），
// asset_proxies 落 dataDir/asset_proxies.json（决策①），不引入 SQLite；
// 字段按 04 §4.2 的 asset_proxies 表逐列对应（asset_key → assetKey 等）。
//
// 说明：04 §3.4「校验失败不自动重试、标记 invalid、重试上限 2 次」——
// 本实现不建立任何自动重试路径（invalid 后由用户显式重新提交 /proxy），
// 因此天然不会突破 2 次上限；POST /proxy 对 invalid 状态的素材允许显式重建。

import (
	"fvcc/internal/store/model"
	"time"
)

// persistAssetProxies 把内存中的代理映射落盘（写盘为副本，避免持锁 IO 过长）。
// P0-2 提交 3：SQLite 主模式下写 asset_proxies 表；非主模式回退 JSON 文件。
func (s *Store) persistAssetProxies() {
	s.mu.RLock()
	items := make([]model.AssetProxy, len(s.assetProxies))
	copy(items, s.assetProxies)
	s.mu.RUnlock()
	s.persistKeyed("asset_proxies", keyedRows(items, func(v model.AssetProxy) string { return v.AssetKey }), func() {
		saveJSON(s.path("asset_proxies.json"), model.AssetProxiesFile{Version: 1, Items: items})
	})
}

// GetAssetProxies 返回全部代理映射（副本，供诊断/测试）。
func (s *Store) GetAssetProxies() []model.AssetProxy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.AssetProxy, len(s.assetProxies))
	copy(out, s.assetProxies)
	return out
}

// GetAssetProxy 按 assetKey（相对 SourceRoot 的 POSIX 路径）查询。
func (s *Store) GetAssetProxy(assetKey string) (model.AssetProxy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.assetProxies {
		if s.assetProxies[i].AssetKey == assetKey {
			return s.assetProxies[i], true
		}
	}
	return model.AssetProxy{}, false
}

// UpsertAssetProxy 新增或覆盖一条代理映射并落盘（以 assetKey 为主键）。
func (s *Store) UpsertAssetProxy(p model.AssetProxy) {
	s.mu.Lock()
	replaced := false
	for i := range s.assetProxies {
		if s.assetProxies[i].AssetKey == p.AssetKey {
			s.assetProxies[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		s.assetProxies = append(s.assetProxies, p)
	}
	s.mu.Unlock()
	s.persistAssetProxies()
}

// MarkAssetProxyState 仅更新状态（ready/invalid/stale），保留其余字段；命中返回 ok=true。
func (s *Store) MarkAssetProxyState(assetKey, state, taskID string) bool {
	s.mu.Lock()
	hit := false
	for i := range s.assetProxies {
		if s.assetProxies[i].AssetKey == assetKey {
			s.assetProxies[i].State = state
			if taskID != "" {
				s.assetProxies[i].TaskID = taskID
			}
			hit = true
			break
		}
	}
	s.mu.Unlock()
	if hit {
		s.persistAssetProxies()
	}
	return hit
}

// TouchAssetProxy 刷新 last_access_at（04 §3.6 容量清理维度：按最久未访问清理）。
func (s *Store) TouchAssetProxy(assetKey string, at time.Time) bool {
	s.mu.Lock()
	hit := false
	for i := range s.assetProxies {
		if s.assetProxies[i].AssetKey == assetKey {
			s.assetProxies[i].LastAccessAt = at.Format(time.RFC3339)
			hit = true
			break
		}
	}
	s.mu.Unlock()
	if hit {
		s.persistAssetProxies()
	}
	return hit
}

// DeleteAssetProxy 删除一条代理映射（仅元数据；代理文件删除由受控清理流程负责，04 §3.6）。
func (s *Store) DeleteAssetProxy(assetKey string) bool {
	s.mu.Lock()
	idx := -1
	for i := range s.assetProxies {
		if s.assetProxies[i].AssetKey == assetKey {
			idx = i
			break
		}
	}
	if idx >= 0 {
		s.assetProxies = append(s.assetProxies[:idx], s.assetProxies[idx+1:]...)
	}
	s.mu.Unlock()
	if idx < 0 {
		return false
	}
	s.persistAssetProxies()
	return true
}

// FindActiveProxyTask 查找同一 file（assetKey）上尚未终结的 GEN_PROXY 任务（04 §3.5 同素材去重）。
// 命中时调用方应直接返回既有 taskId（HTTP 200），不得重复建任务。
func (s *Store) FindActiveProxyTask(assetKey string) (model.Task, bool) {
	if assetKey == "" {
		return model.Task{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.tasks {
		t := s.tasks[i]
		if t.TaskType != model.TaskTypeGenProxy || t.SourceFile != assetKey {
			continue
		}
		if t.Status.IsTerminal() || t.Status == model.StatusError || t.Status == model.StatusCooldown {
			continue
		}
		return t, true
	}
	return model.Task{}, false
}
