package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"fvcc/smbshare"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/logger"
)

// newTraceID 生成任务链路追踪 ID（P2-1）：时间戳 + 4 字节随机，跨端日志聚合键。
func newTraceID(now time.Time) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("tr_%d", now.UnixNano())
	}
	return fmt.Sprintf("tr_%d_%x", now.UnixNano(), b)
}

// MediaProber 元数据探测通道：*FFprobe 满足，单测可注入桩以脱离 ffprobe。
// 方法集覆盖调度侧 ProxyProber（子集），故同一实例可直接注入两者。
type MediaProber interface {
	Probe(path string) (VideoInfo, error)
	Available() bool
}

// Handlers 持有所有依赖的处理器组。
type Handlers struct {
	store     *Store
	probe     MediaProber
	pv        *PathValidator
	remote    *RemoteClient
	hub       *Hub
	scheduler *Scheduler

	// ===== M4：预览网关运行时状态（04 §2）=====
	// 以下三项按需惰性初始化（见 stream.go 的 getter），
	// 使既有 &Handlers{...} 构造点（main.go / 单测）无需改动。
	ticketsMu   sync.Mutex
	tickets     *ticketStore // 预览票据（内存 LRU，进程重启即失效）
	streamsMu   sync.Mutex
	streams     *streamLimiter // 并发限制（单文件 4 / 全局 64）
	thumbsMu    sync.Mutex
	thumbs      *thumbCache // 缩略图缓存 + single-flight
	thumbFFmpeg string      // 缩略图抽帧用的 ffmpeg 路径（空则运行时探测，单测可注入）
}

// ===== 通用 =====

func (h *Handlers) info(c *gin.Context) {
	user := getGatewayUser(c)
	c.JSON(200, gin.H{
		"app":         "fvcc",
		"version":     appVer,
		"runtime":     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		"serverTime":  time.Now().Format(time.RFC3339),
		"gatewayUser": user,
		"ffprobe":     h.probe.Available(),
		"accessPaths": h.pv.AccessPaths(),
	})
}

// ===== 设置 =====

func (h *Handlers) getSettings(c *gin.Context) {
	settings := h.store.GetSettings()
	// 凭据安全（P0-1）：凭据不回显明文
	settings.SMBPassword = MaskSecret(settings.SMBPassword)
	// merge authorized paths from PathValidator so frontend can use them as default scan roots
	authorized := h.pv.AccessPaths()
	for _, p := range authorized {
		found := false
		for _, s := range settings.AccessiblePaths {
			if s == p {
				found = true
				break
			}
		}
		if !found {
			settings.AccessiblePaths = append(settings.AccessiblePaths, p)
		}
	}
	c.JSON(200, gin.H{
		"settings":        settings,
		"authorizedPaths": authorized,
	})
}

func (h *Handlers) saveSettings(c *gin.Context) {
	var s Settings
	if err := c.ShouldBindJSON(&s); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if s.SchedulerIntervalSec < 1 {
		s.SchedulerIntervalSec = 1
	}
	if s.ChunkSizeMB < 1 {
		s.ChunkSizeMB = 4
	}
	if s.MaxRetry < 0 {
		s.MaxRetry = 3
	}
	if s.HistoryLimit < 100 {
		s.HistoryLimit = 1000
	}
	if s.LogLevel == "" {
		s.LogLevel = "INFO"
	}
	// B-04：videoRoot/exportRoot 不在设置页表单内，为空时沿用既有值，避免被覆盖成空
	old := h.store.GetSettings()
	// P0-1：SMBPassword 为空或掩码时保留旧值（前端不回显明文，密码框留空表示不修改）
	if s.SMBPassword == "" || s.SMBPassword == secretMaskValue {
		s.SMBPassword = old.SMBPassword
	}
	if strings.TrimSpace(s.VideoRoot) == "" || strings.TrimSpace(s.ExportRoot) == "" {
		if strings.TrimSpace(s.VideoRoot) == "" {
			s.VideoRoot = old.VideoRoot
		}
		if strings.TrimSpace(s.ExportRoot) == "" {
			s.ExportRoot = old.ExportRoot
		}
	}
	// 2026-09-16 修复⑤：playerMuted 未提交（旧前端/旧设置文件）时沿用既有值，缺省按 true
	if s.PlayerMuted == nil {
		if old.PlayerMuted != nil {
			s.PlayerMuted = old.PlayerMuted
		} else {
			s.PlayerMuted = boolPtr(true)
		}
	}
	// ProxyRoot/CacheRoot 同样不在表单内，为空时沿用既有值（为空时后端按 videoRoot 推导，不破坏兼容）
	if strings.TrimSpace(s.ProxyRoot) == "" {
		s.ProxyRoot = old.ProxyRoot
	}
	if strings.TrimSpace(s.CacheRoot) == "" {
		s.CacheRoot = old.CacheRoot
	}
	h.store.SaveSettings(s)
	h.pv.SetExtraPaths(s.AccessiblePaths)

	logLevel := logger.INFO
	switch strings.ToUpper(s.LogLevel) {
	case "DEBUG":
		logLevel = logger.DEBUG
	case "WARN":
		logLevel = logger.WARN
	case "ERROR":
		logLevel = logger.ERROR
	case "FATAL":
		logLevel = logger.FATAL
	}
	logger.SetLogLevel(logLevel)

	resp := h.store.GetSettings()
	resp.SMBPassword = MaskSecret(resp.SMBPassword) // 凭据安全（P0-1）：凭据不回显明文
	c.JSON(200, gin.H{"settings": resp})
}

func (h *Handlers) getLog(c *gin.Context) {
	logPath := filepath.Join(h.store.dataDir, "info.log")
	info, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(200, gin.H{"size": 0, "content": "", "exists": false})
			return
		}
		fail(c, 500, err.Error())
		return
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, gin.H{"size": info.Size(), "content": string(data), "exists": true})
}

func (h *Handlers) clearLog(c *gin.Context) {
	logPath := filepath.Join(h.store.dataDir, "info.log")
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handlers) getVideoCacheInfo(c *gin.Context) {
	cachePath := filepath.Join(h.store.dataDir, "video_cache.json")
	info, err := os.Stat(cachePath)
	var size int64 = 0
	if err == nil {
		size = info.Size()
	}
	count := h.store.GetVideoCacheCount()
	c.JSON(200, gin.H{"count": count, "size": size})
}

func (h *Handlers) clearVideoCache(c *gin.Context) {
	h.store.ClearVideoCache()
	c.JSON(200, gin.H{"ok": true})
}

// ===== 视频管理 =====

// isReservedMediaName 判定网关保留名（04 §2.4 一致性约束）：`_` 前缀目录/文件
// （`_proxy`、`_wve_cache`、`_exports`）属预览网关与代理工作流的内部产物，
// 不得作为素材库内容展示——扫描时目录整棵 SkipDir、文件直接跳过。
func isReservedMediaName(name string) bool {
	return strings.HasPrefix(name, "_")
}

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".flv": true, ".wmv": true, ".ts": true, ".m4v": true,
	".webm": true, ".mpg": true, ".mpeg": true, ".3gp": true,
}

func (h *Handlers) scanDirectory(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "路径不能为空")
		return
	}

	videos, err := h.scanDirectoryOnce(req.Path)
	if err != nil {
		fail(c, 500, err.Error())
		return
	}

	c.JSON(200, gin.H{"videos": videos, "total": len(videos)})
}

func (h *Handlers) scanDirectoryStream(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		fail(c, 400, "路径不能为空")
		return
	}
	// 需求3：文件夹式阅览。recursive=false 时仅扫描当前目录（不进入子目录），
	// 子目录由前端 browseDirs(/dirs) 另行列举；默认 true 保持旧行为兼容。
	recursive := c.DefaultQuery("recursive", "true") != "false"

	progressChan := make(chan []VideoInfo, 16)
	doneChan := make(chan error, 1)

	go func() {
		err := h.doScanDirectory(path, func(videos []VideoInfo) bool {
			select {
			case progressChan <- videos:
			case <-c.Request.Context().Done():
				return true
			}
			return false
		}, recursive)
		doneChan <- err
	}()

	c.Stream(func(w io.Writer) bool {
		select {
		case videos := <-progressChan:
			data, _ := json.Marshal(gin.H{"type": "progress", "videos": videos, "count": len(videos)})
			c.SSEvent("message", string(data))
			return true
		case err := <-doneChan:
			// 排空 progressChan 中剩余的批次，防止 select 竞态导致丢数据
		drainLoop:
			for {
				select {
				case videos := <-progressChan:
					data, _ := json.Marshal(gin.H{"type": "progress", "videos": videos, "count": len(videos)})
					c.SSEvent("message", string(data))
				default:
					break drainLoop
				}
			}
			if err != nil {
				// P2-4：SSE 错误消息与统一契约对齐（type 保留，error 拆为 code+msg）
				data, _ := json.Marshal(gin.H{"type": "error", "code": "E_SCAN_FAILED", "msg": err.Error()})
				c.SSEvent("message", string(data))
			} else {
				data, _ := json.Marshal(gin.H{"type": "done", "message": "扫描完成"})
				c.SSEvent("message", string(data))
			}
			return false
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func (h *Handlers) scanDirectoryOnce(path string) ([]VideoInfo, error) {
	var videos []VideoInfo
	err := h.doScanDirectory(path, func(newVideos []VideoInfo) bool {
		videos = append(videos, newVideos...)
		return false
	}, true)
	if err != nil {
		return nil, err
	}
	sort.Slice(videos, func(i, j int) bool {
		return videos[i].FileName < videos[j].FileName
	})
	return videos, nil
}

func (h *Handlers) doScanDirectory(path string, onProgress func([]VideoInfo) bool, recursive bool) error {
	if err := h.pv.Validate(path); err != nil {
		return err
	}

	settings := h.store.GetSettings()
	if err := h.validateSMBPath(settings, path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("路径不存在: %v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("指定路径不是目录")
	}

	const batchSize = 10
	var batch []VideoInfo

	err = filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		base := filepath.Base(p)
		if fi.IsDir() {
			// 保留目录（`_` 前缀：_proxy / _wve_cache / _exports）不作为素材库内容展示（04 §2.4）
			if p != path && isReservedMediaName(base) {
				return filepath.SkipDir
			}
			// 需求3：非递归扫描仅处理当前目录，子目录交给前端 browseDirs 逐级展开
			if !recursive && p != path {
				return filepath.SkipDir
			}
			return nil
		}
		if isReservedMediaName(base) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !videoExts[ext] {
			return nil
		}

		format := strings.TrimPrefix(ext, ".")
		size := fi.Size()

		var video VideoInfo
		useCache := false

		if cache, ok := h.store.GetVideoCache(p); ok {
			if _, err := os.Stat(p); err == nil {
				video = VideoInfo{
					Path:         cache.Path,
					FileName:     cache.FileName,
					Format:       cache.Format,
					Size:         cache.Size,
					Duration:     cache.Duration,
					Resolution:   cache.Resolution,
					Width:        cache.Width,
					Height:       cache.Height,
					Codec:        cache.Codec,
					Bitrate:      cache.Bitrate,
					Fps:          cache.Fps,
					AudioCodec:   cache.AudioCodec,
					AudioBitrate: cache.AudioBitrate,
					SampleRate:   cache.SampleRate,
					Channels:     cache.Channels,
					StreamCount:  cache.StreamCount,
					Probed:       cache.Probed,
					Streams:      cache.Streams,
				}
				if !cache.Probed {
					useCache = false
				} else {
					useCache = true
				}
			} else {
				h.store.DeleteVideoCache(p)
			}
		}

		if !useCache {
			probeInfo, err := h.probe.Probe(p)
			if err != nil {
				logger.Warn("scan", "ffprobe failed for %s: %v", p, err)
				video = VideoInfo{
					Path:     p,
					FileName: base,
					Format:   format,
					Size:     size,
					Probed:   false,
				}
			} else {
				h.store.UpsertVideoCache(VideoInfoCache{
					Path:         probeInfo.Path,
					FileName:     probeInfo.FileName,
					Format:       probeInfo.Format,
					Size:         probeInfo.Size,
					Duration:     probeInfo.Duration,
					Resolution:   probeInfo.Resolution,
					Width:        probeInfo.Width,
					Height:       probeInfo.Height,
					Codec:        probeInfo.Codec,
					Bitrate:      probeInfo.Bitrate,
					Fps:          probeInfo.Fps,
					AudioCodec:   probeInfo.AudioCodec,
					AudioBitrate: probeInfo.AudioBitrate,
					SampleRate:   probeInfo.SampleRate,
					Channels:     probeInfo.Channels,
					StreamCount:  probeInfo.StreamCount,
					Probed:       probeInfo.Probed,
					Streams:      probeInfo.Streams,
				})
				video = probeInfo
			}
		}

		batch = append(batch, video)
		if len(batch) >= batchSize {
			if onProgress(batch) {
				return filepath.SkipDir
			}
			batch = nil
		}

		return nil
	})

	if len(batch) > 0 {
		onProgress(batch)
	}

	return err
}

func (h *Handlers) probeVideo(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "路径不能为空")
		return
	}

	if err := h.pv.Validate(req.Path); err != nil {
		fail(c, 403, err.Error())
		return
	}

	// SMB 模式：额外校验路径是否在已共享目录内
	settings := h.store.GetSettings()
	if err := h.validateSMBPath(settings, req.Path); err != nil {
		if errors.Is(err, errSMBNotShared) {
			fail(c, 403, "该文件未通过SMB共享，SMB模式下不可访问")
		} else {
			fail(c, 500, err.Error())
		}
		return
	}

	info, err := h.probe.Probe(req.Path)
	if err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, info)
}

// ===== 文件操作 =====

// previewVideo 提供视频文件预览（流式播放）
func (h *Handlers) previewVideo(c *gin.Context) {
	path := c.Param("path")
	decodedPath, err := url.QueryUnescape(path)
	if err != nil {
		fail(c, 400, "路径解析失败")
		return
	}
	if err := h.pv.Validate(decodedPath); err != nil {
		fail(c, 403, err.Error())
		return
	}
	if _, err := os.Stat(decodedPath); os.IsNotExist(err) {
		fail(c, 404, "文件不存在")
		return
	}
	c.File(decodedPath)
}

// renameVideo 重命名视频文件
func (h *Handlers) renameVideo(c *gin.Context) {
	var req struct {
		Path    string `json:"path" binding:"required"`
		NewName string `json:"newName" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "参数错误")
		return
	}
	if err := h.pv.Validate(req.Path); err != nil {
		fail(c, 403, err.Error())
		return
	}
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		fail(c, 404, "文件不存在")
		return
	}
	if req.NewName == "" {
		fail(c, 400, "新文件名不能为空")
		return
	}
	dir := filepath.Dir(req.Path)
	newPath := filepath.Join(dir, req.NewName)
	if err := os.Rename(req.Path, newPath); err != nil {
		fail(c, 500, "重命名失败: "+err.Error())
		return
	}
	h.store.DeleteVideoCache(req.Path)
	c.JSON(200, gin.H{"ok": true, "newPath": newPath})
}

// moveVideo 移动视频文件到指定目录
func (h *Handlers) moveVideo(c *gin.Context) {
	var req struct {
		Path    string `json:"path" binding:"required"`
		DestDir string `json:"destDir" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "参数错误")
		return
	}
	if err := h.pv.Validate(req.Path); err != nil {
		fail(c, 403, err.Error())
		return
	}
	if err := h.pv.Validate(req.DestDir); err != nil {
		fail(c, 403, "目标目录未授权")
		return
	}
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		fail(c, 404, "文件不存在")
		return
	}
	if _, err := os.Stat(req.DestDir); os.IsNotExist(err) {
		fail(c, 404, "目标目录不存在")
		return
	}
	fileName := filepath.Base(req.Path)
	newPath := filepath.Join(req.DestDir, fileName)
	if err := os.Rename(req.Path, newPath); err != nil {
		fail(c, 500, "移动失败: "+err.Error())
		return
	}
	h.store.DeleteVideoCache(req.Path)
	c.JSON(200, gin.H{"ok": true, "newPath": newPath})
}

// deleteVideo 删除视频文件
func (h *Handlers) deleteVideo(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "路径不能为空")
		return
	}
	if err := h.pv.Validate(req.Path); err != nil {
		fail(c, 403, err.Error())
		return
	}
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		fail(c, 404, "文件不存在")
		return
	}
	// P2-5：删除改为移入回收站（<授权根>/_trash），不再物理删除
	trashPath, err := h.moveToTrash(req.Path)
	if err != nil {
		fail(c, 500, "删除失败: "+err.Error())
		return
	}
	h.store.DeleteVideoCache(req.Path)
	c.JSON(200, gin.H{"ok": true, "trashPath": trashPath})
}

// ===== 目录浏览 =====

// browseEntry 目录浏览条目
type browseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
	// Default 仅授权根列表使用（修复①）：标记该根为「缺省素材根」——
	// 其代理产物位于全局代理根（Settings.proxyRoot，缺省 <素材根>/_proxy），
	// 其他授权根的代理产物位于 <该根>/_proxy，前端据此选择预览请求的 root 参数。
	Default bool `json:"default,omitempty"`
}

// browseDirs 列出指定目录下的子目录和视频文件。
// path 为空时返回授权根目录列表（仅 dirs，无 parent/files）。
// SMB 模式下，path 为空时返回已共享的目录列表，有 path 时校验是否在共享目录内。
func (h *Handlers) browseDirs(c *gin.Context) {
	pathParam := c.Query("path")
	settings := h.store.GetSettings()
	isSMB := settings.TransferMode == "smb" && settings.SMBUser != ""

	// 无 path：返回授权根目录（SMB 模式下过滤为已共享目录）
	if pathParam == "" {
		roots := h.pv.AccessPaths()
		defLocal := h.resolveMediaRoots().SourceLocal
		dirs := make([]browseEntry, 0, len(roots))
		for _, p := range roots {
			if isSMB {
				// SMB 模式：只返回可通过SMB访问的授权目录
				if ok, err := smbshare.IsPathShared(settings.SMBUser, p); err != nil || !ok {
					continue
				}
			}
			dirs = append(dirs, browseEntry{
				Name:    filepath.Base(p),
				Path:    p,
				Default: defLocal != "" && samePath(p, defLocal),
			})
		}
		c.JSON(200, gin.H{
			"authorized": len(roots) > 0,
			"current":    "",
			"dirs":       dirs,
		})
		return
	}

	// 路径安全校验
	if err := h.pv.Validate(pathParam); err != nil {
		fail(c, 403, err.Error())
		return
	}

	// SMB 模式：额外校验路径是否在已共享目录内
	if err := h.validateSMBPath(settings, pathParam); err != nil {
		if errors.Is(err, errSMBNotShared) {
			fail(c, 403, "该目录未通过SMB共享，SMB模式下不可访问")
		} else {
			fail(c, 500, err.Error())
		}
		return
	}

	info, err := os.Stat(pathParam)
	if err != nil {
		fail(c, 404, fmt.Sprintf("路径不存在: %v", err))
		return
	}
	if !info.IsDir() {
		fail(c, 400, "指定路径不是目录")
		return
	}

	entries, err := os.ReadDir(pathParam)
	if err != nil {
		fail(c, 500, fmt.Sprintf("读取目录失败: %v", err))
		return
	}

	var dirs []browseEntry
	var files []browseEntry
	dirs = make([]browseEntry, 0)
	files = make([]browseEntry, 0)
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(pathParam, name)
		if e.IsDir() {
			dirs = append(dirs, browseEntry{Name: name, Path: full})
		} else if videoExts[strings.ToLower(filepath.Ext(name))] {
			fi, err := e.Info()
			sz := int64(0)
			if err == nil {
				sz = fi.Size()
			}
			files = append(files, browseEntry{Name: name, Path: full, Size: sz})
		}
	}

	// 计算上级目录（仅在授权目录内才返回 parent）
	parent := ""
	for _, root := range h.pv.AccessPaths() {
		if pathParam == root {
			// 当前是授权根，parent 为空（回到授权根列表）
			parent = ""
			break
		}
		if strings.HasPrefix(pathParam, root+string(filepath.Separator)) {
			parent = filepath.Dir(pathParam)
			break
		}
	}

	c.JSON(200, gin.H{
		"current": pathParam,
		"parent":  parent,
		"dirs":    dirs,
		"files":   files,
	})
}

// ===== 服务器管理 =====

func (h *Handlers) listServers(c *gin.Context) {
	servers := h.store.GetServers()
	// 凭据安全（P0-1）：凭据不回显明文
	out := make([]Server, len(servers))
	copy(out, servers)
	for i := range out {
		out[i].AuthKey = MaskSecret(out[i].AuthKey)
	}
	c.JSON(200, gin.H{"servers": out})
}

func (h *Handlers) createServer(c *gin.Context) {
	var sv Server
	if err := c.ShouldBindJSON(&sv); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if sv.ID == "" {
		sv.ID = "server-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if sv.LockExpireSec == 0 {
		sv.LockExpireSec = 120
	}
	if sv.Status == "" {
		sv.Status = "offline"
	}
	if sv.AuthKey == secretMaskValue {
		sv.AuthKey = ""
	}
	h.store.UpsertServer(sv)
	sv.AuthKey = MaskSecret(sv.AuthKey)
	c.JSON(200, sv)
}

func (h *Handlers) updateServer(c *gin.Context) {
	id := c.Param("id")
	sv, ok := h.store.GetServer(id)
	if !ok {
		fail(c, 404, "服务器不存在")
		return
	}
	oldKey := sv.AuthKey // 凭据安全（P0-1）：保存旧密钥，掩码/空提交时保留
	if err := c.ShouldBindJSON(&sv); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	sv.ID = id
	if sv.AuthKey == "" || sv.AuthKey == secretMaskValue {
		sv.AuthKey = oldKey
	}
	h.store.UpsertServer(sv)
	sv.AuthKey = MaskSecret(sv.AuthKey)
	c.JSON(200, sv)
}

func (h *Handlers) deleteServer(c *gin.Context) {
	id := c.Param("id")
	if !h.store.DeleteServer(id) {
		fail(c, 404, "服务器不存在")
		return
	}
	h.remote.CloseConn(id)
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handlers) testServer(c *gin.Context) {
	id := c.Param("id")
	sv, ok := h.store.GetServer(id)
	if !ok {
		fail(c, 404, "服务器不存在")
		return
	}

	// 尝试连接
	h.remote.CloseConn(id)
	_, _, err := h.remote.Connect(sv)
	if err != nil {
		h.store.UpdateServerStatus(id, "offline")
		h.hub.BroadcastNodeStatus(id, "offline", healthScoreUnknown, "连通性测试失败")
		failWithCode(c, 200, "E_SERVER_OFFLINE", err.Error())
		return
	}
	h.store.UpdateServerStatus(id, "online")
	h.hub.BroadcastNodeStatus(id, "online", healthScoreUnknown, "连通性测试通过")
	c.JSON(200, gin.H{"ok": true, "status": "online"})
}

// ===== 转码方案管理 =====

func (h *Handlers) listProfiles(c *gin.Context) {
	c.JSON(200, gin.H{"profiles": h.store.GetProfiles()})
}

func (h *Handlers) createProfile(c *gin.Context) {
	var p Profile
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if p.ID == "" {
		p.ID = "profile-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	h.store.UpsertProfile(p)
	c.JSON(200, p)
}

func (h *Handlers) updateProfile(c *gin.Context) {
	id := c.Param("id")
	p, ok := h.store.GetProfile(id)
	if !ok {
		fail(c, 404, "方案不存在")
		return
	}
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	// P1-1: 59 块手写字段映射收敛为反射白名单 helper（applyjson.go），
	// 白名单由 Profile 的 json tag 自动推导，保持部分更新语义。
	delete(updates, "id") // 主键不可通过 update 修改（与旧行为一致）
	applyJSONUpdates(&p, updates)
	h.store.UpsertProfile(p)
	c.JSON(200, p)
}

func (h *Handlers) deleteProfile(c *gin.Context) {
	id := c.Param("id")
	if !h.store.DeleteProfile(id) {
		fail(c, 404, "方案不存在")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ===== 任务管理 =====

func (h *Handlers) listTasks(c *gin.Context) {
	tasks := h.store.GetTasks()
	c.JSON(200, gin.H{"tasks": tasks, "total": len(tasks)})
}

func (h *Handlers) createTask(c *gin.Context) {
	var req struct {
		SourceFile string `json:"sourceFile" binding:"required"`
		OutputFile string `json:"outputFile" binding:"required"`
		ServerID   string `json:"serverId" binding:"required"`
		ProfileID  string `json:"profileId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}

	// 路径安全校验（所有模式都需校验授权目录）
	settings := h.store.GetSettings()
	if err := h.pv.Validate(req.SourceFile); err != nil {
		fail(c, 403, "源文件: "+err.Error())
		return
	}
	if err := h.pv.Validate(req.OutputFile); err != nil {
		fail(c, 403, "输出文件: "+err.Error())
		return
	}

	// 检查输出文件是否已存在
	if _, err := os.Stat(req.OutputFile); err == nil {
		fail(c, 409, "输出文件已存在: "+req.OutputFile)
		return
	}

	// 检查是否有其他非终态任务正在转码同一输出文件
	for _, t := range h.store.GetTasks() {
		if t.OutputFile == req.OutputFile && !t.Status.IsTerminal() {
			fail(c, 409, "已有任务正在转码同一输出文件: "+req.OutputFile)
			return
		}
	}

	// SMB模式：额外校验路径是否在已共享目录内
	if err := h.validateSMBPath(settings, req.SourceFile); err != nil {
		if errors.Is(err, errSMBNotShared) {
			fail(c, 403, "源文件不在SMB共享目录内，无法通过SMB模式访问")
		} else {
			fail(c, 500, err.Error())
		}
		return
	}
	if err := h.validateSMBPath(settings, req.OutputFile); err != nil {
		if errors.Is(err, errSMBNotShared) {
			fail(c, 403, "输出文件不在SMB共享目录内，无法通过SMB模式访问")
		} else {
			fail(c, 500, err.Error())
		}
		return
	}

	var server Server
	if req.ServerID == "_local_" {
		server = Server{
			ID:      "_local_",
			Name:    "fnNAS 自转码",
			IP:      "",
			Port:    0,
			Status:  "online",
			IsLocal: true,
		}
	} else {
		var ok bool
		server, ok = h.store.GetServer(req.ServerID)
		if !ok {
			fail(c, 404, "服务器不存在")
			return
		}
		if !server.IsLocal && server.Status == "offline" {
			logger.Warn("task", "createTask: server offline, adding task as paused: server=%s, task=%s", server.Name, req.SourceFile)
		}
	}
	profile, ok := h.store.GetProfile(req.ProfileID)
	if !ok {
		fail(c, 404, "转码方案不存在")
		return
	}

	// 获取输出文件格式
	outputFormat := strings.ToLower(filepath.Ext(req.OutputFile))
	if outputFormat != "" && outputFormat[0] == '.' {
		outputFormat = outputFormat[1:]
	}

	// 从视频缓存中获取音频码率（如果有）
	var audioBitrate string
	if cache, ok := h.store.GetVideoCache(req.SourceFile); ok {
		audioBitrate = cache.AudioBitrate
	}

	// 生成 FFmpeg 参数
	ffmpegArgs := buildFFmpegArgs(profile, outputFormat, audioBitrate)

	status := StatusQueue
	if !server.IsLocal && server.Status == "offline" {
		status = StatusPaused
	}

	now := time.Now()
	task := Task{
		ID:          "task_" + fmt.Sprintf("%d", now.UnixNano()),
		OrderID:     h.store.NextOrderID(),
		SourceFile:  req.SourceFile,
		OutputFile:  req.OutputFile,
		FileName:    filepath.Base(req.SourceFile),
		OutputName:  filepath.Base(req.OutputFile),
		ServerID:    req.ServerID,
		ServerName:  server.Name,
		ProfileID:   req.ProfileID,
		ProfileName: profile.Name,
		FFmpegArgs:  ffmpegArgs,
		Status:      status,
		Progress:    0,
		RetryType:   Retryable,
		TraceID:     newTraceID(now),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	h.store.UpsertTask(task)
	h.hub.BroadcastTaskUpdate(task.ID, string(status), 0, "任务已创建")
	logger.Info("task", "created: id=%s file=%s server=%s profile=%s status=%s",
		task.ID, task.FileName, task.ServerName, task.ProfileName, task.Status)
	c.JSON(200, task)
}

func (h *Handlers) pauseTask(c *gin.Context) {
	id := c.Param("id")
	t, ok := h.store.GetTask(id)
	if !ok {
		fail(c, 404, "任务不存在")
		return
	}
	if t.Status.IsTerminal() {
		fail(c, 400, "终态任务无法暂停")
		return
	}

	if h.scheduler != nil {
		if cancel, ok := h.scheduler.cancelMap.LoadAndDelete(id); ok {
			cancel.(context.CancelFunc)()
		}
	}

	server, _ := h.store.GetServer(t.ServerID)
	if server.ID != "" {
		h.store.ReleaseTransLock(server.ID, id)
		h.store.ReleaseCodeLock(server.ID, id)
		if server.IsLocal && t.Status == StatusTranscoding {
			StopLocalTranscode(id)
		} else if t.RemoteTaskID != "" && h.remote != nil {
			if err := h.remote.PauseTask(server, t.RemoteTaskID); err != nil {
				logger.Warn("pause", "Failed to pause remote task %s: %v", t.RemoteTaskID, err)
			}
		}
	}

	t.Status = StatusPaused
	t.ErrorMsg = "用户暂停"
	h.store.UpsertTask(t)
	h.hub.BroadcastTaskUpdate(id, string(StatusPaused), t.Progress, "已暂停")
	logger.Info("task", "paused: id=%s file=%s", t.ID, t.FileName)
	c.JSON(200, t)
}

func (h *Handlers) resumeTask(c *gin.Context) {
	id := c.Param("id")
	t, ok := h.store.GetTask(id)
	if !ok {
		fail(c, 404, "任务不存在")
		return
	}
	if t.Status != StatusPaused && t.Status != StatusError {
		fail(c, 400, "仅暂停/错误状态可恢复")
		return
	}

	server, _ := h.store.GetServer(t.ServerID)
	if server.ID != "" && t.RemoteTaskID != "" && h.remote != nil {
		if err := h.remote.ResumeTask(server, t.RemoteTaskID); err != nil {
			logger.Warn("resume", "Failed to resume remote task %s: %v", t.RemoteTaskID, err)
		}
	}

	t.Status = StatusQueue
	t.CoolDownUntil = nil
	t.ErrorMsg = ""
	h.store.UpsertTask(t)
	h.hub.BroadcastTaskUpdate(id, string(StatusQueue), t.Progress, "已恢复")
	logger.Info("task", "resumed: id=%s file=%s", t.ID, t.FileName)
	c.JSON(200, t)
}

func (h *Handlers) cancelTask(c *gin.Context) {
	id := c.Param("id")
	t, ok := h.store.GetTask(id)
	if !ok {
		fail(c, 404, "任务不存在")
		return
	}
	if t.Status.IsTerminal() {
		fail(c, 400, "终态任务无法取消")
		return
	}

	if h.scheduler != nil {
		if cancel, ok := h.scheduler.cancelMap.LoadAndDelete(id); ok {
			cancel.(context.CancelFunc)()
		}
	}

	server, _ := h.store.GetServer(t.ServerID)
	if server.ID != "" {
		h.store.ReleaseTransLock(server.ID, id)
		h.store.ReleaseCodeLock(server.ID, id)
		if server.IsLocal && t.Status == StatusTranscoding {
			StopLocalTranscode(id)
		} else if t.RemoteTaskID != "" {
			// 同步调用 CancelTask，确保 FVCS 收到取消指令后再返回
			if err := h.remote.CancelTask(server, t.RemoteTaskID); err != nil {
				logger.Warn("cancel", "Failed to cancel remote task %s: %v", t.RemoteTaskID, err)
			}
		}
	}

	t.Status = StatusCancelled
	t.ErrorMsg = "用户取消"
	h.store.UpsertTask(t)
	h.hub.BroadcastTaskUpdate(id, string(StatusCancelled), 0, "已取消")
	logger.Info("task", "cancelled: id=%s file=%s", t.ID, t.FileName)

	if h.scheduler != nil {
		h.scheduler.cleanupIncompleteFiles(t)
	}

	c.JSON(200, t)
}

func (h *Handlers) retryTask(c *gin.Context) {
	id := c.Param("id")
	t, ok := h.store.GetTask(id)
	if !ok {
		fail(c, 404, "任务不存在")
		return
	}
	if t.Status != StatusError {
		fail(c, 400, "仅错误状态可重试")
		return
	}

	t.Status = StatusQueue
	t.RetryCount = 0
	t.CoolDownUntil = nil
	t.ErrorMsg = ""
	h.store.UpsertTask(t)
	h.hub.BroadcastTaskUpdate(id, string(StatusQueue), t.Progress, "手动重试")
	logger.Info("task", "retried: id=%s file=%s", t.ID, t.FileName)
	c.JSON(200, t)
}

func (h *Handlers) deleteTask(c *gin.Context) {
	id := c.Param("id")
	t, _ := h.store.GetTask(id)
	if !h.store.DeleteTask(id) {
		fail(c, 404, "任务不存在")
		return
	}
	logger.Info("task", "deleted: id=%s file=%s", id, t.FileName)
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handlers) reorderTasks(c *gin.Context) {
	var req struct {
		TaskIDs []string `json:"taskIds" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	if err := h.store.ReorderTasks(req.TaskIDs); err != nil {
		fail(c, 500, err.Error())
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ===== 历史记录 =====

func (h *Handlers) listHistory(c *gin.Context) {
	history := h.store.GetHistory()
	// 倒序排列（最新的在前）
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}
	c.JSON(200, gin.H{"tasks": history, "total": len(history)})
}

func (h *Handlers) deleteHistory(c *gin.Context) {
	id := c.Param("id")
	if !h.store.DeleteHistoryTask(id) {
		fail(c, 404, "历史记录不存在")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ===== 监控指标 =====

// metrics 返回聚合统计（JSON）。P2-1 可观测性扩展：
// 在原有 totalTasks/totalHistory/totalServers/onlineServers/byStatus 基础上，
// 增加 activeTasks/queuedTasks/failedTasks/completedTotal/nodeCapsCount/offlineServers，
// 前端 Metrics 类型已同步（ui-src/src/types.ts），Prometheus 采集走 /metrics/prometheus。
func (h *Handlers) metrics(c *gin.Context) {
	tasks := h.store.GetTasks()
	history := h.store.GetHistory()
	servers := h.store.GetServers()

	byStatus := map[string]int{}
	activeTasks := 0
	queuedTasks := 0
	failedTasks := 0
	for _, t := range tasks {
		byStatus[string(t.Status)]++
		if !t.Status.IsTerminal() {
			activeTasks++
		}
		if t.Status == StatusQueue {
			queuedTasks++
		}
		if t.Status == StatusError {
			failedTasks++
		}
	}
	completedTotal := 0
	for _, t := range history {
		if t.Status == StatusCompleted {
			completedTotal++
		}
		if t.Status == StatusError {
			failedTasks++
		}
	}

	// 服务器在线数
	onlineServers := 0
	for _, sv := range servers {
		if sv.Status == "online" {
			onlineServers++
		}
	}

	nodeCapsCount := 0
	if h.store != nil {
		nodeCapsCount = len(h.store.GetAllNodeCaps())
	}

	// P2-1：耗时直方图（秒）与累计失败率，基于历史任务 StartedAt→FinishedAt。
	durBuckets := []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600}
	durHist := make(map[string]int)
	var durSum float64
	var durCount int
	doneCount, failCount := 0, 0
	for _, t := range history {
		if t.StartedAt == nil || t.FinishedAt == nil {
			continue
		}
		if t.Status == StatusCompleted {
			doneCount++
		} else if t.Status == StatusError {
			failCount++
		}
		secs := t.FinishedAt.Sub(*t.StartedAt).Seconds()
		if secs < 0 {
			continue
		}
		durSum += secs
		durCount++
		for _, b := range durBuckets {
			if secs <= b {
				durHist[fmt.Sprintf("%.0f", b)]++
			}
		}
		durHist["+Inf"]++
	}
	failureRate := 0.0
	if doneCount+failCount > 0 {
		failureRate = float64(failCount) / float64(doneCount+failCount)
	}
	avgDurationSec := 0.0
	if durCount > 0 {
		avgDurationSec = durSum / float64(durCount)
	}

	c.JSON(200, gin.H{
		"totalTasks":        len(tasks),
		"totalHistory":      len(history),
		"totalServers":      len(servers),
		"onlineServers":     onlineServers,
		"offlineServers":    len(servers) - onlineServers,
		"byStatus":          byStatus,
		"activeTasks":       activeTasks,
		"queuedTasks":       queuedTasks,
		"failedTasks":       failedTasks,
		"completedTotal":    completedTotal,
		"nodeCapsCount":     nodeCapsCount,
		"durationHistogram": durHist,
		"avgDurationSec":    avgDurationSec,
		"failureRate":       failureRate,
	})
}

// metricsPrometheus 输出 Prometheus 文本格式指标（P2-1 可观测性）：
// 任务数（按状态）、活跃任务、队列深度、失败任务、历史完成数、节点在线/离线/能力快照数。
// 手写文本格式，不引入 client_golang 依赖（保持 FVCC 无 CGO 交叉编译链路纯净）。
func (h *Handlers) metricsPrometheus(c *gin.Context) {
	tasks := h.store.GetTasks()
	history := h.store.GetHistory()
	servers := h.store.GetServers()

	byStatus := map[string]int{}
	activeTasks := 0
	for _, t := range tasks {
		byStatus[string(t.Status)]++
		if !t.Status.IsTerminal() {
			activeTasks++
		}
	}
	completedTotal, failedTasks := 0, 0
	for _, t := range history {
		if t.Status == StatusCompleted {
			completedTotal++
		}
		if t.Status == StatusError {
			failedTasks++
		}
	}
	for _, t := range tasks {
		if t.Status == StatusError {
			failedTasks++
		}
	}
	onlineServers := 0
	for _, sv := range servers {
		if sv.Status == "online" {
			onlineServers++
		}
	}
	nodeCapsCount := 0
	if h.store != nil {
		nodeCapsCount = len(h.store.GetAllNodeCaps())
	}

	var b strings.Builder
	b.WriteString("# HELP fvcc_tasks_total 当前任务数（按状态分桶）\n")
	b.WriteString("# TYPE fvcc_tasks_total gauge\n")
	for _, st := range []TaskStatus{StatusQueue, StatusTranscoding, StatusCompleted, StatusError, StatusPaused, StatusCancelled, StatusCooldown} {
		b.WriteString(fmt.Sprintf("fvcc_tasks_total{status=%q} %d\n", st.WireName(), byStatus[string(st)]))
	}
	b.WriteString("# HELP fvcc_tasks_active 非终态活跃任务数\n")
	b.WriteString("# TYPE fvcc_tasks_active gauge\n")
	b.WriteString(fmt.Sprintf("fvcc_tasks_active %d\n", activeTasks))
	b.WriteString("# HELP fvcc_tasks_failed 失败任务数（活跃失败 + 历史失败）\n")
	b.WriteString("# TYPE fvcc_tasks_failed gauge\n")
	b.WriteString(fmt.Sprintf("fvcc_tasks_failed %d\n", failedTasks))
	b.WriteString("# HELP fvcc_tasks_completed_total 历史累计完成数\n")
	b.WriteString("# TYPE fvcc_tasks_completed_total counter\n")
	b.WriteString(fmt.Sprintf("fvcc_tasks_completed_total %d\n", completedTotal))
	b.WriteString("# HELP fvcc_servers_total 渲染节点数（按在线状态分桶）\n")
	b.WriteString("# TYPE fvcc_servers_total gauge\n")
	b.WriteString(fmt.Sprintf("fvcc_servers_total{status=\"online\"} %d\n", onlineServers))
	b.WriteString(fmt.Sprintf("fvcc_servers_total{status=\"offline\"} %d\n", len(servers)-onlineServers))
	b.WriteString("# HELP fvcc_node_caps_count 节点能力快照数\n")
	b.WriteString("# TYPE fvcc_node_caps_count gauge\n")
	b.WriteString(fmt.Sprintf("fvcc_node_caps_count %d\n", nodeCapsCount))

	// P2-1：耗时直方图与累计失败率（基于历史任务 StartedAt→FinishedAt）。
	b.WriteString("# HELP fvcc_task_duration_seconds 任务执行耗时直方图（StartedAt→FinishedAt）\n")
	b.WriteString("# TYPE fvcc_task_duration_seconds histogram\n")
	durBuckets := []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600}
	durCounts := make(map[float64]int)
	var durSum float64
	var durCount int
	doneCount, failCount := 0, 0
	for _, t := range history {
		if t.StartedAt == nil || t.FinishedAt == nil {
			continue
		}
		if t.Status == StatusCompleted {
			doneCount++
		} else if t.Status == StatusError {
			failCount++
		}
		secs := t.FinishedAt.Sub(*t.StartedAt).Seconds()
		if secs < 0 {
			continue
		}
		durSum += secs
		durCount++
		for _, b := range durBuckets {
			if secs <= b {
				durCounts[b]++
			}
		}
	}
	for _, bd := range durBuckets {
		b.WriteString(fmt.Sprintf("fvcc_task_duration_seconds_bucket{le=%q} %d\n", fmt.Sprintf("%.0f", bd), durCounts[bd]))
	}
	b.WriteString(fmt.Sprintf("fvcc_task_duration_seconds_bucket{le=\"+Inf\"} %d\n", durCount))
	b.WriteString(fmt.Sprintf("fvcc_task_duration_seconds_sum %v\n", durSum))
	b.WriteString(fmt.Sprintf("fvcc_task_duration_seconds_count %d\n", durCount))
	failureRate := 0.0
	if doneCount+failCount > 0 {
		failureRate = float64(failCount) / float64(doneCount+failCount)
	}
	b.WriteString("# HELP fvcc_task_failure_rate 历史累计失败率（失败/终态）\n")
	b.WriteString("# TYPE fvcc_task_failure_rate gauge\n")
	b.WriteString(fmt.Sprintf("fvcc_task_failure_rate %v\n", failureRate))

	c.Data(200, "text/plain; version=0.0.4; charset=utf-8", []byte(b.String()))
}

// buildFFmpegArgs 将转码方案转换为 FFmpeg 命令行参数。
// isNVENC 判断是否为 NVIDIA NVENC 硬件编码器（h264_nvenc/hevc_nvenc/av1_nvenc 等）
func isNVENC(vcodec string) bool {
	return strings.HasSuffix(vcodec, "_nvenc")
}

// audioBitrate 参数用于在 MKV 格式且音频 COPY 时写入 STATISTICS_TAGS 元数据
func buildFFmpegArgs(p Profile, outputFormat string, audioBitrate string) string {
	// 自定义 FFmpeg 参数模式：直接使用用户输入的参数
	if p.CustomFfmpeg && p.CustomFfmpegArgs != "" {
		return p.CustomFfmpegArgs
	}

	var args []string

	// 流映射：保留所有视频、音频和字幕流（排除数据流、封面流等不支持的流）
	// FFmpeg 默认只选择第一个视频流和第一个音频流，其他流会被丢弃
	// -map 0 映射所有流，但需要排除 Matroska 不支持的流类型（如数据流、封面流）
	// 使用 ? 使映射变为可选，避免源文件没有某些类型流时 FFmpeg 报错
	args = append(args, "-map", "0:v?", "-map", "0:a?", "-map", "0:s?")

	// 视频参数
	if p.Vcodec == "" {
		// 未启用视频：移除所有视频流
		args = append(args, "-map", "-0:v", "-vn")
	} else {
		// 视频编码
		args = append(args, "-c:v", p.Vcodec)

		if p.Vcodec != "copy" {
			x264fam := p.Vcodec == "libx264" || p.Vcodec == "libx265"
			crfCapable := x264fam || p.Vcodec == "libvpx-vp9" || p.Vcodec == "av1"

			// 分辨率
			if p.Width > 0 && p.Height > 0 {
				args = append(args, "-s", fmt.Sprintf("%dx%d", p.Width, p.Height))
			}

			// 视频滤镜（合并到 -vf）：比例处理 + eq + yadif + unsharp
			var vfParts []string
			if p.AspectRatio == "crop" {
				vfParts = append(vfParts, "crop=iw:ih")
			} else if p.AspectRatio == "fill" {
				vfParts = append(vfParts, "pad=iw:ih")
			} else if p.AspectRatio == "stretch" {
				vfParts = append(vfParts, "scale=iw:ih")
			}
			// eq 滤镜：亮度/对比度/饱和度（非默认值时加入）
			var eqParts []string
			if p.VideoBrightness != 0 {
				eqParts = append(eqParts, fmt.Sprintf("brightness=%.2f", p.VideoBrightness))
			}
			if p.VideoContrast != 0 && p.VideoContrast != 1 {
				eqParts = append(eqParts, fmt.Sprintf("contrast=%.2f", p.VideoContrast))
			}
			if p.VideoSaturation != 0 && p.VideoSaturation != 1 {
				eqParts = append(eqParts, fmt.Sprintf("saturation=%.2f", p.VideoSaturation))
			}
			if len(eqParts) > 0 {
				vfParts = append(vfParts, "eq="+strings.Join(eqParts, ":"))
			}
			if p.EnableYadif {
				vfParts = append(vfParts, "yadif")
			}
			if p.EnableUnsharp {
				v := p.UnsharpStrength
				if v <= 0 {
					v = 1.0
				}
				vfParts = append(vfParts, fmt.Sprintf("unsharp=3:3:%.2f:3:3:0.0", v))
			}
			if len(vfParts) > 0 {
				args = append(args, "-vf", strings.Join(vfParts, ","))
			}

			// 帧率
			if p.Fps == "custom" && p.FpsCustom > 0 {
				args = append(args, "-r", fmt.Sprintf("%d", p.FpsCustom))
			} else if p.Fps != "" && p.Fps != "original" {
				args = append(args, "-r", p.Fps)
			}

			// 码率控制：优先使用 qualityControl/constantQuality（新字段），兼容旧 rateControl/crf
			if p.QualityControl == "quality" {
				qVal := p.ConstantQuality
				if qVal <= 0 {
					qVal = p.Crf // 兼容旧数据
				}
				if qVal > 0 {
					qParam := "-crf"
					if isNVENC(p.Vcodec) || p.Vcodec == "h264_amf" || p.Vcodec == "hevc_amf" {
						qParam = "-cq"
					} else if p.Vcodec == "mpeg4" {
						qParam = "-qp"
					}
					args = append(args, qParam, fmt.Sprintf("%d", qVal))
				}
			} else if p.QualityControl == "bitrate" {
				if p.Bitrate != "" {
					args = append(args, "-b:v", p.Bitrate+"k")
				}
				if p.RateControl == "cbr" && p.Bitrate != "" {
					args = append(args, "-minrate", p.Bitrate+"k", "-maxrate", p.Bitrate+"k")
				}
			} else {
				// 兼容旧数据：无 qualityControl 时使用 rateControl
				switch p.RateControl {
				case "crf":
					if p.Crf > 0 {
						if crfCapable {
							args = append(args, "-crf", fmt.Sprintf("%d", p.Crf))
						} else if isNVENC(p.Vcodec) {
							// 旧数据兼容：NVENC 编码器使用 -cq 而不是 -crf
							args = append(args, "-cq", fmt.Sprintf("%d", p.Crf))
						}
					}
				case "cbr", "vbr":
					if p.Bitrate != "" {
						args = append(args, "-b:v", p.Bitrate+"k")
					}
					if p.RateControl == "cbr" && p.Bitrate != "" {
						args = append(args, "-minrate", p.Bitrate+"k", "-maxrate", p.Bitrate+"k")
					}
				}
			}

			// 扩展码率参数
			if p.VideoMaxRate > 0 {
				args = append(args, "-maxrate", fmt.Sprintf("%dk", p.VideoMaxRate))
			}
			if p.VideoBufSize > 0 {
				args = append(args, "-bufsize", fmt.Sprintf("%dk", p.VideoBufSize))
			}

			// 预设（仅 libx264/libx265 使用 -preset 且选项为 x264 preset 名）
			if p.Preset != "" && x264fam {
				args = append(args, "-preset", p.Preset)
			}

			// 像素格式
			if p.PixFmt != "" && p.PixFmt != "original" {
				args = append(args, "-pix_fmt", p.PixFmt)
			}

			// 编码 profile
			if p.Profile != "" {
				args = append(args, "-profile:v", p.Profile)
			}

			// 调优 tune（仅 libx264/libx265）
			if p.Tune != "" && x264fam {
				args = append(args, "-tune", p.Tune)
			}

			// 旋转
			if p.Rotate != 0 {
				args = append(args, "-metadata:s:v:0", fmt.Sprintf("rotate=%d", p.Rotate))
			}

			// GOP / B 帧
			if p.Gop > 0 {
				args = append(args, "-g", fmt.Sprintf("%d", p.Gop))
			}
			if p.Bframes > 0 && p.Vcodec != "libvpx-vp9" {
				args = append(args, "-bf", fmt.Sprintf("%d", p.Bframes))
			}

			// 缩放算法
			if p.ScaleAlgo != "" {
				args = append(args, "-sws_flags", p.ScaleAlgo)
			}

			// 色彩空间
			if p.ColorSpace != "" && p.ColorSpace != "original" {
				args = append(args, "-colorspace", p.ColorSpace, "-color_primaries", p.ColorSpace, "-color_trc", p.ColorSpace)
			}

			// 参考帧
			if p.VideoRefs > 0 {
				args = append(args, "-refs", fmt.Sprintf("%d", p.VideoRefs))
			}

			// 场景切换阈值（仅 libx264/libx265）
			if p.VideoScThreshold > 0 && x264fam {
				args = append(args, "-sc_threshold", fmt.Sprintf("%d", p.VideoScThreshold))
			}

			// x264 自适应量化强度
			if p.Vcodec == "libx264" && p.X264AQStrength > 0 {
				args = append(args, "-x264-opts", fmt.Sprintf("aq-strength=%.2f", p.X264AQStrength))
			}

			// x265 专属
			if p.Vcodec == "libx265" {
				var x265opts []string
				if p.CtuSize > 0 {
					x265opts = append(x265opts, fmt.Sprintf("ctu-size=%d", p.CtuSize))
				}
				if p.RdLevel > 0 {
					x265opts = append(x265opts, fmt.Sprintf("rd-level=%d", p.RdLevel))
				}
				if len(x265opts) > 0 {
					args = append(args, "-x265-params", strings.Join(x265opts, ":"))
				}
			}

			// NVENC 专属（h264_nvenc/hevc_nvenc/av1_nvenc 共用）
			if isNVENC(p.Vcodec) {
				if p.NvencSpatialAQ {
					args = append(args, "-rc-lookahead", "1")
				}
				if p.NvencTemporalAQ {
					args = append(args, "-temporal-aq", "1")
				}
			}
		}
	}

	// 音频参数
	if p.Acodec == "" {
		// 未启用音频
		args = append(args, "-an")
	} else {
		// 音频编码
		args = append(args, "-c:a", p.Acodec)

		if p.Acodec != "copy" {
			// 声道数
			if p.Channels != "" && p.Channels != "original" {
				args = append(args, "-ac", p.Channels)
			}

			// 采样率
			if p.SampleRate == "custom" && p.SampleRateCustom > 0 {
				args = append(args, "-ar", fmt.Sprintf("%d", p.SampleRateCustom))
			} else if p.SampleRate != "" && p.SampleRate != "original" {
				args = append(args, "-ar", p.SampleRate)
			}

			// 音频码率（无损编码 flac/pcm_s16le 无意义）
			if p.Acodec != "flac" && p.Acodec != "pcm_s16le" {
				if p.AudioBitrate == "custom" && p.AudioBitrateCustom > 0 {
					args = append(args, "-b:a", fmt.Sprintf("%dk", p.AudioBitrateCustom))
				} else if p.AudioBitrate != "" && p.AudioBitrate != "original" {
					args = append(args, "-b:a", p.AudioBitrate+"k")
				}
			}

			// 采样格式
			if p.SampleFmt != "" && p.SampleFmt != "original" {
				args = append(args, "-sample_fmt", p.SampleFmt)
			}

			// AAC profile
			if p.AacProfile != "" && p.AacProfile != "original" && p.Acodec == "aac" {
				args = append(args, "-profile:a", p.AacProfile)
			}

			// 音频滤镜（合并到 -af）：volume + silenceremove + dynaudnorm
			var afParts []string
			if p.Volume > 0 && p.Volume != 1.0 {
				afParts = append(afParts, fmt.Sprintf("volume=%.2f", p.Volume))
			}
			if p.Silence {
				afParts = append(afParts, "silenceremove=stop_periods=-1:stop_duration=0:stop_threshold=-50dB")
			}
			if p.AudioDynNorm {
				afParts = append(afParts, "dynaudnorm")
			}
			if len(afParts) > 0 {
				args = append(args, "-af", strings.Join(afParts, ","))
			}

			// 音频高频截止频率
			if p.AudioCutoff > 0 {
				args = append(args, "-cutoff", fmt.Sprintf("%d", p.AudioCutoff))
			}

			// Opus 压缩级别
			if p.Acodec == "opus" {
				args = append(args, "-compression_level", fmt.Sprintf("%d", p.OpusCompLevel))
			}

			// 音视频同步偏移
			if p.AudioSyncOffset != 0 {
				args = append(args, "-async", fmt.Sprintf("%d", p.AudioSyncOffset))
			}
		}
	}

	// 字幕流编码处理
	// MP4 容器不支持 subrip(srt) 字幕的直接拷贝，需要转换为 mov_text
	// MKV 等容器支持 subrip，可以直接拷贝
	switch strings.ToLower(outputFormat) {
	case "mp4", "m4v":
		args = append(args, "-c:s", "mov_text")
	default:
		args = append(args, "-c:s", "copy")
	}

	// MKV 格式且音频 COPY 时，写入 STATISTICS_TAGS 元数据以保留音频码率
	// 有些源文件在 MKV 容器中，音频码率存储在 STATISTICS_TAGS 中
	// COPY 时这些元数据可能丢失，需要手动写入
	if strings.ToLower(outputFormat) == "mkv" && p.Acodec == "copy" && audioBitrate != "" {
		if _, err := strconv.ParseInt(audioBitrate, 10, 64); err == nil {
			args = append(args, "-metadata:s:a:0", "STATISTICS_TAGS=BPS")
			args = append(args, "-metadata:s:a:0", "STATISTICS_BPS="+audioBitrate)
			args = append(args, "-metadata:s:a:0", "STATISTICS_WRITING_APP=FVCC")
		}
	}

	// 高级封装参数（全局）
	if p.HwAccel != "" && p.HwAccel != "original" {
		args = append(args, "-hwaccel", p.HwAccel)
	}
	if p.MovFastStart {
		args = append(args, "-movflags", "+faststart")
	}
	if p.ThreadCount != "" && p.ThreadCount != "auto" {
		args = append(args, "-threads", p.ThreadCount)
	}

	// 额外参数
	if p.ExtraArgs != "" {
		args = append(args, strings.Fields(p.ExtraArgs)...)
	}

	return strings.Join(args, " ")
}

// requestLogger 简单的访问日志中间件。
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Debug("access", "%s %s %s %v", c.Request.Method, c.Request.URL.Path, c.ClientIP(), time.Since(start))
	}
}
