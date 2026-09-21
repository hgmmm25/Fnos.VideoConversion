package media

// stream_media.go：M4 预览网关的纯媒体侧能力（P2-1 B轮 stream 域收口）。
// 本文件只承载与 HTTP/handler 无关的媒体处理：路径归一化、MIME、Range 解析、
// ETag、抽帧参数与执行。gin/Handlers 相关逻辑保留在根包 stream.go。

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fvcc/internal/edl"
)

// ===== 约束（04 §2.2~§2.4）=====

const (
	ThumbWidth       = 320                // 缩略图宽
	ThumbHeight      = 180                // 缩略图高
	ThumbQuality     = 3                  // ffmpeg -q:v（≈ JPEG 质量 80）
	ThumbGenTimeout  = 30 * time.Second   // 单次抽帧超时
	MaxMediaRelPathLen = 512              // 相对路径长度上限
)

// NormalizeMediaRoot root 参数归一化（空 → src，04 §2.6）。
func NormalizeMediaRoot(root string) string {
	r := strings.TrimSpace(root)
	if r == "" {
		return "src"
	}
	return r
}

// SanitizeRelPath 相对路径四层校验（04 §2.6）：拒绝 NUL/控制字符 → 拒绝反斜杠/绝对路径
// → path.Clean 后拒绝 .. 越界 → 长度上限。成功返回相对当前根的 POSIX 路径。
// 失败时返回 edl.ErrCodeAssetNotInRoot。
func SanitizeRelPath(raw string) (string, string) {
	if raw == "" || len(raw) > MaxMediaRelPathLen {
		return "", edl.ErrCodeAssetNotInRoot
	}
	for _, r := range raw {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "", edl.ErrCodeAssetNotInRoot
		}
	}
	if strings.Contains(raw, `\`) || strings.HasPrefix(raw, "/") {
		return "", edl.ErrCodeAssetNotInRoot
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || clean == "/" || strings.HasPrefix(clean, "../") {
		return "", edl.ErrCodeAssetNotInRoot
	}
	if len(clean) > 255 {
		return "", edl.ErrCodeAssetNotInRoot
	}
	return clean, ""
}

// ===== MIME（04 §2.2）=====

var StreamMimeTypes = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mov":  "video/quicktime",
	".mkv":  "video/x-matroska",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".mxf":  "application/mxf",
	".ts":   "video/mp2t",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".wav":  "audio/wav",
	".flac": "audio/flac",
}

// MimeByExt 按扩展名返回媒体 MIME（未知类型回退 application/octet-stream）。
func MimeByExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if m, ok := StreamMimeTypes[ext]; ok {
		return m
	}
	return "application/octet-stream"
}

// ===== Range 解析（04 §2.2）=====

// ParseRangeHeader 解析单区间 Range 头。
// 返回 hasRange=false 表示无 Range（或语法非法，按忽略处理 → 200 全量）；
// hasRange=true 且 satisfiable=false 表示范围不可满足 → 416。
func ParseRangeHeader(header string, size int64) (start, end int64, hasRange, satisfiable bool) {
	h := strings.TrimSpace(header)
	if h == "" || !strings.HasPrefix(h, "bytes=") {
		return 0, 0, false, false
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if i := strings.Index(spec, ","); i >= 0 { // 多区间只服务第一段（浏览器播放器实际只发单区间）
		spec = spec[:i]
	}
	spec = strings.TrimSpace(spec)
	dash := strings.Index(spec, "-")
	if dash < 0 {
		return 0, 0, false, false
	}
	startStr, endStr := strings.TrimSpace(spec[:dash]), strings.TrimSpace(spec[dash+1:])

	switch {
	case startStr == "" && endStr == "": // "bytes=-"
		return 0, 0, false, false
	case startStr == "": // 后缀区间 "bytes=-N"
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false, false
		}
		if n > size {
			n = size
		}
		if size == 0 {
			return 0, 0, true, false
		}
		return size - n, size - 1, true, true
	default:
		s, err := strconv.ParseInt(startStr, 10, 64)
		if err != nil || s < 0 {
			return 0, 0, false, false
		}
		if s >= size {
			return 0, 0, true, false
		}
		e := size - 1
		if endStr != "" {
			ev, err := strconv.ParseInt(endStr, 10, 64)
			if err != nil || ev < s {
				return 0, 0, false, false
			}
			if ev < e {
				e = ev
			}
		}
		return s, e, true, true
	}
}

// EtagFor 生成 "size-mtime" 派生的强 ETag（04 §2.2 要点 3）。
func EtagFor(size int64, mtime time.Time) string {
	sum := sha1.Sum([]byte(strconv.FormatInt(size, 10) + "-" + strconv.FormatInt(mtime.UnixNano(), 10)))
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

// IfRangeMatches If-Range 校验：素材被替换时不返回脏数据（04 §2.2 要点 3）。
func IfRangeMatches(ifRange, etag string, mtime time.Time) bool {
	v := strings.TrimSpace(ifRange)
	if v == "" {
		return true
	}
	if strings.HasPrefix(v, `"`) || strings.HasPrefix(v, "W/") {
		return v == etag
	}
	if t, err := http.ParseTime(v); err == nil {
		return t.Unix() == mtime.Truncate(time.Second).Unix()
	}
	return false
}

// ===== 抽帧（04 §2.4）=====

// BuildThumbArgs 组装 ffmpeg 抽帧参数（320×180 居中裁剪）。
func BuildThumbArgs(src, dst string, atMs int64) []string {
	sec := float64(atMs) / 1000.0
	if sec < 0 {
		sec = 0
	}
	vf := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d",
		ThumbWidth, ThumbHeight, ThumbWidth, ThumbHeight)
	return []string{
		"-hide_banner", "-nostdin", "-y",
		"-ss", strconv.FormatFloat(sec, 'f', 3, 64),
		"-i", src,
		"-frames:v", "1",
		"-vf", vf,
		"-q:v", strconv.Itoa(ThumbQuality),
		"-f", "image2",
		dst,
	}
}

// GenerateThumb 调用 ffmpeg 抽帧并原子落盘。ffmpegPath 由调用方注入（根包 handler
// 保留可注入的 Handlers.thumbFFmpeg 单测通道）。
func GenerateThumb(ffmpeg, src, dst string, atMs int64) error {
	if strings.TrimSpace(ffmpeg) == "" {
		return fmt.Errorf("ffmpeg 不可用")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx, cancel := context.WithTimeout(context.Background(), ThumbGenTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, BuildThumbArgs(src, tmp, atMs)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("抽帧失败: %v: %s", err, trimForLog(stderr.String(), 200))
	}
	fi, err := os.Stat(tmp)
	if err != nil || fi.Size() == 0 {
		_ = os.Remove(tmp)
		return fmt.Errorf("抽帧输出为空")
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ThumbCacheKey 缩略图缓存键：sha1(root|path|t|mtime|size) 派生文件名（04 §2.4，键含 mtime/size）。
func ThumbCacheKey(root, relPath string, tMs int64, mtime time.Time, size int64) string {
	raw := fmt.Sprintf("%s|%s|%d|%d|%d", root, relPath, tMs, mtime.UnixNano(), size)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:16]) + ".jpg"
}

// ===== 透明占位 JPEG（04 §2.4：抽帧失败返回 200 + 占位，禁止 5xx）=====

const transparentJPEGBase64 = "/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/wAALCAABAAEBAREA/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAAAAAAAAAAAAAAAAD/2gAIAQEAAD8AKp//2Q=="

var (
	transparentJPEGOnce  sync.Once
	transparentJPEGBytes []byte
)

// TransparentJPEG 返回 1×1 透明 JPEG 占位字节。
func TransparentJPEG() []byte {
	transparentJPEGOnce.Do(func() {
		b, err := base64.StdEncoding.DecodeString(transparentJPEGBase64)
		if err != nil || len(b) == 0 {
			b = []byte{}
		}
		transparentJPEGBytes = b
	})
	return transparentJPEGBytes
}

func trimForLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
