package ffmpeg

// RenderEDL 命令构造器（设计文档 05 §3~§6、§7）
//
// 设计原则（05 §4.1 构造器硬约束）：
//  1. 只接收结构化载荷，任何用户字符串都不做 shell 拼接；
//  2. 输出 []string 参数数组，禁止返回 shell 字符串；
//  3. 所有路径参数经校验后进入数组；
//  4. 构造器内部对每个 clip 做断言，失败返回带 index 的错误。
//
// 本文件与既有 ffmpeg.go 共用同一个 package：直接复用 queryEncoders/hasEncoder/
// DetectHardwareAccel/cfg.FFmpegPath，不重复实现硬件探测。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
)

// ============================================================
// 渲染路径常量（05 §3.2）
// ============================================================

const (
	ModeCopyAll             = "copy_all"                   // P-A 零重编码
	ModeSegmentCopyFinal    = "segment_copy_final_encode"  // P-B 默认
	ModeSegmentConcatFilter = "segment_copy_concat_filter" // P-C 混源
)

// 阶段名（05 §6.1 权重表；与 03 §6 stage 字段枚举一致）
const (
	StagePrepare  = "prepare"
	StageSegment  = "segment"
	StageConcat   = "concat"
	StageMux      = "mux"
	StageFinalize = "finalize"
)

// 阶段权重（05 §6.1）；可在后续版本由 Settings.progressWeights 覆盖
var weightPB = map[string]float64{
	StagePrepare: 2, StageSegment: 8, StageConcat: 5, StageMux: 83, StageFinalize: 2,
}
var weightPA = map[string]float64{
	StagePrepare: 5, StageSegment: 60, StageConcat: 30, StageFinalize: 5,
}

const (
	segNameFormat    = "seg_%03d.mp4"
	mergedName       = "merged.mp4"
	finalName        = "final.mp4"
	concatListName   = "concat.txt"
	maxClipsPerBatch = 32 // 05 §4.3：滤镜图分批阈值
)

// ============================================================
// 硬件能力（05 §5.2）
// ============================================================

// HardwareCaps 硬件编码能力探测结果（复用 ffmpeg.go 的探测与编码器查询）
type HardwareCaps struct {
	Accel      HardwareAccelType
	FFmpegPath string
}

// DetectHardwareCaps 探测当前渲染机的硬件加速与 ffmpeg 路径
func DetectHardwareCaps() *HardwareCaps {
	path := ""
	if c := config.Get(); c != nil {
		path = c.FFmpegPath
	}
	return &HardwareCaps{
		Accel:      DetectHardwareAccel(),
		FFmpegPath: path,
	}
}

// Has 判断当前 ffmpeg 是否具备指定编码器
func (c *HardwareCaps) Has(codec string) bool {
	if c == nil || c.FFmpegPath == "" {
		return false
	}
	return hasEncoder(c.FFmpegPath, codec)
}

// ============================================================
// 媒体探测（05 §3.1 判定数据来源）
// ============================================================

// MediaInfo 单个素材的探测结果
type MediaInfo struct {
	Path        string
	DurationMs  int64
	BitrateBps  int64
	VideoCodec  string
	VProfile    string
	VLevel      int
	Width       int
	Height      int
	RFrameRate  string // 原样保留（如 "30000/1001"），同源判定要求完全相等
	PixFmt      string
	TimeBase    string
	HasVideo    bool
	HasAudio    bool
	AudioCodec  string
	AudioRate   int
	AudioChans  int
	KeyframesMs []int64 // 可选：关键帧时间戳（精度归一化用，缺失时按 0 偏移处理）
}

type ffprobeStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Profile    string `json:"profile"`
	Level      int    `json:"level"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
	PixFmt     string `json:"pix_fmt"`
	TimeBase   string `json:"time_base"`
	SampleRate string `json:"sample_rate"`
	Channels   int    `json:"channels"`
}

type ffprobeResult struct {
	Streams []ffprobeStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
}

// ffprobePath 由 ffmpeg 路径推导 ffprobe 路径（同目录同名替换）
func ffprobePath(ffmpegPath string) string {
	if v := os.Getenv("FVCS_FFPROBE_PATH"); v != "" {
		return v
	}
	if ffmpegPath == "" {
		return "ffprobe"
	}
	dir := filepath.Dir(ffmpegPath)
	base := filepath.Base(ffmpegPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if strings.Contains(strings.ToLower(name), "ffmpeg") {
		name = strings.Replace(name, "ffmpeg", "ffprobe", 1)
	} else {
		name = "ffprobe"
	}
	return filepath.Join(dir, name+ext)
}

// ProbeMedia 调用 ffprobe 获取素材元数据（失败返回错误，由调用方映射 E_ASSET_MISSING / E_FFMPEG_MISSING）
func ProbeMedia(ffmpegPath, mediaUNC string) (*MediaInfo, error) {
	probe := ffprobePath(ffmpegPath)
	if _, err := os.Stat(probe); err != nil {
		return nil, fmt.Errorf("%s: ffprobe 不存在", protocol.ErrCodeFFmpegMissing)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, probe,
		"-hide_banner", "-v", "error",
		"-print_format", "json",
		"-show_streams", "-show_format",
		mediaUNC,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe 执行失败: %w", err)
	}
	var r ffprobeResult
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("ffprobe 输出解析失败: %w", err)
	}
	mi := &MediaInfo{Path: mediaUNC}
	if d, err := strconv.ParseFloat(r.Format.Duration, 64); err == nil {
		mi.DurationMs = int64(d*1000 + 0.5)
	}
	if br, err := strconv.ParseInt(r.Format.BitRate, 10, 64); err == nil {
		mi.BitrateBps = br
	}
	for _, s := range r.Streams {
		switch s.CodecType {
		case "video":
			if mi.HasVideo {
				continue // 取首个视频流
			}
			mi.HasVideo = true
			mi.VideoCodec = s.CodecName
			mi.VProfile = s.Profile
			mi.VLevel = s.Level
			mi.Width = s.Width
			mi.Height = s.Height
			mi.RFrameRate = s.RFrameRate
			mi.PixFmt = s.PixFmt
			mi.TimeBase = s.TimeBase
		case "audio":
			if mi.HasAudio {
				continue // 取首个音频流
			}
			mi.HasAudio = true
			mi.AudioCodec = s.CodecName
			mi.AudioRate, _ = strconv.Atoi(s.SampleRate)
			mi.AudioChans = s.Channels
		}
	}
	if !mi.HasVideo {
		return nil, fmt.Errorf("素材无视频流: %s", mediaUNC)
	}
	return mi, nil
}

// SameSignature 同源参数判定（05 §3.1）；返回不一致原因（空串表示一致）
func (m *MediaInfo) SameSignature(o *MediaInfo) string {
	switch {
	case m.VideoCodec != o.VideoCodec:
		return fmt.Sprintf("视频编码不一致（%s vs %s）", m.VideoCodec, o.VideoCodec)
	case m.VProfile != o.VProfile:
		return fmt.Sprintf("编码 profile 不一致（%s vs %s）", m.VProfile, o.VProfile)
	case m.Width != o.Width || m.Height != o.Height:
		return fmt.Sprintf("分辨率不一致（%dx%d vs %dx%d）", m.Width, m.Height, o.Width, o.Height)
	case m.RFrameRate != o.RFrameRate:
		return fmt.Sprintf("帧率不一致（%s vs %s）", m.RFrameRate, o.RFrameRate)
	case m.PixFmt != o.PixFmt:
		return fmt.Sprintf("像素格式不一致（%s vs %s）", m.PixFmt, o.PixFmt)
	case m.TimeBase != o.TimeBase:
		// 05 §3.1 标注为"宽松"条件；实现取保守口径：时间基不一致时放弃 concat demuxer
		return fmt.Sprintf("时间基不一致（%s vs %s）", m.TimeBase, o.TimeBase)
	case m.HasAudio != o.HasAudio:
		return "音轨存在性不一致"
	case m.HasAudio && m.AudioCodec != "aac":
		return fmt.Sprintf("音轨编码非 aac（%s）", m.AudioCodec)
	case m.HasAudio && o.AudioCodec != "aac":
		return fmt.Sprintf("音轨编码非 aac（%s）", o.AudioCodec)
	case m.HasAudio && m.AudioRate != o.AudioRate:
		return fmt.Sprintf("音频采样率不一致（%d vs %d）", m.AudioRate, o.AudioRate)
	case m.HasAudio && m.AudioChans != o.AudioChans:
		return fmt.Sprintf("音频声道数不一致（%d vs %d）", m.AudioChans, o.AudioChans)
	}
	return ""
}

// ============================================================
// Profile 表与硬编协商（05 §5）
// ============================================================

// ProfileSpec 单个方案的可执行参数
type ProfileSpec struct {
	Key       string
	Codec     string
	VideoArgs []string // 含 -c:v
	TagV      string   // "-tag:v hvc1"，无则空
	Hardware  bool     // 是否为硬件编码器
	ProxyH    int      // >0 表示代理用途，附加 scale=-2:H
	EstKbps   int64    // 估算码率（空间预检用）
	Degraded  bool     // 是否由降级产生
}

var profileTable = map[string]ProfileSpec{
	protocol.PresetCopySameSource: {Key: protocol.PresetCopySameSource, Codec: "copy", EstKbps: 0},
	protocol.PresetH264NVENCP5: {
		Key: protocol.PresetH264NVENCP5, Codec: "h264_nvenc", Hardware: true, EstKbps: 12000,
		VideoArgs: []string{"-c:v", "h264_nvenc", "-rc", "vbr", "-cq", "18", "-preset", "p5"},
	},
	protocol.PresetH264NVENCP7: {
		Key: protocol.PresetH264NVENCP7, Codec: "h264_nvenc", Hardware: true, EstKbps: 16000,
		VideoArgs: []string{"-c:v", "h264_nvenc", "-rc", "vbr", "-cq", "16", "-preset", "p7"},
	},
	protocol.PresetHEVCNVENCP5: {
		Key: protocol.PresetHEVCNVENCP5, Codec: "hevc_nvenc", Hardware: true, EstKbps: 8000, TagV: "hvc1",
		VideoArgs: []string{"-c:v", "hevc_nvenc", "-rc", "vbr", "-cq", "20", "-preset", "p5"},
	},
	protocol.PresetH264QSVBalanced: {
		Key: protocol.PresetH264QSVBalanced, Codec: "h264_qsv", Hardware: true, EstKbps: 12000,
		VideoArgs: []string{"-c:v", "h264_qsv", "-global_quality", "20", "-preset", "medium"},
	},
	protocol.PresetHEVCQSVBalanced: {
		Key: protocol.PresetHEVCQSVBalanced, Codec: "hevc_qsv", Hardware: true, EstKbps: 8000, TagV: "hvc1",
		VideoArgs: []string{"-c:v", "hevc_qsv", "-global_quality", "22", "-preset", "medium"},
	},
	protocol.PresetH264AMFBalanced: {
		Key: protocol.PresetH264AMFBalanced, Codec: "h264_amf", Hardware: true, EstKbps: 12000,
		VideoArgs: []string{"-c:v", "h264_amf", "-quality", "balanced", "-rc", "cqp", "-qp_i", "20", "-qp_p", "22"},
	},
	protocol.PresetLibx264Medium: {
		Key: protocol.PresetLibx264Medium, Codec: "libx264", EstKbps: 12000,
		VideoArgs: []string{"-c:v", "libx264", "-crf", "18", "-preset", "medium"},
	},
	protocol.PresetLibx264Slow: {
		Key: protocol.PresetLibx264Slow, Codec: "libx264", EstKbps: 16000,
		VideoArgs: []string{"-c:v", "libx264", "-crf", "16", "-preset", "slow"},
	},
	protocol.PresetLibx265Medium: {
		Key: protocol.PresetLibx265Medium, Codec: "libx265", EstKbps: 8000, TagV: "hvc1",
		VideoArgs: []string{"-c:v", "libx265", "-crf", "20", "-preset", "medium"},
	},
	protocol.PresetProxy720pH264: {
		Key: protocol.PresetProxy720pH264, Codec: "libx264", EstKbps: 2500, ProxyH: 720,
		VideoArgs: []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "23"},
	},
	protocol.PresetProxy720pNVENC: {
		Key: protocol.PresetProxy720pNVENC, Codec: "h264_nvenc", Hardware: true, EstKbps: 2500, ProxyH: 720,
		VideoArgs: []string{"-c:v", "h264_nvenc", "-cq", "23", "-preset", "p5"},
	},
}

// resolveProfile 解析方案并做硬编降级（05 §5.2）
// 返回：有效方案、是否发生降级、降级原因、错误（仅 presetKey 非法时非空）
func resolveProfile(presetKey string, caps *HardwareCaps) (ProfileSpec, bool, string, error) {
	spec, ok := profileTable[presetKey]
	if !ok {
		return ProfileSpec{}, false, "", fmt.Errorf("%s: presetKey %q 不在枚举表", protocol.ErrCodeProfileInvalid, presetKey)
	}
	if spec.Codec == "copy" || !spec.Hardware {
		return spec, false, "", nil
	}
	if caps != nil && caps.Has(spec.Codec) {
		return spec, false, "", nil
	}
	// 指定硬编不可用 → 降级（hevc 系列降到 libx265，其余降到 libx264）
	fallbackKey := protocol.PresetLibx264Medium
	if strings.HasPrefix(spec.Codec, "hevc") {
		fallbackKey = protocol.PresetLibx265Medium
	}
	fb := profileTable[fallbackKey]
	fb.Degraded = true
	reason := fmt.Sprintf("未检测到 %s 编码器，已降级为 %s（软件编码）", spec.Codec, fb.Codec)
	logger.Warn("render", "%s", reason)
	return fb, true, reason, nil
}

// resolveEncoder 文档契约函数（05 §5.2）
func resolveEncoder(presetKey string, caps *HardwareCaps) (string, bool) {
	spec, fb, _, err := resolveProfile(presetKey, caps)
	if err != nil {
		return "libx264", true
	}
	return spec.Codec, fb
}

// ============================================================
// 渲染计划（05 §4.1）
// ============================================================

// SegmentPlan 单片段渲染计划
type SegmentPlan struct {
	Index      int
	SrcUNC     string
	In         time.Duration
	Out        time.Duration
	SeekStart  time.Duration // 实际 -ss 起点（= In - HeadOffset）
	HeadOffset time.Duration // 段头多余内容（精度归一化口径，05 §4.6）
	HasAudio   bool
	AudioCopy  bool
	SegPath    string
	Args       []string
}

// RenderStage 可执行阶段（对 05 §4.1 的扩展：显式给出执行顺序与权重，
// 便于 task 层按 05 §6.1 推进进度；文档既有字段仍全部保留且语义不变）
type RenderStage struct {
	Name       string
	Args       []string
	Output     string
	WeightPct  float64
	ExpectedMs int64 // 该阶段预期输出时长（进度分母；prepare/finalize 为 0）
}

// RenderPlan 三阶段渲染计划
type RenderPlan struct {
	Mode           string
	WorkDir        string
	Segments       []SegmentPlan
	ConcatList     string
	ConcatArgs     []string
	FinalArgs      []string
	EstOutputBytes int64

	// 下面为执行所需的补充信息（文档 05 §4.1 字段的超集）
	Stages             []RenderStage
	OutputPath         string // 成品 UNC
	TempOutput         string // 阶段三落盘路径（workDir 内），finalize 阶段复制到 OutputPath
	FinalFrom          string // finalize 源（copy_all 为 merged.mp4，其余为 TempOutput）
	TotalMs            int64
	EffectivePresetKey string
	Fallback           bool
	DegradedToFilter   bool
	Warnings           []string
}

// ============================================================
// 主入口：构造三阶段命令（05 §4.1）
// ============================================================

// BuildRenderEDLCommands 依据结构化载荷构造渲染计划。
//   - p        : RenderEDL 载荷（已通过 protocol.ValidateRenderEDLPayload）
//   - srcUNC   : 素材相对路径 → UNC 绝对路径（pkg/smb.BuildSMBPath 包装）
//   - outUNC   : 成品 UNC 绝对路径
//   - workDir  : 中间产物目录（05 §4.5 决策由调用方负责）
//   - caps     : 硬件能力（nil 时按无硬编处理）
//   - probes   : 已缓存的 ffprobe 结果；缺失的条目由本函数内部探测
func BuildRenderEDLCommands(
	p *protocol.RenderTaskPayload,
	srcUNC func(relFile string) (string, error),
	outUNC string,
	workDir string,
	caps *HardwareCaps,
	probes map[string]*MediaInfo,
) (*RenderPlan, error) {

	// ---- 0. 载荷白名单校验（04 §4.1 约束 4 / 03 §5.2）----
	if err := protocol.ValidateRenderEDLPayload(p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(workDir) == "" {
		return nil, fmt.Errorf("%s: workDir 为空", protocol.ErrCodePayloadInvalid)
	}

	plan := &RenderPlan{
		WorkDir:    workDir,
		OutputPath: outUNC,
		TotalMs:    p.TotalMs,
	}
	ffmpegPath := ""
	if caps != nil {
		ffmpegPath = caps.FFmpegPath
	}

	// ---- 1. 逐 clip 解析路径 + 探测（05 §3.3 步骤 A/B）----
	infos := make([]*MediaInfo, len(p.Clips))
	for i, c := range p.Clips {
		unc, err := srcUNC(c.File)
		if err != nil {
			return nil, &protocol.PayloadError{Code: protocol.ErrCodeAssetMissing, Msg: "素材路径解析失败: " + err.Error(), Field: "file", Index: i}
		}
		mi := probes[c.File]
		if mi == nil {
			probed, perr := ProbeMedia(ffmpegPath, unc)
			if perr != nil {
				code := protocol.ErrCodeAssetMissing
				if strings.Contains(perr.Error(), protocol.ErrCodeFFmpegMissing) {
					code = protocol.ErrCodeFFmpegMissing
				}
				return nil, &protocol.PayloadError{Code: code, Msg: fmt.Sprintf("素材不可读（%s）: %v", c.File, perr), Field: "file", Index: i}
			}
			mi = probed
		}
		infos[i] = mi

		// 入点/出点必须落在素材时长内（03 §2.5）
		outMs, _ := protocol.ParseTimecode(c.Out)
		if mi.DurationMs > 0 && outMs > mi.DurationMs+100 {
			return nil, &protocol.PayloadError{
				Code: protocol.ErrCodeEDLInvalid, Field: "out", Index: i,
				Msg: fmt.Sprintf("out=%s 超出源时长 %s", c.Out, protocol.FormatMs(mi.DurationMs)),
			}
		}
	}

	// ---- 2. 同源判定 + 路径决策（05 §3.2 / §3.3）----
	sameAll := true
	degradeReason := ""
	for i := 1; i < len(infos); i++ {
		if reason := infos[0].SameSignature(infos[i]); reason != "" {
			sameAll = false
			degradeReason = fmt.Sprintf("片段 %d 与片段 0 不同源：%s", i, reason)
			break
		}
	}

	needAudio := false
	for _, mi := range infos {
		if mi.HasAudio {
			needAudio = true
			break
		}
	}
	if p.Timeline.Audio && !needAudio {
		plan.Warnings = append(plan.Warnings, "所有素材均无音轨，输出将不含音频流")
	}

	mode := ModeSegmentCopyFinal
	presetKey := p.Profile.PresetKey

	if presetKey == protocol.PresetCopySameSource {
		if sameAll {
			mode = ModeCopyAll
		} else {
			// D4 降级原则：不报错，自动转为转码路径并在消息中说明
			presetKey = protocol.PresetH264NVENCP5
			mode = ModeSegmentCopyFinal
			plan.Warnings = append(plan.Warnings,
				"所选“不重编码”要求全部片段同源，实际不满足："+degradeReason+"；已自动降级为转码导出")
		}
	} else if !sameAll {
		mode = ModeSegmentConcatFilter
		plan.Warnings = append(plan.Warnings, "素材不同源（"+degradeReason+"），走混源拼接路径（单次转码）")
	}

	// ---- 3. Profile 硬编协商（05 §5.2）----
	spec, fb, fbReason, err := resolveProfile(presetKey, caps)
	if err != nil {
		return nil, &protocol.PayloadError{Code: protocol.ErrCodeProfileInvalid, Field: "profile.presetKey", Msg: err.Error(), Index: -1}
	}
	plan.EffectivePresetKey = spec.Key
	plan.Fallback = fb
	if fb && fbReason != "" {
		plan.Warnings = append(plan.Warnings, fbReason)
	}
	if mode == ModeSegmentConcatFilter && spec.Codec == "copy" {
		spec = profileTable[protocol.PresetLibx264Medium]
		plan.EffectivePresetKey = spec.Key
	}

	// ---- 4. 分段计划（05 §4.2 / §4.6）----
	headOffsets := make([]time.Duration, len(p.Clips))
	var sumHead time.Duration
	for i, c := range p.Clips {
		inMs, _ := protocol.ParseTimecode(c.In)
		outMs, _ := protocol.ParseTimecode(c.Out)
		seg := SegmentPlan{
			Index:    i,
			SrcUNC:   infos[i].Path,
			In:       time.Duration(inMs) * time.Millisecond,
			Out:      time.Duration(outMs) * time.Millisecond,
			HasAudio: infos[i].HasAudio,
			SegPath:  filepath.Join(workDir, fmt.Sprintf(segNameFormat, i)),
		}
		// 精度归一化：段头关键帧偏移（无关键帧数据时按 0 处理）
		if kf := keyframeBefore(infos[i].KeyframesMs, inMs); kf >= 0 && kf < inMs {
			seg.HeadOffset = time.Duration(inMs-kf) * time.Millisecond
		}
		seg.SeekStart = seg.In - seg.HeadOffset
		seg.AudioCopy = seg.HasAudio && infos[i].AudioCodec == "aac" && mode != ModeSegmentConcatFilter
		headOffsets[i] = seg.HeadOffset
		sumHead += seg.HeadOffset
		plan.Segments = append(plan.Segments, seg)
	}

	// 归一化偏差收敛：转码路径下若累计偏差 > 100ms，改用滤镜裁切路径（05 §4.6 方案 B）
	if mode != ModeCopyAll && sumHead > 100*time.Millisecond {
		mode = ModeSegmentConcatFilter
		plan.DegradedToFilter = true
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("段头累计偏移 %dms 超过 100ms，改用滤镜裁切归一化", sumHead.Milliseconds()))
		for i := range plan.Segments {
			plan.Segments[i].AudioCopy = false
		}
		if spec.Codec == "copy" {
			spec = profileTable[protocol.PresetLibx264Medium]
			plan.EffectivePresetKey = spec.Key
		}
	}

	// ---- 5. 生成阶段一命令（05 §4.2）----
	weights := weightPB
	if mode == ModeCopyAll {
		weights = weightPA
	}
	segWeightTotal := weights[StageSegment]
	for i := range plan.Segments {
		seg := &plan.Segments[i]
		seg.Args = buildSegmentArgs(seg, p, mode, needAudio, infos[i])
		dur := float64(seg.Out-seg.In) / float64(time.Millisecond)
		total := float64(p.TotalMs)
		w := 0.0
		if total > 0 {
			w = segWeightTotal * dur / total
		}
		plan.Stages = append(plan.Stages, RenderStage{
			Name:       StageSegment,
			Args:       seg.Args,
			Output:     seg.SegPath,
			WeightPct:  w,
			ExpectedMs: (seg.Out - seg.SeekStart).Milliseconds(),
		})
	}

	// ---- 6. 阶段二/三（05 §4.3 / §4.4）----
	switch mode {
	case ModeCopyAll, ModeSegmentCopyFinal:
		merged := filepath.Join(workDir, mergedName)
		plan.ConcatList = buildConcatList(plan.Segments)
		plan.ConcatArgs = buildConcatArgs(workDir, concatListName, merged)
		plan.Stages = append(plan.Stages, RenderStage{
			Name: StageConcat, Args: plan.ConcatArgs, Output: merged,
			WeightPct: weights[StageConcat], ExpectedMs: p.TotalMs,
		})
		if mode == ModeSegmentCopyFinal {
			plan.TempOutput = filepath.Join(workDir, finalName)
			plan.FinalArgs = buildFinalArgs(p, spec, infos[0], merged, plan.TempOutput, true)
			plan.FinalFrom = plan.TempOutput
			plan.Stages = append(plan.Stages, RenderStage{
				Name: StageMux, Args: plan.FinalArgs, Output: plan.TempOutput,
				WeightPct: weights[StageMux], ExpectedMs: p.TotalMs,
			})
		} else {
			plan.FinalFrom = merged // copy_all 直接改名落盘
		}

	case ModeSegmentConcatFilter:
		plan.TempOutput = filepath.Join(workDir, finalName)
		plan.FinalFrom = plan.TempOutput
		graph, err := buildFilterGraph(p, plan.Segments, needAudio, headOffsets)
		if err != nil {
			return nil, &protocol.PayloadError{Code: protocol.ErrCodeEDLInvalid, Msg: err.Error(), Index: -1}
		}
		plan.FinalArgs = buildConcatFilterArgs(p, spec, plan.Segments, graph, plan.TempOutput)
		plan.Stages = append(plan.Stages, RenderStage{
			Name: StageMux, Args: plan.FinalArgs, Output: plan.TempOutput,
			WeightPct: weights[StageConcat] + weights[StageMux], ExpectedMs: p.TotalMs,
		})
	}

	plan.Stages = append(plan.Stages, RenderStage{
		Name: StageFinalize, Args: nil, Output: outUNC, WeightPct: weights[StageFinalize],
	})
	// prepare 阶段（05 §6.1）前置于执行序列：建中间目录、写 concat 清单、磁盘空间预检
	plan.Stages = append([]RenderStage{{
		Name: StagePrepare, Output: workDir, WeightPct: weights[StagePrepare],
	}}, plan.Stages...)
	plan.Mode = mode
	plan.EstOutputBytes = estimateOutputBytes(p, plan.Segments, infos, mode, spec)
	return plan, nil
}

// keyframeBefore 从关键帧列表中找到 <= atMs 的最后一个关键帧；找不到返回 -1
func keyframeBefore(kfs []int64, atMs int64) int64 {
	best := int64(-1)
	for _, k := range kfs {
		if k <= atMs && k > best {
			best = k
		}
	}
	return best
}

// ============================================================
// 命令拼装
// ============================================================

func baseArgs() []string {
	return []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "warning", "-progress", "pipe:1"}
}

// buildSegmentArgs 阶段一：分段截取（05 §4.2）
func buildSegmentArgs(seg *SegmentPlan, p *protocol.RenderTaskPayload, mode string, needAudio bool, mi *MediaInfo) []string {
	args := baseArgs()
	args = append(args, "-ss", protocol.FormatMs(seg.SeekStart.Milliseconds()))
	args = append(args, "-t", protocol.FormatMs((seg.Out - seg.SeekStart).Milliseconds()))
	args = append(args, "-i", seg.SrcUNC)

	silentFill := needAudio && !seg.HasAudio
	if silentFill {
		args = append(args, "-f", "lavfi", "-i",
			fmt.Sprintf("anullsrc=channel_layout=stereo:sample_rate=%d", audioSampleRate(p)))
	}

	args = append(args, "-map", "0:v:0")
	switch {
	case !needAudio:
		args = append(args, "-an")
	case silentFill:
		args = append(args, "-map", "1:a:0")
	case seg.AudioCopy:
		args = append(args, "-map", "0:a:0", "-c:a", "copy")
	default:
		args = append(args, "-map", "0:a:0")
		args = append(args, audioEncodeArgs(p, true)...)
	}

	args = append(args, "-c:v", "copy")
	if silentFill {
		// 05 §4.2：lavfi anullsrc 为裸 PCM，必须显式指定 aac 编码器
		args = append(args, audioEncodeArgs(p, true)...)
		args = append(args, "-shortest")
	}
	args = append(args, "-avoid_negative_ts", "make_zero", "-fflags", "+genpts", "-movflags", "+faststart")
	args = append(args, seg.SegPath)
	return args
}

func audioSampleRate(p *protocol.RenderTaskPayload) int {
	if p.Timeline.SampleRate > 0 {
		return p.Timeline.SampleRate
	}
	return 48000
}

// audioEncodeArgs 音频统一编码参数（05 §5.1：-c:a aac -b:a 192k -ar 48000 -ac 2）
func audioEncodeArgs(p *protocol.RenderTaskPayload, includeCodec bool) []string {
	bitrate := p.Profile.Audio.Bitrate
	if bitrate == "" {
		bitrate = "192k"
	}
	ch := p.Profile.Audio.Channels
	if ch <= 0 {
		ch = 2
	}
	out := []string{}
	if includeCodec {
		out = append(out, "-c:a", "aac")
	}
	return append(out, "-b:a", bitrate, "-ar", strconv.Itoa(audioSampleRate(p)), "-ac", strconv.Itoa(ch))
}

// buildConcatList 生成 concat demuxer 清单（05 §4.3）
func buildConcatList(segs []SegmentPlan) string {
	var sb strings.Builder
	for _, s := range segs {
		sb.WriteString("file '")
		sb.WriteString(escapeConcatPath(s.SegPath))
		sb.WriteString("'\n")
	}
	return sb.String()
}

// escapeConcatPath 清单路径：反斜杠转 '/', 单引号转义（05 §4.3）
func escapeConcatPath(p string) string {
	return strings.ReplaceAll(strings.ReplaceAll(p, "\\", "/"), "'", `'\''`)
}

func buildConcatArgs(workDir, listName, out string) []string {
	args := baseArgs()
	args = append(args, "-f", "concat", "-safe", "0",
		"-i", filepath.Join(workDir, listName),
		"-c", "copy", "-avoid_negative_ts", "make_zero", "-movflags", "+faststart")
	return append(args, out)
}

// buildFinalArgs 阶段三：按 profile 单次转码导出（05 §4.4）
func buildFinalArgs(p *protocol.RenderTaskPayload, spec ProfileSpec, src *MediaInfo, in, out string, fromDemuxer bool) []string {
	args := baseArgs()
	args = append(args, "-i", in)
	args = append(args, "-map", "0:v:0")
	if p.Timeline.Audio {
		args = append(args, "-map", "0:a:0")
	}
	args = append(args, spec.VideoArgs...)
	args = append(args, "-pix_fmt", pixFmt(p))
	args = append(args, "-fps_mode", "cfr", "-r", strconv.Itoa(p.Timeline.FPS))
	if spec.TagV != "" {
		args = append(args, "-tag:v", spec.TagV)
	}
	if vf := scaleFilterIfNeeded(src, p, fromDemuxer); vf != "" {
		args = append(args, "-vf", vf)
	}
	if p.Timeline.Audio || hasAudioStream(p, fromDemuxer) {
		args = append(args, audioEncodeArgs(p, true)...)
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-t", protocol.FormatMs(p.TotalMs))
	args = append(args, "-map_metadata", "-1", "-movflags", "+faststart", "-max_muxing_queue_size", "2048")
	return append(args, out)
}

// hasAudioStream P-B 路径下 merged.mp4 的音轨由分段阶段决定：
// 只要时间轴要求音频或任一素材带音轨，merged 就带音轨
func hasAudioStream(p *protocol.RenderTaskPayload, _ bool) bool {
	return p.Timeline.Audio
}

func pixFmt(p *protocol.RenderTaskPayload) string {
	if p.Profile.Video.PixFmt != "" {
		return p.Profile.Video.PixFmt
	}
	return "yuv420p"
}

// scaleFilterIfNeeded P-B 路径下仅在源分辨率 ≠ 目标时附加缩放（05 §4.4）
func scaleFilterIfNeeded(src *MediaInfo, p *protocol.RenderTaskPayload, fromDemuxer bool) string {
	if !fromDemuxer || src == nil {
		return ""
	}
	if src.Width == p.Timeline.Width && src.Height == p.Timeline.Height {
		return ""
	}
	return scalePadFilter(p.Timeline.Width, p.Timeline.Height)
}

func scalePadFilter(w, h int) string {
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1",
		w, h, w, h)
}

// buildConcatFilterArgs P-C：filter_complex concat 单次转码，直接产出成品（05 §4.3）
func buildConcatFilterArgs(p *protocol.RenderTaskPayload, spec ProfileSpec, segs []SegmentPlan, graph, out string) []string {
	args := baseArgs()
	for _, s := range segs {
		args = append(args, "-i", s.SegPath)
	}
	args = append(args, "-filter_complex", graph, "-map", "[v]")
	if p.Timeline.Audio {
		args = append(args, "-map", "[a]")
	}
	args = append(args, spec.VideoArgs...)
	args = append(args, "-pix_fmt", pixFmt(p))
	args = append(args, "-fps_mode", "cfr", "-r", strconv.Itoa(p.Timeline.FPS))
	if spec.TagV != "" {
		args = append(args, "-tag:v", spec.TagV)
	}
	if p.Timeline.Audio {
		args = append(args, audioEncodeArgs(p, true)...)
	}
	args = append(args, "-t", protocol.FormatMs(p.TotalMs))
	args = append(args, "-map_metadata", "-1", "-movflags", "+faststart", "-max_muxing_queue_size", "2048")
	return append(args, out)
}

// buildFilterGraph 逐片段生成滤镜链（05 §4.3）
//   - 片段数 > 32 时分批 concat，避免滤镜图过大
//   - headOffsets > 0 时插入 trim/atrim 做精度归一化
func buildFilterGraph(p *protocol.RenderTaskPayload, segs []SegmentPlan, needAudio bool, headOffsets []time.Duration) (string, error) {
	n := len(segs)
	if n == 0 {
		return "", fmt.Errorf("片段数为 0")
	}
	fps := strconv.Itoa(p.Timeline.FPS)
	sr := strconv.Itoa(audioSampleRate(p))

	var chains []string
	batchV := []string{}
	batchA := []string{}
	batch := 0

	flush := func() (int, error) {
		if len(batchV) == 0 {
			return batch, nil
		}
		label := "v"
		alabel := "a"
		if batch > 0 || n > maxClipsPerBatch {
			label = fmt.Sprintf("vb%d", batch)
			alabel = fmt.Sprintf("ab%d", batch)
		}
		if needAudio {
			// concat 滤镜要求输入按片段交错排列（v0,a0,v1,a1,...），
			// 不能把全部视频标签排在全部音频标签之前，否则链接报
			// "Media type mismatch ... concat input pad N (audio)"。
			chains = append(chains, fmt.Sprintf(
				"%sconcat=n=%d:v=1:a=1[%s][%s]",
				joinInterleaved(batchV, batchA), len(batchV), label, alabel))
		} else {
			chains = append(chains, fmt.Sprintf("%sconcat=n=%d:v=1:a=0[%s]",
				joinLabels(batchV), len(batchV), label))
		}
		batch++
		batchV = nil
		batchA = nil
		return batch, nil
	}

	for i := range segs {
		v := fmt.Sprintf("[%d:v]", i)
		a := fmt.Sprintf("[%d:a]", i)
		if headOffsets != nil && int64(headOffsets[i]) > 0 {
			off := strconv.FormatFloat(float64(headOffsets[i])/float64(time.Second), 'f', 3, 64)
			v += fmt.Sprintf("trim=start=%s,setpts=PTS-STARTPTS,", off)
			a += fmt.Sprintf("atrim=start=%s,asetpts=PTS-STARTPTS,", off)
		}
		// 注：scalePadFilter 已含 setsar=1，此处不再重复追加（历史冗余）
		v += fmt.Sprintf("%s,fps=%s[v%d]", scalePadFilter(p.Timeline.Width, p.Timeline.Height), fps, i)
		chains = append(chains, v)
		batchV = append(batchV, fmt.Sprintf("[v%d]", i))

		if needAudio {
			a += fmt.Sprintf("aformat=sample_fmts=fltp:sample_rates=%s:channel_layouts=stereo[a%d]", sr, i)
			chains = append(chains, a)
			batchA = append(batchA, fmt.Sprintf("[a%d]", i))
		}

		if len(batchV) == maxClipsPerBatch || i == n-1 {
			if _, err := flush(); err != nil {
				return "", err
			}
		}
	}

	// 多批次：再做一次批次间 concat
	if batch > 1 {
		var bv, ba []string
		for k := 0; k < batch; k++ {
			bv = append(bv, fmt.Sprintf("[vb%d]", k))
			if needAudio {
				ba = append(ba, fmt.Sprintf("[ab%d]", k))
			}
		}
		if needAudio {
			chains = append(chains, fmt.Sprintf("%sconcat=n=%d:v=1:a=1[v][a]",
				joinInterleaved(bv, ba), batch))
		} else {
			chains = append(chains, fmt.Sprintf("%sconcat=n=%d:v=1:a=0[v]",
				joinLabels(bv), batch))
		}
	}
	return strings.Join(chains, ";"), nil
}

func joinLabels(ls []string) string {
	return strings.Join(ls, "")
}

// joinInterleaved 按片段交错拼接视频/音频标签（[v0][a0][v1][a1]...）。
// FFmpeg concat 滤镜在 v=1:a=1 时要求输入顺序为「片段0视频,片段0音频,片段1视频,片段1音频,...」，
// 若先输出全部视频标签再输出全部音频标签，链接阶段会报
// "Media type mismatch between ... (video) and ... concat ... input pad N (audio)"。
func joinInterleaved(v, a []string) string {
	if len(a) == 0 {
		return joinLabels(v)
	}
	var b strings.Builder
	for i := range v {
		b.WriteString(v[i])
		if i < len(a) {
			b.WriteString(a[i])
		}
	}
	return b.String()
}

// ============================================================
// 空间预估（05 §4.5）
// ============================================================

func estimateOutputBytes(p *protocol.RenderTaskPayload, segs []SegmentPlan, infos []*MediaInfo, mode string, spec ProfileSpec) int64 {
	var srcBytes float64
	for i, s := range segs {
		var bps float64
		if infos != nil && i < len(infos) && infos[i] != nil && infos[i].BitrateBps > 0 {
			bps = float64(infos[i].BitrateBps)
		} else {
			bps = float64(spec.EstKbps) * 1000
		}
		durSec := float64(s.Out-s.In) / float64(time.Second)
		srcBytes += durSec * bps / 8
	}
	if srcBytes <= 0 {
		srcBytes = float64(p.TotalMs) / 1000 * 4000 // 兜底：约 4MB/s
	}
	est := srcBytes * 1.6 // 中间产物系数
	if mode != ModeCopyAll {
		est *= 0.6 // 转码路径目标码率通常更低
	}
	return int64(est)
}

// ============================================================
// 代理命令构造（04 §3.3）
// ============================================================

// BuildProxyCommand 构造代理生成命令（复用同一三分类骨架，单阶段输出）
func BuildProxyCommand(srcUNC, outUNC string, t protocol.ProxyTemplate, caps *HardwareCaps) ([]string, error) {
	if t.KeyframeOnly {
		return nil, fmt.Errorf("%s: keyframeOnly 为 P1+ 预留能力，P0 未实现", protocol.ErrCodeProfileInvalid)
	}
	presetKey := t.PresetKey
	if presetKey == "" {
		presetKey = protocol.PresetProxy720pH264
	}
	spec, ok := profileTable[presetKey]
	if !ok || spec.ProxyH == 0 {
		return nil, fmt.Errorf("%s: 代理模板 presetKey 非法: %q", protocol.ErrCodeProfileInvalid, presetKey)
	}
	if spec.Hardware && (caps == nil || !caps.Has(spec.Codec)) {
		spec = profileTable[protocol.PresetProxy720pH264]
		logger.Warn("render", "未检测到 %s，代理改用 libx264", "h264_nvenc")
	}

	ffmpegPath := ""
	if caps != nil {
		ffmpegPath = caps.FFmpegPath
	}
	mi, err := ProbeMedia(ffmpegPath, srcUNC)
	if err != nil {
		return nil, err
	}

	args := baseArgs()
	args = append(args, "-i", srcUNC)

	height := t.Height
	if height <= 0 {
		height = spec.ProxyH
	}
	vf := fmt.Sprintf("scale=-2:%d", height)

	args = append(args, spec.VideoArgs...)
	if t.CRF > 0 && spec.Codec != "h264_nvenc" {
		args = append(args, "-crf", strconv.Itoa(t.CRF))
	}
	if t.Preset != "" && spec.Codec != "h264_nvenc" {
		args = append(args, "-preset", t.Preset)
	}
	args = append(args, "-vf", vf, "-pix_fmt", "yuv420p")

	// GOP：按模板 gopSeconds 与源帧率计算
	fps := frameRateFloat(mi.RFrameRate)
	if fps <= 0 {
		fps = 30
	}
	gopSec := t.GOPSec
	if gopSec <= 0 {
		gopSec = 2
	}
	gop := int(float64(gopSec) * fps)
	if gop < 1 {
		gop = 1
	}
	args = append(args, "-g", strconv.Itoa(gop), "-keyint_min", strconv.Itoa(gop), "-sc_threshold", "0")
	if t.FPSFollowSrc {
		args = append(args, "-fps_mode", "cfr", "-r", strconv.FormatFloat(fps, 'f', -1, 64))
	}

	args = append(args, "-map", "0:v:0")
	if mi.HasAudio {
		args = append(args, "-map", "0:a:0", "-c:a", "aac", "-b:a", proxyAudioBitrate(t), "-ac", "2")
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-movflags", "+faststart", "-map_metadata", "-1")
	return append(args, outUNC), nil
}

func proxyAudioBitrate(t protocol.ProxyTemplate) string {
	if t.AudioBitrate != "" {
		return t.AudioBitrate
	}
	return "128k"
}

// frameRateFloat "30000/1001" → 29.97
func frameRateFloat(r string) float64 {
	if r == "" {
		return 0
	}
	parts := strings.Split(r, "/")
	if len(parts) != 2 {
		v, _ := strconv.ParseFloat(r, 64)
		return v
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}
