package api

// P2-2 测试补强（2026-09-17）：EDL 校验模糊测试。
// fuzz 目标覆盖三个纯校验入口：
//   1) decodeRenderPayloadStrict —— 任意字节的 JSON 载荷解析，不应 panic；
//   2) validateOutputNameStrict —— 任意输出名合法性校验，不应 panic；
//   3) validateRenderEDLPayload —— 解析出的载荷整体校验，不应 panic。
// 种子语料包含合法载荷/边界输出名，go test 默认仅跑种子，-fuzz 时持续变异。

import (
	"encoding/json"
	"testing"
)

// FuzzDecodeRenderPayloadStrict 任意字节 JSON → 严格解析不 panic。
func FuzzDecodeRenderPayloadStrict(f *testing.F) {
	seeds := []string{
		`{"clips":[],"out":"a.mp4"}`,
		`{"clips":[{"in":"/vol/media/a.mp4","outMs":9000,"inMs":0,"out":"seg1.mp4"}],"out":"result.mp4","profile":{"videoCodec":"h264","width":1920,"height":1080,"crf":23,"fps":30},"root":"/vol"}`,
		`{"clips":null,"out":""}`,
		`{}`,
		`not-json`,
		``,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		raw := json.RawMessage(data)
		// 仅断言不 panic；解析失败返回 err 属合法路径
		_, _ = decodeRenderPayloadStrict(raw)
	})
}

// FuzzValidateOutputNameStrict 任意字符串 → 输出名校验不 panic。
func FuzzValidateOutputNameStrict(f *testing.F) {
	seeds := []string{
		"result.mp4",
		"a_b-c1.mp4",
		"",
		"../escape.mp4",
		"a/b.mp4",
		"中文文件名.mp4",
		"a b.mp4",
		".hidden.mp4",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		_ = validateOutputNameStrict(name)
	})
}

// FuzzValidateRenderEDLPayload 随机 JSON → 整体载荷校验不 panic。
func FuzzValidateRenderEDLPayload(f *testing.F) {
	seeds := []string{
		`{"clips":[],"out":"a.mp4","profile":{"videoCodec":"h264","width":1920,"height":1080,"crf":23,"fps":30},"root":"/vol"}`,
		`{"clips":[{"in":"/vol/m/a.mp4","outMs":1000,"inMs":0,"out":"s.mp4"}],"out":"r.mp4","profile":{"videoCodec":"hevc","width":1280,"height":720,"crf":28,"fps":25},"root":"/vol","checksum":"abc123","totalMs":1000}`,
		`{"out":"x.mp4"}`,
		`null`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var p RenderTaskPayload
		if err := json.Unmarshal(data, &p); err != nil {
			return
		}
		_ = validateRenderEDLPayload(&p)
	})
}
