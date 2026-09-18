package media

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"fvcc/internal/store/model"
	"fvcc/logger"
)

// FFprobe 封装 ffprobe 工具，运行时探测路径，不存在则降级为桩数据。
type FFprobe struct {
	binaryPath string // ffprobe 可执行文件路径，空表示不可用
	available  bool
}

// NewFFprobe 创建 ffprobe 封装，探测系统中的 ffprobe。
func NewFFprobe() *FFprobe {
	// 1. 环境变量指定路径优先
	for _, envKey := range []string{"FVCC_FFPROBE", "TRIM_FFMPEG_PATH"} {
		if p := strings.TrimSpace(os.Getenv(envKey)); p != "" {
			if _, err := os.Stat(p); err == nil {
				return &FFprobe{binaryPath: p, available: true}
			}
		}
	}
	// 2. PATH 中查找
	if p, err := exec.LookPath("ffprobe"); err == nil {
		return &FFprobe{binaryPath: p, available: true}
	}
	// 3. 常见安装路径回退（fnOS / Linux 服务用户 PATH 可能受限）
	for _, candidate := range []string{
		"/usr/bin/ffprobe",
		"/usr/local/bin/ffprobe",
		"/opt/ffmpeg/bin/ffprobe",
		"/opt/bin/ffprobe",
		"/usr/trim/lib/mediasrv/bin/ffprobe",
		"/usr/trim/lib/mediasrv/ffprobe",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return &FFprobe{binaryPath: candidate, available: true}
		}
	}
	logger.Warn("ffprobe", "not found, falling back to stub data (install ffprobe for real probing)")
	return &FFprobe{available: false}
}

// Available 返回 ffprobe 是否可用。
func (f *FFprobe) Available() bool { return f.available }

// Probe 提取视频元数据。ffprobe 不可用时返回基于文件信息的桩数据。
func (f *FFprobe) Probe(path string) (model.VideoInfo, error) {
	base := filepath.Base(path)
	info := model.VideoInfo{Path: path, FileName: base, Probed: f.available}

	// 文件扩展名（格式）
	ext := filepath.Ext(base)
	if ext != "" {
		info.Format = strings.ToLower(strings.TrimPrefix(ext, "."))
	}

	// 获取文件大小
	if sz, err := fileSize(path); err == nil {
		info.Size = sz
	}

	if !f.available {
		// 降级：按扩展名猜测，填充桩数据
		f.stubProbe(&info)
		return info, nil
	}

	// 真实 ffprobe 调用
	cmd := exec.Command(f.binaryPath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format", "-show_streams",
		path,
	)
	// 设置环境变量确保中文路径和输出能正确处理
	cmd.Env = append(os.Environ(), "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8")

	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("ffprobe", "error for %s: %v, stderr: %s", path, err, string(out))
		info.Probed = false
		return info, fmt.Errorf("ffprobe 执行失败: %w", err)
	}

	var pr probeResult
	if err := json.Unmarshal(out, &pr); err != nil {
		logger.Error("ffprobe", "json unmarshal error for %s: %v", path, err)
		if len(out) > 1000 {
			logger.Debug("ffprobe", "first 1000 bytes: %s", string(out[:1000]))
		} else {
			logger.Debug("ffprobe", "full output: %s", string(out))
		}
		return info, fmt.Errorf("ffprobe 输出解析失败: %w", err)
	}

	// 流数量
	if pr.Format.NbStreams > 0 {
		info.StreamCount = pr.Format.NbStreams
	} else {
		info.StreamCount = len(pr.Streams)
	}

	// 解析 format
	if pr.Format.Duration != "" {
		if d, err := strconv.ParseFloat(pr.Format.Duration, 64); err == nil {
			info.Duration = d
		}
	}
	if pr.Format.BitRate != "" {
		info.Bitrate = pr.Format.BitRate
	}

	// 保存完整流信息
	for _, st := range pr.Streams {
		info.Streams = append(info.Streams, model.StreamInfo{
			Index:         st.Index,
			CodecType:     st.CodecType,
			CodecName:     st.CodecName,
			CodecLongName: st.CodecLongName,
			Width:         st.Width,
			Height:        st.Height,
			RFrameRate:    st.RFrameRate,
			BitRate:       st.BitRate,
			SampleRate:    st.SampleRate,
			Channels:      st.Channels,
			ChannelLayout: st.ChannelLayout,
			Tags:          st.Tags,
		})
	}

	// 解析 streams（视频流取第一个，音频流优先取AAC）
	var gotVideo, gotAudio bool
	var videoBitrateBps int64

	// 优先查找AAC音频流
	var aacStream *probeStream
	for i := range pr.Streams {
		if pr.Streams[i].CodecType == "audio" && pr.Streams[i].CodecName == "aac" {
			aacStream = &pr.Streams[i]
			break
		}
	}

	for _, st := range pr.Streams {
		if st.CodecType == "video" && !gotVideo {
			info.Codec = st.CodecName
			info.Width = st.Width
			info.Height = st.Height
			info.Resolution = fmt.Sprintf("%dx%d", st.Width, st.Height)
			if st.RFrameRate != "" {
				info.Fps = st.RFrameRate
			}
			if st.BitRate != "" {
				if v, err := strconv.ParseInt(st.BitRate, 10, 64); err == nil {
					videoBitrateBps = v
				}
			}
			gotVideo = true
		} else if st.CodecType == "audio" && !gotAudio {
			// 如果有AAC流，优先使用AAC流
			if aacStream != nil {
				st = *aacStream
			}
			info.AudioCodec = st.CodecName
			if st.BitRate != "" {
				info.AudioBitrate = st.BitRate
			} else if st.Tags != nil {
				if st.Tags["BPS"] != "" {
					info.AudioBitrate = st.Tags["BPS"]
				} else if st.Tags["BPS-eng"] != "" {
					info.AudioBitrate = st.Tags["BPS-eng"]
				} else if st.Tags["bit_rate"] != "" {
					info.AudioBitrate = st.Tags["bit_rate"]
				} else if st.Tags["BitRate"] != "" {
					info.AudioBitrate = st.Tags["BitRate"]
				} else if st.Tags["bitrate"] != "" {
					info.AudioBitrate = st.Tags["bitrate"]
				} else if st.Tags["STATISTICS_BPS"] != "" {
					info.AudioBitrate = st.Tags["STATISTICS_BPS"]
				}
			}
			if st.SampleRate != "" {
				info.SampleRate = st.SampleRate
			}
			if st.Channels > 0 {
				info.Channels = st.Channels
			}
			gotAudio = true
		}
	}

	// 回退：如果第一个音频流没有码率，尝试从其他音频流读取
	if info.AudioBitrate == "" {
		for _, st := range pr.Streams {
			if st.CodecType == "audio" && st.BitRate != "" {
				info.AudioBitrate = st.BitRate
				break
			} else if st.CodecType == "audio" && st.Tags != nil {
				if st.Tags["BPS"] != "" {
					info.AudioBitrate = st.Tags["BPS"]
					break
				} else if st.Tags["BPS-eng"] != "" {
					info.AudioBitrate = st.Tags["BPS-eng"]
					break
				} else if st.Tags["STATISTICS_BPS"] != "" {
					info.AudioBitrate = st.Tags["STATISTICS_BPS"]
					break
				}
			}
		}
	}

	// 回退：使用总码率减去视频码率估算音频码率（仅当文件有音频流时）
	if info.AudioBitrate == "" && info.AudioCodec != "" && info.Bitrate != "" && videoBitrateBps > 0 {
		if total, err := strconv.ParseInt(info.Bitrate, 10, 64); err == nil {
			audioEst := total - videoBitrateBps
			if audioEst > 0 && audioEst < total {
				info.AudioBitrate = strconv.FormatInt(audioEst, 10)
			}
		}
	}

	// 回退：从 format tags 中读取
	if info.AudioBitrate == "" && pr.Format.Tags != nil {
		if pr.Format.Tags["BPS"] != "" {
			info.AudioBitrate = pr.Format.Tags["BPS"]
		} else if pr.Format.Tags["audio_bitrate"] != "" {
			info.AudioBitrate = pr.Format.Tags["audio_bitrate"]
		}
	}

	info.Probed = true
	return info, nil
}

// stubProbe 降级桩数据：按扩展名和文件大小生成模拟信息。
func (f *FFprobe) stubProbe(info *model.VideoInfo) {
	ext := strings.ToLower(filepath.Ext(info.FileName))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".m4v":
		// 模拟常见视频参数
		info.Duration = float64(info.Size) / 1_000_000 // 粗略估算
		if info.Duration < 1 {
			info.Duration = 1
		}
		info.Resolution = "unknown"
		info.Codec = "unknown"
		info.Fps = "unknown"
		info.AudioCodec = "unknown"
		info.Probed = false // 标记为未真实提取
	default:
		// 非视频扩展名
		info.Probed = false
	}
}

// probeResult ffprobe JSON 输出结构。
type probeResult struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	Index            int               `json:"index"`
	CodecName        string            `json:"codec_name"`
	CodecLongName    string            `json:"codec_long_name"`
	CodecType        string            `json:"codec_type"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	RFrameRate       string            `json:"r_frame_rate"`
	BitRate          string            `json:"bit_rate"`
	SampleRate       string            `json:"sample_rate"`
	Channels         int               `json:"channels"`
	ChannelLayout    string            `json:"channel_layout"`
	BitsPerRawSample interface{}       `json:"bits_per_raw_sample"`
	Tags             map[string]string `json:"tags"`
}

type probeFormat struct {
	Duration  string            `json:"duration"`
	BitRate   string            `json:"bit_rate"`
	NbStreams int               `json:"nb_streams"`
	Tags      map[string]string `json:"tags"`
}

// ===== 辅助函数 =====

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
