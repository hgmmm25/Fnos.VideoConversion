// RenderEDL 命令构造器单元测试（对应设计文档 05 §9 T1~T4 / T7）
//
// 说明：
//   - 本文件不依赖真实 ffmpeg / 真实媒体文件：所有 MediaInfo 由测试直接构造并
//     作为 probes 传入，因此 BuildRenderEDLCommands 不会触发 ffprobe。
//   - 断言口径为“命令构造正确性”（阶段划分、参数序列、时长/编码器/滤镜），
//     对应 05 §4.2 / §4.3 / §4.4 与 §5.2 硬编协商。
package ffmpeg

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"Fnos.VC_Service/pkg/protocol"
)

// ---------------- 测试夹具 ----------------

const testSrcUNC = `\\NAS\media\videos\demo\a_01.mp4`

func srcUNCFromRel(rel string) (string, error) {
	return `\\NAS\media\` + strings.ReplaceAll(rel, "/", `\`), nil
}

// sameSignatureMI 构造与 T1/T2/T4 一致的探测结果（同源参数：H.264 High / 1920x1080 / 30fps / aac 48k）
func sameSignatureMI(path string, durMs int64, hasAudio bool) *MediaInfo {
	mi := &MediaInfo{
		Path:       path,
		DurationMs: durMs,
		BitrateBps: 8_000_000,
		VideoCodec: "h264",
		VProfile:   "High",
		VLevel:     40,
		Width:      1920,
		Height:     1080,
		RFrameRate: "30/1",
		PixFmt:     "yuv420p",
		TimeBase:   "1/15360",
		HasVideo:   true,
		HasAudio:   hasAudio,
	}
	if hasAudio {
		mi.AudioCodec = "aac"
		mi.AudioRate = 48000
		mi.AudioChans = 2
	}
	return mi
}

func clip(file, in, out string) protocol.WireClip {
	return protocol.WireClip{File: file, In: in, Out: out, Speed: 1.0}
}

// buildPayload 构造合法 RenderEDL 载荷（时间轴 1920x1080@30、含音频）
func buildPayload(presetKey string, clips []protocol.WireClip) *protocol.RenderTaskPayload {
	return &protocol.RenderTaskPayload{
		Type:      protocol.PayloadTypeRenderEDL,
		ProjectID: "proj_test",
		Output:    "demo_out.mp4",
		Timeline: protocol.Timeline{
			Width: 1920, Height: 1080, FPS: 30, SampleRate: 48000, Audio: true,
		},
		Clips: clips,
		Profile: protocol.RenderProfile{
			PresetKey: presetKey,
			Container: "mp4",
			Video:     protocol.RenderVideoProfile{Codec: "h264", CRF: 18, Preset: "medium", PixFmt: "yuv420p"},
			Audio:     protocol.RenderAudioProfile{Codec: "aac", Bitrate: "192k", Channels: 2, SampleRate: 48000},
		},
	}
}

// hasSeq 判断参数数组中是否存在连续子序列
func hasSeq(args []string, seq ...string) bool {
	if len(seq) == 0 || len(args) < len(seq) {
		return false
	}
	for i := 0; i+len(seq) <= len(args); i++ {
		ok := true
		for j := range seq {
			if args[i+j] != seq[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func joined(args []string) string { return strings.Join(args, " ") }

func stageNames(plan *RenderPlan) []string {
	out := make([]string, 0, len(plan.Stages))
	for _, s := range plan.Stages {
		out = append(out, s.Name)
	}
	return out
}

// ---------------- T1：同源素材 + copy_same_source → copy_all 全程零重编码 ----------------

func TestT1_CopyAll_SameSource_NoReencode(t *testing.T) {
	clips := []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:05.000"),
		clip("videos/a_02.mp4", "00:00:10.000", "00:00:15.000"),
		clip("videos/a_03.mp4", "00:00:20.000", "00:00:25.000"),
	}
	p := buildPayload(protocol.PresetCopySameSource, clips)
	probes := map[string]*MediaInfo{
		"videos/a_01.mp4": sameSignatureMI(`\\NAS\media\videos\a_01.mp4`, 600000, true),
		"videos/a_02.mp4": sameSignatureMI(`\\NAS\media\videos\a_02.mp4`, 600000, true),
		"videos/a_03.mp4": sameSignatureMI(`\\NAS\media\videos\a_03.mp4`, 600000, true),
	}

	plan, err := BuildRenderEDLCommands(p, srcUNCFromRel, `\\NAS\media\exports\demo_out.mp4`,
		t.TempDir(), nil, probes)
	if err != nil {
		t.Fatalf("T1 构造失败: %v", err)
	}

	if plan.Mode != ModeCopyAll {
		t.Fatalf("T1 期望 Mode=%s，实际 %s（warnings=%v）", ModeCopyAll, plan.Mode, plan.Warnings)
	}
	if plan.FinalArgs != nil {
		t.Errorf("T1 copy_all 不应产生阶段三命令，实际 %v", plan.FinalArgs)
	}
	if plan.TotalMs != 15000 {
		t.Errorf("T1 期望总时长 15000ms，实际 %d", plan.TotalMs)
	}

	// 分段：每段 -ss/-t 正确、视频流 copy、音频 copy
	if len(plan.Segments) != 3 {
		t.Fatalf("T1 期望 3 个片段，实际 %d", len(plan.Segments))
	}
	for i, seg := range plan.Segments {
		if !hasSeq(seg.Args, "-c:v", "copy") {
			t.Errorf("T1 片段 %d 未使用 -c:v copy: %s", i, joined(seg.Args))
		}
		if !hasSeq(seg.Args, "-c:a", "copy") {
			t.Errorf("T1 片段 %d 音频应为 copy（源为 aac）: %s", i, joined(seg.Args))
		}
		if !hasSeq(seg.Args, "-avoid_negative_ts", "make_zero") || !hasSeq(seg.Args, "-movflags", "+faststart") {
			t.Errorf("T1 片段 %d 缺少时间戳/封装参数: %s", i, joined(seg.Args))
		}
		if seg.Args[len(seg.Args)-1] != seg.SegPath {
			t.Errorf("T1 片段 %d 输出路径应位于参数末尾: %s", i, seg.Args[len(seg.Args)-1])
		}
	}

	// 拼接：concat demuxer 零重编码 + 清单 3 行
	if !hasSeq(plan.ConcatArgs, "-f", "concat") || !hasSeq(plan.ConcatArgs, "-safe", "0") ||
		!hasSeq(plan.ConcatArgs, "-c", "copy") {
		t.Errorf("T1 concat 参数不符合 05 §4.3: %s", joined(plan.ConcatArgs))
	}
	if got := strings.Count(strings.TrimRight(plan.ConcatList, "\n"), "\n") + 1; got != 3 {
		t.Errorf("T1 concat 清单应为 3 行，实际 %d 行: %q", got, plan.ConcatList)
	}
	for _, line := range strings.Split(strings.TrimRight(plan.ConcatList, "\n"), "\n") {
		if !strings.HasPrefix(line, "file '") || !strings.HasSuffix(line, "'") {
			t.Errorf("T1 concat 清单行格式非法: %q", line)
		}
		if strings.Contains(line, `\`) {
			t.Errorf("T1 concat 清单路径应转 '/': %q", line)
		}
	}

	// 阶段序列：prepare → segment×3 → concat → finalize（无 mux）
	want := []string{StagePrepare, StageSegment, StageSegment, StageSegment, StageConcat, StageFinalize}
	if got := stageNames(plan); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("T1 阶段序列错误：期望 %v，实际 %v", want, got)
	}
	if plan.FinalFrom == "" || !strings.HasSuffix(plan.FinalFrom, "merged.mp4") {
		t.Errorf("T1 copy_all 的 finalize 源应为 merged.mp4，实际 %q", plan.FinalFrom)
	}
}

// ---------------- T2：同源素材转码导出（含硬编不可用降级） ----------------

func TestT2_SameSource_Transcode_ProfileNegotiation(t *testing.T) {
	clips := []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:05.000"),
		clip("videos/a_02.mp4", "00:00:00.000", "00:00:05.000"),
		clip("videos/a_03.mp4", "00:00:00.000", "00:00:05.000"),
	}
	p := buildPayload(protocol.PresetH264NVENCP5, clips)
	probes := map[string]*MediaInfo{
		"videos/a_01.mp4": sameSignatureMI(`\\NAS\media\videos\a_01.mp4`, 600000, true),
		"videos/a_02.mp4": sameSignatureMI(`\\NAS\media\videos\a_02.mp4`, 600000, true),
		"videos/a_03.mp4": sameSignatureMI(`\\NAS\media\videos\a_03.mp4`, 600000, true),
	}

	workDir := t.TempDir()
	plan, err := BuildRenderEDLCommands(p, srcUNCFromRel, `\\NAS\media\exports\demo_out.mp4`, workDir, nil, probes)
	if err != nil {
		t.Fatalf("T2 构造失败: %v", err)
	}

	if plan.Mode != ModeSegmentCopyFinal {
		t.Fatalf("T2 期望 Mode=%s，实际 %s", ModeSegmentCopyFinal, plan.Mode)
	}
	// caps=nil（未检测到 NVENC）→ 必须降级为软件编码并在告警中说明（05 §5.2 / D4）
	if !plan.Fallback {
		t.Errorf("T2 硬编不可用时应标记 Fallback=true")
	}
	if plan.EffectivePresetKey != protocol.PresetLibx264Medium {
		t.Errorf("T2 期望降级为 %s，实际 %s", protocol.PresetLibx264Medium, plan.EffectivePresetKey)
	}
	if len(plan.Warnings) == 0 {
		t.Errorf("T2 降级必须带可读告警")
	}
	if !hasSeq(plan.FinalArgs, "-c:v", "libx264") {
		t.Errorf("T2 阶段三应使用 libx264: %s", joined(plan.FinalArgs))
	}

	// 阶段三必含时长上限、像素格式、帧率与 faststart
	if !hasSeq(plan.FinalArgs, "-t", "00:00:15.000") {
		t.Errorf("T2 阶段三缺少 -t 00:00:15.000: %s", joined(plan.FinalArgs))
	}
	if !hasSeq(plan.FinalArgs, "-pix_fmt", "yuv420p") || !hasSeq(plan.FinalArgs, "-r", "30") {
		t.Errorf("T2 阶段三缺少像素格式/帧率参数: %s", joined(plan.FinalArgs))
	}
	if !hasSeq(plan.FinalArgs, "-movflags", "+faststart") || !hasSeq(plan.FinalArgs, "-map_metadata", "-1") {
		t.Errorf("T2 阶段三缺少封装/元数据参数: %s", joined(plan.FinalArgs))
	}
	if !hasSeq(plan.FinalArgs, "-c:a", "aac") || !hasSeq(plan.FinalArgs, "-b:a", "192k") ||
		!hasSeq(plan.FinalArgs, "-ar", "48000") || !hasSeq(plan.FinalArgs, "-ac", "2") {
		t.Errorf("T2 音频参数不符合 05 §5.1: %s", joined(plan.FinalArgs))
	}
	// 阶段一仍为零重编码
	for i, seg := range plan.Segments {
		if !hasSeq(seg.Args, "-c:v", "copy") {
			t.Errorf("T2 片段 %d 阶段一必须 copy: %s", i, joined(seg.Args))
		}
	}
	// 阶段序列含 mux
	want := []string{StagePrepare, StageSegment, StageSegment, StageSegment, StageConcat, StageMux, StageFinalize}
	if got := stageNames(plan); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("T2 阶段序列错误：期望 %v，实际 %v", want, got)
	}
}

// ---------------- T3：混源素材 → filter_complex 单次转码（缩放/补边/统一帧率） ----------------

func TestT3_MixedSource_FilterConcat(t *testing.T) {
	clips := []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:05.000"),
		clip("videos/b_01.mp4", "00:00:00.000", "00:00:04.000"),
	}
	p := buildPayload(protocol.PresetLibx264Medium, clips)
	probes := map[string]*MediaInfo{
		"videos/a_01.mp4": sameSignatureMI(`\\NAS\media\videos\a_01.mp4`, 600000, true),
		"videos/b_01.mp4": func() *MediaInfo {
			mi := sameSignatureMI(`\\NAS\media\videos\b_01.mp4`, 600000, true)
			mi.Width, mi.Height, mi.RFrameRate = 1280, 720, "25/1"
			return mi
		}(),
	}

	plan, err := BuildRenderEDLCommands(p, srcUNCFromRel, `\\NAS\media\exports\demo_out.mp4`, t.TempDir(), nil, probes)
	if err != nil {
		t.Fatalf("T3 构造失败: %v", err)
	}

	if plan.Mode != ModeSegmentConcatFilter {
		t.Fatalf("T3 期望 Mode=%s，实际 %s（warnings=%v）", ModeSegmentConcatFilter, plan.Mode, plan.Warnings)
	}
	all := joined(plan.FinalArgs)
	if !hasSeq(plan.FinalArgs, "-filter_complex") {
		t.Fatalf("T3 应使用 filter_complex: %s", all)
	}
	for _, need := range []string{
		"scale=1920:1080:force_original_aspect_ratio=decrease",
		"pad=1920:1080",
		"setsar=1",
		"fps=30",
		"aformat=sample_fmts=fltp:sample_rates=48000:channel_layouts=stereo",
		"concat=n=2:v=1:a=1",
	} {
		if !strings.Contains(all, need) {
			t.Errorf("T3 滤镜图缺少 %q: %s", need, all)
		}
	}
	if !hasSeq(plan.FinalArgs, "-map", "[v]") || !hasSeq(plan.FinalArgs, "-map", "[a]") {
		t.Errorf("T3 未映射 concat 输出标签: %s", all)
	}
	// 混源必须单次转码，不得出现 copy
	if strings.Contains(all, "copy") {
		t.Errorf("T3 混源路径不得使用 copy: %s", all)
	}
	if !hasSeq(plan.FinalArgs, "-t", "00:00:09.000") {
		t.Errorf("T3 期望 -t 00:00:09.000: %s", all)
	}
	// 无 concat demuxer 阶段
	for _, s := range plan.Stages {
		if s.Name == StageConcat {
			t.Errorf("T3 混源路径不应存在 concat demuxer 阶段")
		}
	}
	if plan.FinalFrom != plan.TempOutput {
		t.Errorf("T3 finalize 源应为阶段三产物 %q，实际 %q", plan.TempOutput, plan.FinalFrom)
	}
}

// ---------------- T4：素材缺音轨 → lavfi 补静音，保证可拼接 ----------------

func TestT4_MissingAudio_SilentFill(t *testing.T) {
	clips := []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:05.000"),
		clip("videos/a_03.mp4", "00:00:00.000", "00:00:06.000"),
	}
	p := buildPayload(protocol.PresetLibx264Medium, clips)
	probes := map[string]*MediaInfo{
		"videos/a_01.mp4": sameSignatureMI(`\\NAS\media\videos\a_01.mp4`, 600000, true),
		"videos/a_03.mp4": sameSignatureMI(`\\NAS\media\videos\a_03.mp4`, 600000, false), // 无音轨
	}

	plan, err := BuildRenderEDLCommands(p, srcUNCFromRel, `\\NAS\media\exports\demo_out.mp4`, t.TempDir(), nil, probes)
	if err != nil {
		t.Fatalf("T4 构造失败: %v", err)
	}

	if len(plan.Segments) != 2 {
		t.Fatalf("T4 期望 2 个片段，实际 %d", len(plan.Segments))
	}
	silent := plan.Segments[1]
	if silent.HasAudio {
		t.Fatalf("T4 片段 1 应被判定为无音轨")
	}
	if silent.AudioCopy {
		t.Errorf("T4 无音轨片段不得走音频 copy")
	}
	if !hasSeq(silent.Args, "-f", "lavfi") ||
		!strings.Contains(joined(silent.Args), "anullsrc=channel_layout=stereo:sample_rate=48000") {
		t.Fatalf("T4 无音轨片段应补 lavfi anullsrc 静音: %s", joined(silent.Args))
	}
	if !hasSeq(silent.Args, "-map", "1:a:0") || !hasSeq(silent.Args, "-c:a", "aac") {
		t.Errorf("T4 静音轨应映射并编码为 aac: %s", joined(silent.Args))
	}
	if !hasSeq(silent.Args, "-shortest") {
		t.Errorf("T4 静音轨应带 -shortest 防止音频超出视频长度: %s", joined(silent.Args))
	}
	if silent.Args[len(silent.Args)-1] != silent.SegPath {
		t.Errorf("T4 输出路径应位于参数末尾: %s", silent.Args[len(silent.Args)-1])
	}
	// 音轨存在性不一致 → 同源判定失败，走 P-C 混源单次转码（05 §3.1 / §4.3）
	if plan.Mode != ModeSegmentConcatFilter {
		t.Errorf("T4 音轨存在性不一致应降级为 %s，实际 %s", ModeSegmentConcatFilter, plan.Mode)
	}
	// 有音轨片段在 P-C 路径下音频统一转码，不得 copy
	withAudio := plan.Segments[0]
	if !hasSeq(withAudio.Args, "-map", "0:a:0") || !hasSeq(withAudio.Args, "-c:a", "aac") {
		t.Errorf("T4 有音轨片段应保留音轨并统一转 aac: %s", joined(withAudio.Args))
	}
	if hasSeq(withAudio.Args, "-c:a", "copy") {
		t.Errorf("T4 P-C 路径不得对音频 copy: %s", joined(withAudio.Args))
	}
	if plan.TotalMs != 11000 {
		t.Errorf("T4 期望总时长 11000ms，实际 %d", plan.TotalMs)
	}
}

// ---------------- T7：素材缺失 → 可定位失败，且不产生中间产物 ----------------

func TestT7_AssetMissing_Locatable(t *testing.T) {
	clips := []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:05.000"),
		clip("videos/not_exist.mp4", "00:00:00.000", "00:00:05.000"),
	}
	p := buildPayload(protocol.PresetH264NVENCP5, clips)

	// 片段 0 提供缓存探测结果，避免触发真实 ffprobe；片段 1 在路径解析阶段即失败
	probes := map[string]*MediaInfo{
		"videos/a_01.mp4": sameSignatureMI(`\\NAS\media\videos\a_01.mp4`, 600000, true),
	}

	workDir := t.TempDir()
	_, err := BuildRenderEDLCommands(p, func(rel string) (string, error) {
		if rel == "videos/not_exist.mp4" {
			return "", os.ErrNotExist
		}
		return srcUNCFromRel(rel)
	}, `\\NAS\media\exports\demo_out.mp4`, workDir, nil, probes)
	if err == nil {
		t.Fatalf("T7 素材缺失必须返回错误")
	}
	pe, ok := err.(*protocol.PayloadError)
	if !ok {
		t.Fatalf("T7 期望 *protocol.PayloadError，实际 %T: %v", err, err)
	}
	if pe.Code != protocol.ErrCodeAssetMissing {
		t.Errorf("T7 期望错误码 %s，实际 %s", protocol.ErrCodeAssetMissing, pe.Code)
	}
	if pe.Index != 1 {
		t.Errorf("T7 期望失败定位到片段 1，实际 index=%d", pe.Index)
	}
	if pe.Field != "file" {
		t.Errorf("T7 期望 field=file，实际 field=%q", pe.Field)
	}

	// 构造阶段失败不得留下中间产物
	entries, rerr := os.ReadDir(workDir)
	if rerr != nil {
		t.Fatalf("T7 读取中间目录失败: %v", rerr)
	}
	if len(entries) != 0 {
		t.Errorf("T7 构造失败后中间目录应为空，实际 %d 项", len(entries))
	}
}

// ---------------- 载荷闸门：非法 presetKey / 越界出点必须被拦下 ----------------

func TestPayloadGate_RejectsInvalid(t *testing.T) {
	// presetKey 不在枚举表
	p := buildPayload("not_a_preset_key", []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:05.000"),
	})
	probes := map[string]*MediaInfo{
		"videos/a_01.mp4": sameSignatureMI(testSrcUNC, 600000, true),
	}
	if _, err := BuildRenderEDLCommands(p, srcUNCFromRel, `\\NAS\media\exports\o.mp4`, t.TempDir(), nil, probes); err == nil {
		t.Errorf("非法 presetKey 应被拒绝")
	} else if pe, ok := err.(*protocol.PayloadError); !ok || pe.Code != protocol.ErrCodeProfileInvalid {
		t.Errorf("非法 presetKey 错误码应为 %s，实际 %v", protocol.ErrCodeProfileInvalid, err)
	}

	// out 超出源时长（03 §2.5）：源仅 4s，EDL 出点 5s
	shortProbes := map[string]*MediaInfo{
		"videos/a_01.mp4": sameSignatureMI(testSrcUNC, 4000, true),
	}
	p2 := buildPayload(protocol.PresetCopySameSource, []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:05.000"),
	})
	if _, err := BuildRenderEDLCommands(p2, srcUNCFromRel, `\\NAS\media\exports\o.mp4`, t.TempDir(), nil, shortProbes); err == nil {
		t.Errorf("out 超出源时长应返回 E_EDL_INVALID")
	} else if pe, ok := err.(*protocol.PayloadError); !ok || pe.Code != protocol.ErrCodeEDLInvalid {
		t.Errorf("越界错误码应为 %s，实际 %v", protocol.ErrCodeEDLInvalid, err)
	} else if pe.Index != 0 || pe.Field != "out" {
		t.Errorf("越界错误应定位到 index=0/field=out，实际 index=%d/field=%q", pe.Index, pe.Field)
	}
}

// ---------------- A-03：concat 清单路径转义（05 §4.3） ----------------

func TestA03_ConcatPathEscape(t *testing.T) {
	// 反斜杠统一转 '/'，单引号按 POSIX 规则转义为 '\''
	if got, want := escapeConcatPath(`\\NAS\media\it's\a_01.mp4`), `//NAS/media/it'\''s/a_01.mp4`; got != want {
		t.Errorf("A-03 清单路径转义错误：期望 %q，实际 %q", want, got)
	}
	// 含中文与空格的路径不得被改动（仅反斜杠/单引号）
	if got := escapeConcatPath(`\\NAS\媒体盘\成片 01.mp4`); strings.Contains(got, `\`) || !strings.Contains(got, "成片 01.mp4") {
		t.Errorf("A-03 中文/空格路径处理异常: %q", got)
	}
}

// ---------------- A-03：>32 段触发滤镜图分批 concat（05 §4.3） ----------------

func TestA03_FortySegment_BatchedFilterGraph(t *testing.T) {
	const n = 40
	clips := make([]protocol.WireClip, 0, n)
	probes := map[string]*MediaInfo{}
	for i := 0; i < n; i++ {
		rel := fmt.Sprintf("videos/seg_%02d.mp4", i)
		clips = append(clips, clip(rel, "00:00:00.000", "00:00:03.000"))
		mi := sameSignatureMI(`\\NAS\media\`+strings.ReplaceAll(rel, "/", `\`), 20000, true)
		if i%2 == 1 { // 交替混源（720p25）以强制走 P-C
			mi.Width, mi.Height, mi.RFrameRate = 1280, 720, "25/1"
		}
		probes[rel] = mi
	}
	p := buildPayload(protocol.PresetH264NVENCP5, clips)
	plan, err := BuildRenderEDLCommands(p, srcUNCFromRel, `\\NAS\media\exports\forty.mp4`, t.TempDir(), nil, probes)
	if err != nil {
		t.Fatalf("A-03 40 段构造失败: %v", err)
	}
	if plan.Mode != ModeSegmentConcatFilter {
		t.Fatalf("A-03 期望 Mode=%s，实际 %s（warnings=%v）", ModeSegmentConcatFilter, plan.Mode, plan.Warnings)
	}
	graph := ""
	for i, a := range plan.FinalArgs {
		if a == "-filter_complex" && i+1 < len(plan.FinalArgs) {
			graph = plan.FinalArgs[i+1]
		}
	}
	if graph == "" {
		t.Fatalf("A-03 未找到 filter_complex: %s", joined(plan.FinalArgs))
	}
	// 40 段 / 每批 32 → 2 批 + 1 次批间合并
	// 注意：concat 滤镜要求输入按片段交错（vb0,ab0,vb1,ab1…），批间合并同样适用
	for _, need := range []string{"concat=n=32:v=1:a=1[vb0][ab0]", "concat=n=8:v=1:a=1[vb1][ab1]",
		"[vb0][ab0][vb1][ab1]concat=n=2:v=1:a=1[v][a]"} {
		if !strings.Contains(graph, need) {
			t.Errorf("A-03 滤镜图缺少分批片段 %q", need)
		}
	}
	if plan.TotalMs != 120000 {
		t.Errorf("A-03 期望总时长 120000ms，实际 %d", plan.TotalMs)
	}
	if !hasSeq(plan.FinalArgs, "-t", "00:02:00.000") {
		t.Errorf("A-03 期望 -t 00:02:00.000: %s", joined(plan.FinalArgs))
	}
}
