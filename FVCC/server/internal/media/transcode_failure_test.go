package media

import (
	"strings"
	"testing"

	"fvcc/internal/remote"
)

// P3-7 覆盖率补强：本地转码时间解析 + 远端失败码/文案组合 helper 单测。

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"01:02:03", 3723},
		{"00:00:05.5", 5.5},
		{"10:20:30", 37230},
		{"00:61:00", 3660}, // 不进位，按公式直算（与 ffmpeg out_time 直读口径一致）
		{"", 0},
		{"12", 0},
		{"1:2", 0},
		{"a:b:c", 0},
	}
	for _, c := range cases {
		if got := parseTime(c.in); got != c.want {
			t.Errorf("parseTime(%q) = %v，期望 %v", c.in, got, c.want)
		}
	}
}

func TestRemoteFailureCode(t *testing.T) {
	if got := RemoteFailureCode(remote.RemoteTaskStatus{ErrorCode: " E_FFMPEG_EXIT "}); got != "E_FFMPEG_EXIT" {
		t.Fatalf("应优先取节点上报码（含去空格），实际 %q", got)
	}
	for _, st := range []remote.RemoteTaskStatus{{}, {ErrorCode: "  "}} {
		if got := RemoteFailureCode(st); got != remote.ErrCodeRenderFailed {
			t.Fatalf("缺码时应回退 %s，实际 %q", remote.ErrCodeRenderFailed, got)
		}
	}
}

func TestRemoteFailureMessage(t *testing.T) {
	full := RemoteFailureMessage("渲染失败", remote.RemoteTaskStatus{
		Status: "Failed", Stage: "transcoding", ErrorCode: "E_FFMPEG_EXIT", ErrorMessage: "exit status 1",
	})
	for _, want := range []string{"渲染失败", "节点状态 Failed", "阶段 transcoding", "E_FFMPEG_EXIT", "exit status 1"} {
		if !strings.Contains(full, want) {
			t.Fatalf("完整明细缺少 %q:\n%s", want, full)
		}
	}

	minimal := RemoteFailureMessage("代理生成失败", remote.RemoteTaskStatus{Status: "Failed"})
	if !strings.Contains(minimal, "代理生成失败（节点状态 Failed）") {
		t.Fatalf("仅状态时的文案异常:\n%s", minimal)
	}
	if strings.Contains(minimal, "阶段") || strings.Contains(minimal, ":") {
		t.Fatalf("空字段不应产生占位:\n%s", minimal)
	}
}
