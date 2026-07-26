package main

import (
	"context"
	"encoding/json"
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
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/logger"
)

// Handlers 持有所有依赖的处理器组。
type Handlers struct {
	store     *Store
	probe     *FFprobe
	pv        *PathValidator
	remote    *RemoteClient
	hub       *Hub
	scheduler *Scheduler
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
	c.JSON(200, gin.H{"settings": h.store.GetSettings()})
}

func (h *Handlers) saveSettings(c *gin.Context) {
	var s Settings
	if err := c.ShouldBindJSON(&s); err != nil {
		c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
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

	c.JSON(200, gin.H{"settings": h.store.GetSettings()})
}

func (h *Handlers) getLog(c *gin.Context) {
	logPath := filepath.Join(h.store.dataDir, "info.log")
	info, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(200, gin.H{"size": 0, "content": "", "exists": false})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"size": info.Size(), "content": string(data), "exists": true})
}

func (h *Handlers) clearLog(c *gin.Context) {
	logPath := filepath.Join(h.store.dataDir, "info.log")
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
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
		c.JSON(400, gin.H{"error": "路径不能为空"})
		return
	}

	videos, err := h.scanDirectoryOnce(req.Path)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"videos": videos, "total": len(videos)})
}

func (h *Handlers) scanDirectoryStream(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(400, gin.H{"error": "路径不能为空"})
		return
	}

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
		})
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
				data, _ := json.Marshal(gin.H{"type": "error", "error": err.Error()})
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
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(videos, func(i, j int) bool {
		return videos[i].FileName < videos[j].FileName
	})
	return videos, nil
}

func (h *Handlers) doScanDirectory(path string, onProgress func([]VideoInfo) bool) error {
	if err := h.pv.Validate(path); err != nil {
		return err
	}

	settings := h.store.GetSettings()
	if settings.TransferMode == "smb" && settings.SMBUser != "" {
		if ok, err := smbshare.IsPathShared(settings.SMBUser, path); err != nil {
			return fmt.Errorf("校验共享状态失败: %v", err)
		} else if !ok {
			return fmt.Errorf("该目录未通过SMB共享，SMB模式下不可访问")
		}
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
		if fi.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !videoExts[ext] {
			return nil
		}

		base := filepath.Base(p)
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
		c.JSON(400, gin.H{"error": "路径不能为空"})
		return
	}

	if err := h.pv.Validate(req.Path); err != nil {
		c.JSON(403, gin.H{"error": err.Error()})
		return
	}

	// SMB 模式：额外校验路径是否在已共享目录内
	settings := h.store.GetSettings()
	if settings.TransferMode == "smb" && settings.SMBUser != "" {
		if ok, err := smbshare.IsPathShared(settings.SMBUser, req.Path); err != nil {
			c.JSON(500, gin.H{"error": "校验共享状态失败: " + err.Error()})
			return
		} else if !ok {
			c.JSON(403, gin.H{"error": "该文件未通过SMB共享，SMB模式下不可访问"})
			return
		}
	}

	info, err := h.probe.Probe(req.Path)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
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
		c.JSON(400, gin.H{"error": "路径解析失败"})
		return
	}
	if err := h.pv.Validate(decodedPath); err != nil {
		c.JSON(403, gin.H{"error": err.Error()})
		return
	}
	if _, err := os.Stat(decodedPath); os.IsNotExist(err) {
		c.JSON(404, gin.H{"error": "文件不存在"})
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
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := h.pv.Validate(req.Path); err != nil {
		c.JSON(403, gin.H{"error": err.Error()})
		return
	}
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		c.JSON(404, gin.H{"error": "文件不存在"})
		return
	}
	if req.NewName == "" {
		c.JSON(400, gin.H{"error": "新文件名不能为空"})
		return
	}
	dir := filepath.Dir(req.Path)
	newPath := filepath.Join(dir, req.NewName)
	if err := os.Rename(req.Path, newPath); err != nil {
		c.JSON(500, gin.H{"error": "重命名失败: " + err.Error()})
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
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := h.pv.Validate(req.Path); err != nil {
		c.JSON(403, gin.H{"error": err.Error()})
		return
	}
	if err := h.pv.Validate(req.DestDir); err != nil {
		c.JSON(403, gin.H{"error": "目标目录未授权"})
		return
	}
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		c.JSON(404, gin.H{"error": "文件不存在"})
		return
	}
	if _, err := os.Stat(req.DestDir); os.IsNotExist(err) {
		c.JSON(404, gin.H{"error": "目标目录不存在"})
		return
	}
	fileName := filepath.Base(req.Path)
	newPath := filepath.Join(req.DestDir, fileName)
	if err := os.Rename(req.Path, newPath); err != nil {
		c.JSON(500, gin.H{"error": "移动失败: " + err.Error()})
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
		c.JSON(400, gin.H{"error": "路径不能为空"})
		return
	}
	if err := h.pv.Validate(req.Path); err != nil {
		c.JSON(403, gin.H{"error": err.Error()})
		return
	}
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		c.JSON(404, gin.H{"error": "文件不存在"})
		return
	}
	if err := os.Remove(req.Path); err != nil {
		c.JSON(500, gin.H{"error": "删除失败: " + err.Error()})
		return
	}
	h.store.DeleteVideoCache(req.Path)
	c.JSON(200, gin.H{"ok": true})
}

// ===== 目录浏览 =====

// browseEntry 目录浏览条目
type browseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
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
		dirs := make([]browseEntry, 0, len(roots))
		for _, p := range roots {
			if isSMB {
				// SMB 模式：只返回可通过SMB访问的授权目录
				if ok, err := smbshare.IsPathShared(settings.SMBUser, p); err != nil || !ok {
					continue
				}
			}
			dirs = append(dirs, browseEntry{Name: filepath.Base(p), Path: p})
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
		c.JSON(403, gin.H{"error": err.Error()})
		return
	}

	// SMB 模式：额外校验路径是否在已共享目录内
	if isSMB {
		if ok, err := smbshare.IsPathShared(settings.SMBUser, pathParam); err != nil {
			c.JSON(500, gin.H{"error": "校验共享状态失败: " + err.Error()})
			return
		} else if !ok {
			c.JSON(403, gin.H{"error": "该目录未通过SMB共享，SMB模式下不可访问"})
			return
		}
	}

	info, err := os.Stat(pathParam)
	if err != nil {
		c.JSON(404, gin.H{"error": fmt.Sprintf("路径不存在: %v", err)})
		return
	}
	if !info.IsDir() {
		c.JSON(400, gin.H{"error": "指定路径不是目录"})
		return
	}

	entries, err := os.ReadDir(pathParam)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("读取目录失败: %v", err)})
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
	c.JSON(200, gin.H{"servers": h.store.GetServers()})
}

func (h *Handlers) createServer(c *gin.Context) {
	var sv Server
	if err := c.ShouldBindJSON(&sv); err != nil {
		c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
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
	h.store.UpsertServer(sv)
	c.JSON(200, sv)
}

func (h *Handlers) updateServer(c *gin.Context) {
	id := c.Param("id")
	sv, ok := h.store.GetServer(id)
	if !ok {
		c.JSON(404, gin.H{"error": "服务器不存在"})
		return
	}
	if err := c.ShouldBindJSON(&sv); err != nil {
		c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	sv.ID = id
	h.store.UpsertServer(sv)
	c.JSON(200, sv)
}

func (h *Handlers) deleteServer(c *gin.Context) {
	id := c.Param("id")
	if !h.store.DeleteServer(id) {
		c.JSON(404, gin.H{"error": "服务器不存在"})
		return
	}
	h.remote.CloseConn(id)
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handlers) testServer(c *gin.Context) {
	id := c.Param("id")
	sv, ok := h.store.GetServer(id)
	if !ok {
		c.JSON(404, gin.H{"error": "服务器不存在"})
		return
	}

	// 尝试连接
	h.remote.CloseConn(id)
	_, _, err := h.remote.Connect(sv)
	if err != nil {
		h.store.UpdateServerStatus(id, "offline")
		c.JSON(200, gin.H{"ok": false, "error": err.Error()})
		return
	}
	h.store.UpdateServerStatus(id, "online")
	c.JSON(200, gin.H{"ok": true, "status": "online"})
}

// ===== 转码方案管理 =====

func (h *Handlers) listProfiles(c *gin.Context) {
	c.JSON(200, gin.H{"profiles": h.store.GetProfiles()})
}

func (h *Handlers) createProfile(c *gin.Context) {
	var p Profile
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
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
		c.JSON(404, gin.H{"error": "方案不存在"})
		return
	}
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	if v, ok := updates["name"]; ok {
		if s, ok := v.(string); ok {
			p.Name = s
		}
	}
	if v, ok := updates["customFfmpeg"]; ok {
		if b, ok := v.(bool); ok {
			p.CustomFfmpeg = b
		}
	}
	if v, ok := updates["customFfmpegArgs"]; ok {
		if s, ok := v.(string); ok {
			p.CustomFfmpegArgs = s
		}
	}
	if v, ok := updates["outputPath"]; ok {
		if s, ok := v.(string); ok {
			p.OutputPath = s
		}
	}
	if v, ok := updates["deleteSource"]; ok {
		if b, ok := v.(bool); ok {
			p.DeleteSource = b
		}
	}
	if v, ok := updates["vcodec"]; ok {
		if s, ok := v.(string); ok {
			p.Vcodec = s
		}
	}
	if v, ok := updates["acodec"]; ok {
		if s, ok := v.(string); ok {
			p.Acodec = s
		}
	}
	if v, ok := updates["width"]; ok {
		if f, ok := v.(float64); ok {
			p.Width = int(f)
		}
	}
	if v, ok := updates["height"]; ok {
		if f, ok := v.(float64); ok {
			p.Height = int(f)
		}
	}
	if v, ok := updates["aspectRatio"]; ok {
		if s, ok := v.(string); ok {
			p.AspectRatio = s
		}
	}
	if v, ok := updates["fps"]; ok {
		if s, ok := v.(string); ok {
			p.Fps = s
		}
	}
	if v, ok := updates["fpsCustom"]; ok {
		if f, ok := v.(float64); ok {
			p.FpsCustom = int(f)
		}
	}
	if v, ok := updates["rateControl"]; ok {
		if s, ok := v.(string); ok {
			p.RateControl = s
		}
	}
	if v, ok := updates["crf"]; ok {
		if f, ok := v.(float64); ok {
			p.Crf = int(f)
		}
	}
	if v, ok := updates["bitrate"]; ok {
		if s, ok := v.(string); ok {
			p.Bitrate = s
		}
	}
	if v, ok := updates["qualityControl"]; ok {
		if s, ok := v.(string); ok {
			p.QualityControl = s
		}
	}
	if v, ok := updates["constantQuality"]; ok {
		if f, ok := v.(float64); ok {
			p.ConstantQuality = int(f)
		}
	}
	if v, ok := updates["preset"]; ok {
		if s, ok := v.(string); ok {
			p.Preset = s
		}
	}
	if v, ok := updates["pixFmt"]; ok {
		if s, ok := v.(string); ok {
			p.PixFmt = s
		}
	}
	if v, ok := updates["profile"]; ok {
		if s, ok := v.(string); ok {
			p.Profile = s
		}
	}
	if v, ok := updates["tune"]; ok {
		if s, ok := v.(string); ok {
			p.Tune = s
		}
	}
	if v, ok := updates["rotate"]; ok {
		if f, ok := v.(float64); ok {
			p.Rotate = int(f)
		}
	}
	if v, ok := updates["gop"]; ok {
		if f, ok := v.(float64); ok {
			p.Gop = int(f)
		}
	}
	if v, ok := updates["bframes"]; ok {
		if f, ok := v.(float64); ok {
			p.Bframes = int(f)
		}
	}
	if v, ok := updates["scaleAlgo"]; ok {
		if s, ok := v.(string); ok {
			p.ScaleAlgo = s
		}
	}
	if v, ok := updates["videoMaxRate"]; ok {
		if f, ok := v.(float64); ok {
			p.VideoMaxRate = int(f)
		}
	}
	if v, ok := updates["videoBufSize"]; ok {
		if f, ok := v.(float64); ok {
			p.VideoBufSize = int(f)
		}
	}
	if v, ok := updates["videoBrightness"]; ok {
		if f, ok := v.(float64); ok {
			p.VideoBrightness = f
		}
	}
	if v, ok := updates["videoContrast"]; ok {
		if f, ok := v.(float64); ok {
			p.VideoContrast = f
		}
	}
	if v, ok := updates["videoSaturation"]; ok {
		if f, ok := v.(float64); ok {
			p.VideoSaturation = f
		}
	}
	if v, ok := updates["enableYadif"]; ok {
		if b, ok := v.(bool); ok {
			p.EnableYadif = b
		}
	}
	if v, ok := updates["enableUnsharp"]; ok {
		if b, ok := v.(bool); ok {
			p.EnableUnsharp = b
		}
	}
	if v, ok := updates["unsharpStrength"]; ok {
		if f, ok := v.(float64); ok {
			p.UnsharpStrength = f
		}
	}
	if v, ok := updates["colorSpace"]; ok {
		if s, ok := v.(string); ok {
			p.ColorSpace = s
		}
	}
	if v, ok := updates["videoRefs"]; ok {
		if f, ok := v.(float64); ok {
			p.VideoRefs = int(f)
		}
	}
	if v, ok := updates["x264AQStrength"]; ok {
		if f, ok := v.(float64); ok {
			p.X264AQStrength = f
		}
	}
	if v, ok := updates["nvencSpatialAQ"]; ok {
		if b, ok := v.(bool); ok {
			p.NvencSpatialAQ = b
		}
	}
	if v, ok := updates["nvencTemporalAQ"]; ok {
		if b, ok := v.(bool); ok {
			p.NvencTemporalAQ = b
		}
	}
	if v, ok := updates["videoScThreshold"]; ok {
		if f, ok := v.(float64); ok {
			p.VideoScThreshold = int(f)
		}
	}
	if v, ok := updates["ctuSize"]; ok {
		if f, ok := v.(float64); ok {
			p.CtuSize = int(f)
		}
	}
	if v, ok := updates["rdLevel"]; ok {
		if f, ok := v.(float64); ok {
			p.RdLevel = int(f)
		}
	}
	if v, ok := updates["channels"]; ok {
		if s, ok := v.(string); ok {
			p.Channels = s
		}
	}
	if v, ok := updates["sampleRate"]; ok {
		if s, ok := v.(string); ok {
			p.SampleRate = s
		}
	}
	if v, ok := updates["sampleRateCustom"]; ok {
		if f, ok := v.(float64); ok {
			p.SampleRateCustom = int(f)
		}
	}
	if v, ok := updates["audioBitrate"]; ok {
		if s, ok := v.(string); ok {
			p.AudioBitrate = s
		}
	}
	if v, ok := updates["audioBitrateCustom"]; ok {
		if f, ok := v.(float64); ok {
			p.AudioBitrateCustom = int(f)
		}
	}
	if v, ok := updates["sampleFmt"]; ok {
		if s, ok := v.(string); ok {
			p.SampleFmt = s
		}
	}
	if v, ok := updates["aacProfile"]; ok {
		if s, ok := v.(string); ok {
			p.AacProfile = s
		}
	}
	if v, ok := updates["volume"]; ok {
		if f, ok := v.(float64); ok {
			p.Volume = f
		}
	}
	if v, ok := updates["silence"]; ok {
		if b, ok := v.(bool); ok {
			p.Silence = b
		}
	}
	if v, ok := updates["audioDynNorm"]; ok {
		if b, ok := v.(bool); ok {
			p.AudioDynNorm = b
		}
	}
	if v, ok := updates["audioCutoff"]; ok {
		if f, ok := v.(float64); ok {
			p.AudioCutoff = int(f)
		}
	}
	if v, ok := updates["opusCompLevel"]; ok {
		if f, ok := v.(float64); ok {
			p.OpusCompLevel = int(f)
		}
	}
	if v, ok := updates["audioSyncOffset"]; ok {
		if f, ok := v.(float64); ok {
			p.AudioSyncOffset = int(f)
		}
	}
	if v, ok := updates["hwAccel"]; ok {
		if s, ok := v.(string); ok {
			p.HwAccel = s
		}
	}
	if v, ok := updates["movFastStart"]; ok {
		if b, ok := v.(bool); ok {
			p.MovFastStart = b
		}
	}
	if v, ok := updates["threadCount"]; ok {
		if s, ok := v.(string); ok {
			p.ThreadCount = s
		}
	}
	if v, ok := updates["outputSuffix"]; ok {
		if s, ok := v.(string); ok {
			p.OutputSuffix = s
		}
	}
	if v, ok := updates["extraArgs"]; ok {
		if s, ok := v.(string); ok {
			p.ExtraArgs = s
		}
	}
	h.store.UpsertProfile(p)
	c.JSON(200, p)
}

func (h *Handlers) deleteProfile(c *gin.Context) {
	id := c.Param("id")
	if !h.store.DeleteProfile(id) {
		c.JSON(404, gin.H{"error": "方案不存在"})
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
		c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	// 路径安全校验（所有模式都需校验授权目录）
	settings := h.store.GetSettings()
	if err := h.pv.Validate(req.SourceFile); err != nil {
		c.JSON(403, gin.H{"error": "源文件: " + err.Error()})
		return
	}
	if err := h.pv.Validate(req.OutputFile); err != nil {
		c.JSON(403, gin.H{"error": "输出文件: " + err.Error()})
		return
	}

	// 检查输出文件是否已存在
	if _, err := os.Stat(req.OutputFile); err == nil {
		c.JSON(409, gin.H{"error": "输出文件已存在: " + req.OutputFile})
		return
	}

	// 检查是否有其他非终态任务正在转码同一输出文件
	for _, t := range h.store.GetTasks() {
		if t.OutputFile == req.OutputFile && !t.Status.IsTerminal() {
			c.JSON(409, gin.H{"error": "已有任务正在转码同一输出文件: " + req.OutputFile})
			return
		}
	}

	// SMB模式：额外校验路径是否在已共享目录内
	if settings.TransferMode == "smb" && settings.SMBUser != "" {
		if ok, err := smbshare.IsPathShared(settings.SMBUser, req.SourceFile); err != nil {
			c.JSON(500, gin.H{"error": "校验源文件共享状态失败: " + err.Error()})
			return
		} else if !ok {
			c.JSON(403, gin.H{"error": "源文件不在SMB共享目录内，无法通过SMB模式访问"})
			return
		}
		if ok, err := smbshare.IsPathShared(settings.SMBUser, req.OutputFile); err != nil {
			c.JSON(500, gin.H{"error": "校验输出文件共享状态失败: " + err.Error()})
			return
		} else if !ok {
			c.JSON(403, gin.H{"error": "输出文件不在SMB共享目录内，无法通过SMB模式访问"})
			return
		}
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
			c.JSON(404, gin.H{"error": "服务器不存在"})
			return
		}
		if !server.IsLocal && server.Status == "offline" {
			logger.Warn("task", "createTask: server offline, adding task as paused: server=%s, task=%s", server.Name, req.SourceFile)
		}
	}
	profile, ok := h.store.GetProfile(req.ProfileID)
	if !ok {
		c.JSON(404, gin.H{"error": "转码方案不存在"})
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

	task := Task{
		ID:          "task_" + fmt.Sprintf("%d", time.Now().UnixNano()),
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
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
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
		c.JSON(404, gin.H{"error": "任务不存在"})
		return
	}
	if t.Status.IsTerminal() {
		c.JSON(400, gin.H{"error": "终态任务无法暂停"})
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
		c.JSON(404, gin.H{"error": "任务不存在"})
		return
	}
	if t.Status != StatusPaused && t.Status != StatusError {
		c.JSON(400, gin.H{"error": "仅暂停/错误状态可恢复"})
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
		c.JSON(404, gin.H{"error": "任务不存在"})
		return
	}
	if t.Status.IsTerminal() {
		c.JSON(400, gin.H{"error": "终态任务无法取消"})
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
		c.JSON(404, gin.H{"error": "任务不存在"})
		return
	}
	if t.Status != StatusError {
		c.JSON(400, gin.H{"error": "仅错误状态可重试"})
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
		c.JSON(404, gin.H{"error": "任务不存在"})
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
		c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	if err := h.store.ReorderTasks(req.TaskIDs); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
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
		c.JSON(404, gin.H{"error": "历史记录不存在"})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ===== 监控指标 =====

func (h *Handlers) metrics(c *gin.Context) {
	tasks := h.store.GetTasks()
	history := h.store.GetHistory()
	servers := h.store.GetServers()

	stats := gin.H{
		"totalTasks":   len(tasks),
		"totalHistory": len(history),
		"totalServers": len(servers),
		"byStatus":     map[string]int{},
	}
	byStatus := stats["byStatus"].(map[string]int)
	for _, t := range tasks {
		byStatus[string(t.Status)]++
	}

	// 服务器在线数
	onlineServers := 0
	for _, sv := range servers {
		if sv.Status == "online" {
			onlineServers++
		}
	}
	stats["onlineServers"] = onlineServers

	c.JSON(200, stats)
}

// buildFFmpegArgs 将转码方案转换为 FFmpeg 命令行参数。
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
					if p.Vcodec == "h264_nvenc" || p.Vcodec == "hevc_nvenc" || p.Vcodec == "h264_amf" || p.Vcodec == "hevc_amf" {
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
					if p.Crf > 0 && crfCapable {
						args = append(args, "-crf", fmt.Sprintf("%d", p.Crf))
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

			// NVENC 专属
			if p.Vcodec == "h264_nvenc" || p.Vcodec == "hevc_nvenc" {
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
