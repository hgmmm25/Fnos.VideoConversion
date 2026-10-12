package logger

import (
	"bytes"
	"strings"
	"testing"
)

// P3-7 覆盖率补强：logger 对外配置/缓冲/导出 API 的单测。
// 注意测试间对全局状态的依赖：每个用例显式 SetLogLevel 复位，避免顺序耦合。

func TestLogLevelAndConfigRoundTrip(t *testing.T) {
	defer SetLogLevel(INFO)

	SetLogLevel(DEBUG)
	if got := GetLogLevel(); got != DEBUG {
		t.Fatalf("期望 DEBUG，实际 %v", got)
	}
	SetLogConfig(5, 3)
	SetLogLevel(INFO)
	if got := GetLogLevel(); got != INFO {
		t.Fatalf("期望 INFO，实际 %v", got)
	}
}

func TestRecentLogsBufferAndExport(t *testing.T) {
	SetLogLevel(INFO)
	defer SetLogLevel(INFO)
	ClearLogs()

	Info("mod-a", "hello %d", 1)
	InfoT("mod-a", "trace-1", "world")
	Debug("mod-a", "should-be-filtered") // 默认 INFO 级别下被过滤（覆盖 level<currentLevel 分支）

	logs := GetRecentLogs(10)
	if len(logs) != 2 {
		t.Fatalf("期望缓冲 2 条（DEBUG 被过滤），实际 %d", len(logs))
	}
	if logs[0].Message != "hello 1" || logs[1].TraceID != "trace-1" {
		t.Fatalf("缓冲内容异常: %+v", logs)
	}
	if got := GetRecentLogs(1); len(got) != 1 || got[0].Message != "world" {
		t.Fatalf("count 截断应取末尾 1 条: %+v", got)
	}

	var buf bytes.Buffer
	if err := ExportLogs(&buf); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "hello 1") || !strings.Contains(out, "[trace=trace-1]") {
		t.Fatalf("导出内容缺少消息或 trace 段:\n%s", out)
	}
	if strings.Contains(out, "should-be-filtered") {
		t.Fatalf("被过滤的 DEBUG 不应进入缓冲/导出")
	}

	ClearLogs()
	if len(GetRecentLogs(10)) != 0 {
		t.Fatalf("ClearLogs 后缓冲应为空")
	}
}

func TestTraceSegment(t *testing.T) {
	if s := traceSegment(""); s != "" {
		t.Fatalf("空 traceID 应返回空串，实际 %q", s)
	}
	if s := traceSegment("abc"); s != " [trace=abc]" {
		t.Fatalf("traceSegment 格式异常: %q", s)
	}
}
