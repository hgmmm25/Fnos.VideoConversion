package api

// M4：预览网关（04 §2 与 §7 改动点索引）。
//
// 路由（router.go 注册）：
//   - POST   /api/stream/ticket  申请预览票据（非一次性；绑定 path+root+来源 IP；TTL 300s；04 §2.3）
//   - DELETE /api/stream/ticket  显式失效票据（页面卸载时调用）
//   - GET    /api/stream         Range 视频流（200/206/416），三根白名单 src/proxy/dest（04 §2.2、§2.6）
//   - GET    /api/thumb          抽帧缩略图（JPEG 320×180，惰性 + single-flight，04 §2.4）
//
// Range 解析与分段写出逻辑参照 FVCS/pkg/server/server.go: handleRangeDownload（04 §2.1：
// FVCC/FVCS 为独立二进制、无共享包，故复制实现而非跨机复用），并补齐 ETag/Last-Modified/If-Range。

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fvcc/internal/media"
	"fvcc/logger"

	"github.com/gin-gonic/gin"
)

// ===== 错误码（04 §2.2、§6）=====

const (
	errCodeTicketInvalid       = "E_TICKET_INVALID"        // 票据缺失/过期/被改绑
	errCodeStreamBusy          = "E_STREAM_BUSY"           // 全局 64 路占满
	errCodeRangeNotSatisfiable = "E_RANGE_NOT_SATISFIABLE" // 416（04 §2.2；码值实现补充）
)

// ===== 约束（04 §2.2~§2.4）=====

const (
	streamTicketTTL      = 300 * time.Second  // 票据有效期
	streamTicketPerIPMax = 60                 // 单 IP 60s 内最多发放量
	streamTicketIPWindow = 60 * time.Second   // 发放量滑动窗口
	streamGlobalMax      = 64                 // 全局并行 Range 上限
	streamPerFileMax     = 4                  // 单文件并行 Range 上限
	streamCopyChunk      = 256 * 1024         // 分段写出块大小
	thumbWidth           = media.ThumbWidth    // 缩略图宽（04 §2.4）
	thumbHeight          = media.ThumbHeight   // 缩略图高
	thumbQuality         = media.ThumbQuality  // ffmpeg -q:v（≈ JPEG 质量 80）
	thumbGlobalMax       = 2                  // 抽帧全局并发
	thumbQueueTimeout    = 30 * time.Second   // 抽帧排队超时
	thumbGenTimeout      = media.ThumbGenTimeout // 单次抽帧超时
	thumbHTTPMaxAge      = 7 * 24 * time.Hour // 缩略图浏览器缓存（键含 mtime/size）
	maxMediaRelPathLen   = media.MaxMediaRelPathLen // 相对路径长度上限
)

// streamErr 统一失败响应（03 §4.1：{ok:false, code, msg}）。
func streamErr(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"ok": false, "code": code, "msg": msg})
}

// mediaErrStatus 错误码 → HTTP 状态（04 §2.2）。
func mediaErrStatus(code string) int {
	switch code {
	case errCodeAssetNotInRoot:
		return http.StatusForbidden
	case errCodeAssetMissing:
		return http.StatusNotFound
	case errCodeRootUnknown:
		return http.StatusBadRequest
	default:
		return http.StatusBadRequest
	}
}

// resolveMediaFile 把 (root, 相对路径) 解析为根内真实文件路径（04 §2.6）：
// ResolveRoot → 拼接 → EvalSymlinks → 前缀校验（复用 PathValidator 的四层思路）。
// 返回错误码：空串表示成功。
func (h *Handlers) resolveMediaFile(root, relPath string) (string, string) {
	localRoot, code := h.ResolveRoot(root)
	if code != "" {
		logger.Error("stream", "非法 root=%q（04 §6，属编码缺陷）", root)
		return "", code
	}
	joined := filepath.Join(localRoot, filepath.FromSlash(relPath))

	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", errCodeAssetMissing
	}
	fi, err := os.Stat(real)
	if err != nil || fi.IsDir() {
		return "", errCodeAssetMissing
	}
	realRoot, err := filepath.EvalSymlinks(localRoot)
	if err != nil {
		realRoot = localRoot
	}
	if !pathWithin(realRoot, real) {
		return "", errCodeAssetNotInRoot
	}
	return real, ""
}

// ===== 票据（04 §2.3）=====

type streamTicket struct {
	Token     string
	Path      string // 相对申请时根（root）的 POSIX 路径
	Root      string
	IP        string
	ExpiresAt time.Time
}

type ticketStore struct {
	mu     sync.Mutex
	items  map[string]*streamTicket
	issued map[string][]time.Time // 来源 IP → 发放时刻（滑动窗口限流）
	now    func() time.Time       // 可注入时钟（单测）
	rngHex func() string          // 可注入 token 生成（单测）
}

func newTicketStore() *ticketStore {
	return &ticketStore{
		items:  map[string]*streamTicket{},
		issued: map[string][]time.Time{},
		now:    time.Now,
		rngHex: randHex16,
	}
}

// randHex16 生成 16 字节随机数的 32 位十六进制串（tk_<32hex>）。
// issue 发放票据；超出单 IP 配额返回 ok=false（04 §2.3：单 IP 60s ≤ 60 个）。
func (ts *ticketStore) issue(relPath, root, ip string) (*streamTicket, bool) {
	now := ts.now()
	ts.mu.Lock()
	defer ts.mu.Unlock()

	for k, v := range ts.items {
		if now.After(v.ExpiresAt) {
			delete(ts.items, k)
		}
	}
	cut := now.Add(-streamTicketIPWindow)
	kept := make([]time.Time, 0, len(ts.issued[ip]))
	for _, t := range ts.issued[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= streamTicketPerIPMax {
		ts.issued[ip] = kept
		return nil, false
	}
	tk := &streamTicket{
		Token:     "tk_" + ts.rngHex(),
		Path:      relPath,
		Root:      root,
		IP:        ip,
		ExpiresAt: now.Add(streamTicketTTL),
	}
	ts.items[tk.Token] = tk
	ts.issued[ip] = append(kept, now)
	return tk, true
}

// validate 校验票据：存在 + 未过期 + path/root 绑定一致 + 来源 IP 一致（非一次性，04 §2.3）。
func (ts *ticketStore) validate(token, relPath, root, ip string) string {
	if token == "" {
		return errCodeTicketInvalid
	}
	now := ts.now()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	tk, ok := ts.items[token]
	if !ok {
		return errCodeTicketInvalid
	}
	if !now.Before(tk.ExpiresAt) {
		delete(ts.items, token)
		return errCodeTicketInvalid
	}
	if ip != "" && tk.IP != "" && tk.IP != ip {
		return errCodeTicketInvalid
	}
	if tk.Path != relPath || tk.Root != root {
		return errCodeTicketInvalid
	}
	return ""
}

// revoke 显式失效票据（DELETE /api/stream/ticket）。
func (ts *ticketStore) revoke(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, ok := ts.items[token]; !ok {
		return false
	}
	delete(ts.items, token)
	return true
}

// ===== Handlers 惰性 getter（构造点无需改动）=====

func (h *Handlers) ticketStore() *ticketStore {
	h.ticketsMu.Lock()
	defer h.ticketsMu.Unlock()
	if h.tickets == nil {
		h.tickets = newTicketStore()
	}
	return h.tickets
}

func (h *Handlers) streamLimiter() *streamLimiter {
	h.streamsMu.Lock()
	defer h.streamsMu.Unlock()
	if h.streams == nil {
		h.streams = newStreamLimiter()
	}
	return h.streams
}

func (h *Handlers) thumbCache() *thumbCache {
	h.thumbsMu.Lock()
	defer h.thumbsMu.Unlock()
	if h.thumbs == nil {
		h.thumbs = newThumbCache()
	}
	return h.thumbs
}

// ===== 并发限制（04 §2.2）=====

type streamLimiter struct {
	mu      sync.Mutex
	total   int
	perFile map[string]int
}

func newStreamLimiter() *streamLimiter {
	return &streamLimiter{perFile: map[string]int{}}
}

func (l *streamLimiter) acquire(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= streamGlobalMax || l.perFile[key] >= streamPerFileMax {
		return false
	}
	l.total++
	l.perFile[key]++
	return true
}

func (l *streamLimiter) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total > 0 {
		l.total--
	}
	if n := l.perFile[key]; n <= 1 {
		delete(l.perFile, key)
	} else {
		l.perFile[key] = n - 1
	}
}

// ===== POST /api/stream/ticket（04 §2.3）=====

type streamTicketInput struct {
	Path string `json:"path"`
	Root string `json:"root"`
}

func (h *Handlers) handleStreamTicket(c *gin.Context) {
	var in streamTicketInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8*1024)
	if err := c.ShouldBindJSON(&in); err != nil {
		streamErr(c, http.StatusBadRequest, errCodeEDLInvalid, "请求体解析失败: "+err.Error())
		return
	}
	root := media.NormalizeMediaRoot(in.Root)
	clean, code := media.SanitizeRelPath(strings.TrimSpace(in.Path))
	if code != "" {
		streamErr(c, http.StatusForbidden, code, "素材路径不在授权范围内")
		return
	}
	if _, code := h.ResolveRoot(root); code != "" {
		logger.Error("stream", "票据申请收到非法 root=%q（04 §6，属编码缺陷）", in.Root)
		streamErr(c, http.StatusBadRequest, code, "root 参数非法")
		return
	}
	if _, code := h.resolveMediaFile(root, clean); code != "" {
		streamErr(c, mediaErrStatus(code), code, "素材不存在或不可访问")
		return
	}

	ip := c.ClientIP()
	tk, ok := h.ticketStore().issue(clean, root, ip)
	if !ok {
		streamErr(c, http.StatusTooManyRequests, errCodeStreamBusy, "票据申请过于频繁，请稍后重试")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ticket":    tk.Token,
		"expiresAt": tk.ExpiresAt.Format(time.RFC3339),
	})
}

// handleStreamTicketDelete DELETE /api/stream/ticket（04 §2.3：页面卸载时可调用）。
func (h *Handlers) handleStreamTicketDelete(c *gin.Context) {
	token := strings.TrimSpace(c.Query("ticket"))
	if token == "" {
		var in struct {
			Ticket string `json:"ticket"`
		}
		_ = c.ShouldBindJSON(&in)
		token = strings.TrimSpace(in.Ticket)
	}
	ok := h.ticketStore().revoke(token)
	c.JSON(http.StatusOK, gin.H{"ok": ok})
}

// ===== GET/HEAD /api/stream（04 §2.2）=====

func (h *Handlers) handleStream(c *gin.Context) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Header("Allow", "GET, HEAD")
		streamErr(c, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "仅支持 GET/HEAD")
		return
	}

	root := media.NormalizeMediaRoot(c.Query("root"))
	clean, code := media.SanitizeRelPath(strings.TrimSpace(c.Query("path")))
	if code != "" {
		streamErr(c, http.StatusForbidden, code, "素材路径不在授权范围内")
		return
	}
	if code := h.ticketStore().validate(strings.TrimSpace(c.Query("ticket")), clean, root, c.ClientIP()); code != "" {
		streamErr(c, http.StatusForbidden, code, "预览票据无效或已过期")
		return
	}

	abs, code := h.resolveMediaFile(root, clean)
	if code != "" {
		streamErr(c, mediaErrStatus(code), code, "素材不存在或不可访问")
		return
	}

	f, err := os.Open(abs)
	if err != nil {
		streamErr(c, http.StatusNotFound, errCodeAssetMissing, "素材不可读")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		streamErr(c, http.StatusNotFound, errCodeAssetMissing, "素材不可读")
		return
	}

	key := root + "|" + clean
	limiter := h.streamLimiter()
	if !limiter.acquire(key) {
		streamErr(c, http.StatusTooManyRequests, errCodeStreamBusy, "预览并发已满，请稍后重试")
		return
	}
	defer limiter.release(key)

	size := fi.Size()
	mtime := fi.ModTime()
	etag := media.EtagFor(size, mtime)

	c.Header("Content-Type", media.MimeByExt(abs))
	c.Header("Accept-Ranges", "bytes")
	c.Header("ETag", etag)
	c.Header("Last-Modified", mtime.UTC().Format(http.TimeFormat))
	c.Header("Cache-Control", "private, max-age=0, must-revalidate")

	rangeHdr := c.GetHeader("Range")
	if !media.IfRangeMatches(c.GetHeader("If-Range"), etag, mtime) {
		rangeHdr = "" // 素材已替换：忽略 Range，返回最新全量（防脏数据）
	}
	start, end, hasRange, satisfiable := media.ParseRangeHeader(rangeHdr, size)
	if hasRange && !satisfiable {
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		if c.Request.Method == http.MethodHead {
			return
		}
		streamErr(c, http.StatusRequestedRangeNotSatisfiable, errCodeRangeNotSatisfiable, "请求范围不可满足")
		return
	}

	if !hasRange {
		c.Status(http.StatusOK)
		if c.Request.Method == http.MethodHead {
			return
		}
		_, _ = io.CopyBuffer(c.Writer, f, make([]byte, streamCopyChunk))
		return
	}

	length := end - start + 1
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	c.Status(http.StatusPartialContent)
	if c.Request.Method == http.MethodHead {
		return
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		logger.Error("stream", "seek 失败 path_hash=%s: %v", hashForLog(clean), err)
		return
	}
	// 不缓存到内存：直接 Seek + 定长流式写出，块大小 256 KB（04 §2.2 要点 2）
	_, _ = io.CopyBuffer(c.Writer, io.LimitReader(f, length), make([]byte, streamCopyChunk))
}

// ===== GET /api/thumb（04 §2.4）=====

type thumbCall struct {
	done chan struct{}
	file string
	err  error
}

type thumbCache struct {
	mu       sync.Mutex
	inflight map[string]*thumbCall
	sem      chan struct{}
}

func newThumbCache() *thumbCache {
	return &thumbCache{inflight: map[string]*thumbCall{}, sem: make(chan struct{}, thumbGlobalMax)}
}

// do 同键并发合并（single-flight）：同键同时只有一个抽帧任务，其余等待复用结果。
func (tc *thumbCache) do(key string, fn func() (string, error)) (string, error) {
	tc.mu.Lock()
	if c, ok := tc.inflight[key]; ok {
		tc.mu.Unlock()
		<-c.done
		return c.file, c.err
	}
	call := &thumbCall{done: make(chan struct{})}
	tc.inflight[key] = call
	tc.mu.Unlock()

	call.file, call.err = fn()
	close(call.done)

	tc.mu.Lock()
	delete(tc.inflight, key)
	tc.mu.Unlock()
	return call.file, call.err
}

// thumbCacheKey 缓存键 sha1(root|path|t|mtime|size)（04 §2.4，实现收口至 internal/media）。
func thumbCacheKey(root, relPath string, tMs int64, mtime time.Time, size int64) string {
	return media.ThumbCacheKey(root, relPath, tMs, mtime, size)
}

// h.ffmpegPath 返回抽帧用 ffmpeg 路径（可注入，便于单测）。
func (h *Handlers) ffmpegPath() string {
	if strings.TrimSpace(h.thumbFFmpeg) != "" {
		return h.thumbFFmpeg
	}
	return findFFmpeg()
}

// generateThumb 调用 ffmpeg 抽帧并原子落盘（实现收口至 internal/media.GenerateThumb）。
func (h *Handlers) generateThumb(src, dst string, atMs int64) error {
	return media.GenerateThumb(h.ffmpegPath(), src, dst, atMs)
}

func trimForLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// writePlaceholderThumb 失败兜底：200 + 1×1 透明 JPEG（不缓存）。
func writePlaceholderThumb(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/jpeg", media.TransparentJPEG())
}

func (h *Handlers) handleThumb(c *gin.Context) {
	root := media.NormalizeMediaRoot(c.Query("root"))
	clean, code := media.SanitizeRelPath(strings.TrimSpace(c.Query("path")))
	if code != "" {
		streamErr(c, http.StatusForbidden, code, "素材路径不在授权范围内")
		return
	}
	// 票据可选（api.ts thumbUrl 的 ticket 为可选参数）：提供了就必须与 path/root 一致。
	if tk := strings.TrimSpace(c.Query("ticket")); tk != "" {
		if code := h.ticketStore().validate(tk, clean, root, c.ClientIP()); code != "" {
			streamErr(c, http.StatusForbidden, code, "预览票据无效或已过期")
			return
		}
	}

	abs, code := h.resolveMediaFile(root, clean)
	if code != "" {
		streamErr(c, mediaErrStatus(code), code, "素材不存在或不可访问")
		return
	}
	fi, err := os.Stat(abs)
	if err != nil {
		streamErr(c, http.StatusNotFound, errCodeAssetMissing, "素材不可访问")
		return
	}

	atMs, _ := strconv.ParseInt(strings.TrimSpace(c.Query("t")), 10, 64)
	if atMs < 0 {
		atMs = 0
	}
	key := thumbCacheKey(root, clean, atMs, fi.ModTime(), fi.Size())

	cacheRoot, _ := h.resolveCacheRoot()
	if cacheRoot == "" {
		writePlaceholderThumb(c)
		return
	}
	dst := filepath.Join(cacheRoot, "thumbs", key)
	if data, err := os.ReadFile(dst); err == nil && len(data) > 0 {
		c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", int(thumbHTTPMaxAge.Seconds())))
		c.Data(http.StatusOK, "image/jpeg", data)
		return
	}

	tc := h.thumbCache()
	got, err := tc.do(key, func() (string, error) {
		select {
		case tc.sem <- struct{}{}:
			defer func() { <-tc.sem }()
		case <-time.After(thumbQueueTimeout):
			return "", fmt.Errorf("缩略图队列超时")
		}
		// 二次命中检查：等待排队期间可能已被其他请求生成
		if d, rerr := os.ReadFile(dst); rerr == nil && len(d) > 0 {
			return dst, nil
		}
		if gerr := h.generateThumb(abs, dst, atMs); gerr != nil {
			return "", gerr
		}
		return dst, nil
	})
	if err != nil {
		logger.Warn("stream", "缩略图生成失败 path_hash=%s t=%d: %v", hashForLog(clean), atMs, err)
		writePlaceholderThumb(c)
		return
	}
	data, err := os.ReadFile(got)
	if err != nil || len(data) == 0 {
		writePlaceholderThumb(c)
		return
	}
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", int(thumbHTTPMaxAge.Seconds())))
	c.Data(http.StatusOK, "image/jpeg", data)
}
