package model

// VideoInfo ↔ VideoInfoCache 转换方法测试（混乱报告 §2.3 证据 3 收敛后
// 保证字段完整，避免引入缺字段回归）。

import (
	"testing"
	"time"
)

func TestVideoInfoCacheToVideoInfo(t *testing.T) {
	src := VideoInfoCache{
		Path:         "/media/a.mp4",
		FileName:     "a.mp4",
		Format:       "mp4",
		Size:         1024,
		Duration:     10.5,
		Resolution:   "1920x1080",
		Width:        1920,
		Height:       1080,
		Codec:        "h264",
		Bitrate:      "8000kbps",
		Fps:          "30",
		AudioCodec:   "aac",
		AudioBitrate: "128kbps",
		SampleRate:   "48000",
		Channels:     2,
		StreamCount:  2,
		Probed:       true,
		UpdatedAt:    time.Now(),
		Streams:      []StreamInfo{{Index: 0, CodecType: "video", CodecName: "h264"}},
	}

	v := src.ToVideoInfo()
	if v.Path != src.Path || v.FileName != src.FileName || v.Format != src.Format ||
		v.Size != src.Size || v.Duration != src.Duration || v.Resolution != src.Resolution ||
		v.Width != src.Width || v.Height != src.Height || v.Codec != src.Codec ||
		v.Bitrate != src.Bitrate || v.Fps != src.Fps || v.AudioCodec != src.AudioCodec ||
		v.AudioBitrate != src.AudioBitrate || v.SampleRate != src.SampleRate ||
		v.Channels != src.Channels || v.StreamCount != src.StreamCount ||
		v.Probed != src.Probed || len(v.Streams) != len(src.Streams) {
		t.Fatalf("ToVideoInfo 字段不一致: got %+v want 源 %+v", v, src)
	}
}

func TestVideoInfoToCache(t *testing.T) {
	src := VideoInfo{
		Path:         "/media/b.mkv",
		FileName:     "b.mkv",
		Format:       "mkv",
		Size:         2048,
		Duration:     20.25,
		Resolution:   "3840x2160",
		Width:        3840,
		Height:       2160,
		Codec:        "hevc",
		Bitrate:      "12000kbps",
		Fps:          "60",
		AudioCodec:   "opus",
		AudioBitrate: "192kbps",
		SampleRate:   "48000",
		Channels:     6,
		StreamCount:  3,
		Probed:       true,
		Streams:      []StreamInfo{{Index: 1, CodecType: "audio", CodecName: "opus"}},
	}

	c := src.ToCache()
	if c.Path != src.Path || c.FileName != src.FileName || c.Format != src.Format ||
		c.Size != src.Size || c.Duration != src.Duration || c.Resolution != src.Resolution ||
		c.Width != src.Width || c.Height != src.Height || c.Codec != src.Codec ||
		c.Bitrate != src.Bitrate || c.Fps != src.Fps || c.AudioCodec != src.AudioCodec ||
		c.AudioBitrate != src.AudioBitrate || c.SampleRate != src.SampleRate ||
		c.Channels != src.Channels || c.StreamCount != src.StreamCount ||
		c.Probed != src.Probed || len(c.Streams) != len(src.Streams) {
		t.Fatalf("ToCache 字段不一致: got %+v want 源 %+v", c, src)
	}
	// UpdatedAt 由 store.UpsertVideoCache 写入时统一赋值，转换时应保持零值。
	if !c.UpdatedAt.IsZero() {
		t.Fatalf("ToCache 不应携带 UpdatedAt: %v", c.UpdatedAt)
	}
}
