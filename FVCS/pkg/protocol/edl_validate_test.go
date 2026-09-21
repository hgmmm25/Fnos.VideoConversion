package protocol

// EDL 白名单校验测试（D-01，07 §3.2 / §3.5 / §3.6 / §8）
//
// 用例数据来自 testdata/edl_vectors.json —— 该文件与 FVCC 侧共用，
// 保证"两侧独立实现、规则一致"（07 §3.1 纵深防御）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vecOK = "OK"

type edlVectors struct {
	Version int             `json:"version"`
	Spec    string          `json:"spec"`
	RelPath []relPathVector `json:"relpath"`
	Output  []nameVector    `json:"output"`
	Payload []payloadVector `json:"payload"`
	Symlink []symlinkVector `json:"symlink"`
}

type relPathVector struct {
	ID      string `json:"id"`
	In      string `json:"in"`
	Decoded bool   `json:"decoded"`
	Want    string `json:"wantCode"`
	Note    string `json:"note"`
}

type nameVector struct {
	ID   string `json:"id"`
	In   string `json:"in"`
	Want string `json:"wantCode"`
	Note string `json:"note"`
}

type payloadVector struct {
	ID   string          `json:"id"`
	Raw  json.RawMessage `json:"raw"`
	Want string          `json:"wantCode"`
	Note string          `json:"note"`
}

type symlinkVector struct {
	ID   string `json:"id"`
	Rel  string `json:"rel"`
	Want string `json:"wantCode"`
	Note string `json:"note"`
}

func loadVectors(t *testing.T) *edlVectors {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "edl_vectors.json"))
	if err != nil {
		t.Fatalf("读取测试向量失败: %v", err)
	}
	var v edlVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("解析测试向量失败: %v", err)
	}
	if v.Version == 0 || len(v.RelPath) == 0 {
		t.Fatal("测试向量内容异常（version/relpath 为空）")
	}
	return &v
}

func codeOf(e *PayloadError) string {
	if e == nil {
		return vecOK
	}
	return e.Code
}

// ---------- 相对路径 L1~L3 ----------

func TestEDLVectorsRelPath(t *testing.T) {
	v := loadVectors(t)
	for _, c := range v.RelPath {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			var e *PayloadError
			if c.Decoded {
				e = ValidateRelPathDecoded(c.In, AllowedSourceExt)
			} else {
				e = ValidateRelPath(c.In, AllowedSourceExt)
			}
			if got := codeOf(e); got != c.Want {
				t.Fatalf("rel=%q 期望 %s，实际 %s（%v）｜%s", c.In, c.Want, got, e, c.Note)
			}
		})
	}
}

// ---------- 输出名 §3.5 ----------

func TestEDLVectorsOutputName(t *testing.T) {
	v := loadVectors(t)
	for _, c := range v.Output {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			e := ValidateOutputName(c.In)
			if got := codeOf(e); got != c.Want {
				t.Fatalf("output=%q 期望 %s，实际 %s（%v）｜%s", c.In, c.Want, got, e, c.Note)
			}
		})
	}
}

// ---------- 载荷 §8 S4~S12 ----------

func TestEDLVectorsPayload(t *testing.T) {
	v := loadVectors(t)
	for _, c := range v.Payload {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			p, e := DecodeRenderPayloadStrict(c.Raw)
			if e == nil {
				e = ValidateRenderEDLPayload(p)
			}
			if got := codeOf(e); got != c.Want {
				t.Fatalf("期望 %s，实际 %s（%v）｜%s", c.Want, got, e, c.Note)
			}
		})
	}
}

// ---------- 载荷限制 §3.6 ----------

func TestValidatePayloadLimits(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"合法嵌套", `{"a":1,"b":[1,2,{"c":"x"}]}`, vecOK},
		{"深度9", strings.Repeat("[", 9) + strings.Repeat("]", 9), ErrCodePayloadInvalid},
		{"深度8", strings.Repeat("[", 8) + strings.Repeat("]", 8), vecOK},
		{"单字符串300", `{"a":"` + strings.Repeat("b", 300) + `"}`, ErrCodePayloadInvalid},
		{"字符串255", `{"a":"` + strings.Repeat("b", 255) + `"}`, vecOK},
		{"结构不完整", `{"a":1`, ErrCodePayloadInvalid},
		{"超256KB", `{"a":"` + strings.Repeat("b", 300*1024) + `"}`, ErrCodeEDLTooLarge},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			e := ValidatePayloadLimits([]byte(c.raw))
			if got := codeOf(e); got != c.want {
				t.Fatalf("期望 %s，实际 %s（%v）", c.want, got, e)
			}
		})
	}
}

// ---------- 落地校验 L4 ----------

func TestResolveRelPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "videos", "2024"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "videos", "2024", "a.mp4")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, e := ResolveRelPath(root, "videos/2024/a.mp4")
	if e != nil {
		t.Fatalf("合法路径应通过 L4，实际 %v", e)
	}
	if filepath.Base(got) != "a.mp4" {
		t.Fatalf("解析结果异常: %s", got)
	}

	// 尚不存在的输出文件：对父目录求 EvalSymlinks
	if _, e := ResolveRelPath(root, "videos/2024/out.mp4"); e != nil {
		t.Fatalf("不存在的输出文件应通过（父目录存在），实际 %v", e)
	}

	if _, e := ResolveRelPath(root, "../../etc/passwd"); e == nil {
		t.Fatal("穿越路径必须被拒")
	}
	if _, e := ResolveRelPath("", "videos/a.mp4"); e == nil {
		t.Fatal("空根必须被拒")
	}
}

func TestResolveRelPathSymlinkEscape(t *testing.T) {
	v := loadVectors(t)
	if len(v.Symlink) == 0 {
		t.Skip("无符号链接向量")
	}

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("当前环境无法创建符号链接，跳过 L4 符号链接用例: %v", err)
	}

	for _, c := range v.Symlink {
		_, e := ResolveRelPath(root, c.Rel)
		if got := codeOf(e); got != c.Want {
			t.Fatalf("%s 期望 %s，实际 %s（%v）｜%s", c.ID, c.Want, got, e, c.Note)
		}
	}
}
