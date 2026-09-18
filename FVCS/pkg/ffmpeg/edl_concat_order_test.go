// concat 滤镜输入顺序回归测试（缺陷：混源拼接 mux 阶段 Media type mismatch）
//
// 背景：
//   - 现象：混源（视频编码/参数不一致）多片段 RenderEDL 任务在 mux 阶段失败，
//     ffmpeg 退出码 0xffffffea(-22)，stderr:
//     "Media type mismatch between the 'Parsed_fps_N' filter output pad 0 (video)
//     and the 'Parsed_concat_M' filter input pad 1 (audio)"
//   - 根因：buildFilterGraph 生成 concat 输入标签时，把全部视频标签排在全部音频标签之前
//     （[v0][v1][v2][a0][a1][a2]...），而 concat 滤镜在 v=1:a=1 时要求按片段交错
//     （[v0][a0][v1][a1][v2][a2]...）。
//   - 本文件以「标签序列严格断言 + 扁平顺序负向断言」锁死该行为，防止回归。
package ffmpeg

import (
	"fmt"
	"strings"
	"testing"

	"Fnos.VC_Service/pkg/protocol"
)

// 提取 -filter_complex 的滤镜图字符串
func filterGraphOf(t *testing.T, args []string) string {
	t.Helper()
	for i, a := range args {
		if a == "-filter_complex" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("未找到 -filter_complex: %s", joined(args))
	return ""
}

// concat 输入标签辅助断言：期望标签序列按片段交错出现
// startIdx：该批次首片段的全局编号（段标签使用全局索引 [v<i>]/[a<i>]）
func assertConcatInterleaved(t *testing.T, graph string, startIdx, clips int, label, alabel string) {
	t.Helper()
	var want strings.Builder
	for i := startIdx; i < startIdx+clips; i++ {
		fmt.Fprintf(&want, "[v%d][a%d]", i, i)
	}
	fmt.Fprintf(&want, "concat=n=%d:v=1:a=1[%s][%s]", clips, label, alabel)
	if !strings.Contains(graph, want.String()) {
		t.Errorf("concat 输入标签未按片段交错，期望含 %q\n实际滤镜图: %s", want.String(), graph)
	}
}

func TestFilterGraph_ConcatInputInterleaved_ThreeClips(t *testing.T) {
	clips := []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:03.000"),
		clip("videos/b_01.mp4", "00:00:00.000", "00:00:03.000"),
		clip("videos/c_01.mp4", "00:00:00.000", "00:00:03.000"),
	}
	p := buildPayload(protocol.PresetH264NVENCP5, clips)
	segs := make([]SegmentPlan, len(clips))

	graph, err := buildFilterGraph(p, segs, true, nil)
	if err != nil {
		t.Fatalf("混源三片段滤镜图构造失败: %v", err)
	}
	assertConcatInterleaved(t, graph, 0, len(clips), "v", "a")
	// 负向断言：不得出现「全部视频标签连续排列」的扁平形态
	if strings.Contains(graph, "[v0][v1]") {
		t.Errorf("concat 输入标签仍为扁平顺序（视频全部在前）: %s", graph)
	}
}

func TestFilterGraph_ConcatInputInterleaved_TwoClips_NoAudio(t *testing.T) {
	clips := []protocol.WireClip{
		clip("videos/a_01.mp4", "00:00:00.000", "00:00:03.000"),
		clip("videos/b_01.mp4", "00:00:00.000", "00:00:03.000"),
	}
	p := buildPayload(protocol.PresetH264NVENCP5, clips)
	segs := make([]SegmentPlan, len(clips))

	graph, err := buildFilterGraph(p, segs, false, nil)
	if err != nil {
		t.Fatalf("无音轨混源滤镜图构造失败: %v", err)
	}
	if !strings.Contains(graph, "[v0][v1]concat=n=2:v=1:a=0[v]") {
		t.Errorf("无音轨路径应保持纯视频顺序拼接: %s", graph)
	}
}

func TestFilterGraph_ConcatInputInterleaved_FortyClipsBatched(t *testing.T) {
	const n = 40
	clips := make([]protocol.WireClip, 0, n)
	probes := map[string]*MediaInfo{}
	for i := 0; i < n; i++ {
		rel := fmt.Sprintf("videos/seg_%02d.mp4", i)
		clips = append(clips, clip(rel, "00:00:00.000", "00:00:03.000"))
		mi := sameSignatureMI(`\\NAS\media\`+strings.ReplaceAll(rel, "/", `\`), 20000, true)
		if i%2 == 1 { // 交替混源以强制走 P-C（filter_complex）路径
			mi.Width, mi.Height, mi.RFrameRate = 1280, 720, "25/1"
		}
		probes[rel] = mi
	}
	p := buildPayload(protocol.PresetH264NVENCP5, clips)
	plan, err := BuildRenderEDLCommands(p, srcUNCFromRel, `\\NAS\media\exports\forty.mp4`, t.TempDir(), nil, probes)
	if err != nil {
		t.Fatalf("40 段混源构造失败: %v", err)
	}
	if plan.Mode != ModeSegmentConcatFilter {
		t.Fatalf("期望 Mode=%s，实际 %s", ModeSegmentConcatFilter, plan.Mode)
	}
	graph := filterGraphOf(t, plan.FinalArgs)

	// 批内交错（32 段，全局编号 0..31）：[v0][a0]...[v31][a31]concat=n=32:v=1:a=1[vb0][ab0]
	assertConcatInterleaved(t, graph, 0, 32, "vb0", "ab0")
	// 批内交错（8 段，全局编号 32..39，标签沿用全局索引）
	assertConcatInterleaved(t, graph, 32, 8, "vb1", "ab1")
	// 批间合并同样必须交错：vb0,ab0,vb1,ab1
	if !strings.Contains(graph, "[vb0][ab0][vb1][ab1]concat=n=2:v=1:a=1[v][a]") {
		t.Errorf("批间合并输入标签未交错: %s", graph)
	}
	// 负向断言：扁平顺序不得出现在任何 concat 之前
	if strings.Contains(graph, "[v31][a31]concat=n=32") == false {
		t.Errorf("32 段批次收尾标签异常: %s", graph)
	}
}

func TestJoinInterleaved(t *testing.T) {
	cases := []struct {
		v, a []string
		want string
	}{
		{[]string{"[v0]"}, []string{"[a0]"}, "[v0][a0]"},
		{[]string{"[v0]", "[v1]", "[v2]"}, []string{"[a0]", "[a1]", "[a2]"}, "[v0][a0][v1][a1][v2][a2]"},
		{[]string{"[vb0]", "[vb1]"}, []string{"[ab0]", "[ab1]"}, "[vb0][ab0][vb1][ab1]"},
		{[]string{"[v0]", "[v1]"}, nil, "[v0][v1]"},
	}
	for _, c := range cases {
		if got := joinInterleaved(c.v, c.a); got != c.want {
			t.Errorf("joinInterleaved(%v, %v) = %q，期望 %q", c.v, c.a, got, c.want)
		}
	}
}
