package remote

// progressPush 双风格兼容解析测试（API_CONTRACT §3.2）：
// 线上字段 camelCase 优先，缺失回落 snake_case——旧节点
// task_id/out_time_ms/total_ms/seg_total 报文不回归。

import (
	"encoding/json"
	"testing"
)

func TestProgressPushUnmarshalDualStyle(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want progressPush
	}{
		{
			name: "camelCase",
			raw:  `{"taskId":"t1","progress":42.5,"stage":"segment","seg":2,"segTotal":10,"outTimeMs":1234,"totalMs":9999,"speed":"2.0x","msg":"ok"}`,
			want: progressPush{TaskID: "t1", Progress: 42.5, Stage: "segment", Seg: 2, SegTotal: 10, OutTimeMs: 1234, TotalMs: 9999, Speed: "2.0x", Msg: "ok"},
		},
		{
			name: "snakeCaseLegacy",
			raw:  `{"task_id":"t2","progress":10,"stage":"prepare","seg":1,"seg_total":5,"out_time_ms":100,"total_ms":8000,"speed":"1.0x","msg":"legacy"}`,
			want: progressPush{TaskID: "t2", Progress: 10, Stage: "prepare", Seg: 1, SegTotal: 5, OutTimeMs: 100, TotalMs: 8000, Speed: "1.0x", Msg: "legacy"},
		},
		{
			name: "mixedCamelPrecedence",
			raw:  `{"taskId":"t3","task_id":"WRONG","segTotal":7,"seg_total":8,"outTimeMs":500,"out_time_ms":600}`,
			want: progressPush{TaskID: "t3", SegTotal: 7, OutTimeMs: 500},
		},
		{
			name: "empty",
			raw:  `{}`,
			want: progressPush{},
		},
	}
	for _, tc := range cases {
		var p progressPush
		if err := json.Unmarshal([]byte(tc.raw), &p); err != nil {
			t.Fatalf("%s: Unmarshal 失败: %v", tc.name, err)
		}
		if p != tc.want {
			t.Fatalf("%s: 解析结果不符: got %+v want %+v", tc.name, p, tc.want)
		}
	}
}

func TestProgressPushMarshalCamelCase(t *testing.T) {
	p := progressPush{TaskID: "t9", Progress: 88, Stage: "finalize", SegTotal: 3, OutTimeMs: 1, TotalMs: 2}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("重新解析失败: %v", err)
	}
	for _, key := range []string{"taskId", "segTotal", "outTimeMs", "totalMs"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("Marshal 后缺少 camelCase 键 %s: %s", key, raw)
		}
	}
	for _, key := range []string{"task_id", "seg_total", "out_time_ms", "total_ms"} {
		if _, ok := m[key]; ok {
			t.Fatalf("Marshal 后不应出现 snake_case 键 %s: %s", key, raw)
		}
	}
}
