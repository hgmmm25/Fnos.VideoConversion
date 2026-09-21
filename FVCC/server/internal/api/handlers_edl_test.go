package api

// B-03 验收测试（03 §4.2 / §4.3 / §5）：
//  1) 项目 CRUD 路由契约与 03 §4 示例一致（字段名、clips 空数组、不泄漏内部字段）；
//  2) PUT rev 乐观锁 → 409 E_REV_CONFLICT 且 detail 含 serverRev/clientRev；
//  3) 名称唯一 → 409；项目不存在 → 404；
//  4) 契约级闸门：片段时长关系、出点超素材、speed/transition、路径结构、clips 数量。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newEDLTestRouter(t *testing.T, dir string) (*gin.Engine, *Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	h := &Handlers{store: s, hub: NewHub()}
	return newRouter(Config{}, h), s
}

func doEDLRequest(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, gwPrefix+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const edlTimelineJSON = `{"width":1920,"height":1080,"fps":30,"sampleRate":48000,"audio":true}`

func TestEDLProjectCRUDContract(t *testing.T) {
	r, _ := newEDLTestRouter(t, t.TempDir())

	// 1) 新建（03 §4.2 示例）
	w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects",
		`{"name":"demo_粗剪","timeline":`+edlTimelineJSON+`}`)
	if w.Code != http.StatusOK {
		t.Fatalf("新建项目失败: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"clips":[]`) {
		t.Fatalf("clips 必须序列化为空数组: %s", body)
	}
	if strings.Contains(body, `"segment"`) || strings.Contains(body, "payload") {
		t.Fatalf("响应不得泄漏内部字段: %s", body)
	}
	if !strings.Contains(body, `"clipCount":0`) || !strings.Contains(body, `"totalMs":0`) {
		t.Fatalf("响应必须包含派生字段 clipCount/totalMs: %s", body)
	}
	var created Project
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("响应不可解析: %v", err)
	}
	if len(created.ID) != 10 || created.ID[:2] != "p_" || created.Rev != 1 {
		t.Fatalf("项目标识/版本错误: id=%q rev=%d", created.ID, created.Rev)
	}
	if created.Timeline.FPS != 30 || created.Timeline.SampleRate != 48000 || !created.Timeline.Audio {
		t.Fatalf("时间线参数未按契约保留: %+v", created.Timeline)
	}

	// 2) 列表（03 §4.2：含 clipCount）
	w = doEDLRequest(t, r, http.MethodGet, "/api/edl/projects", "")
	var list struct {
		Items []ProjectSummary `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("列表响应不可解析: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != created.ID || list.Items[0].Rev != 1 {
		t.Fatalf("列表内容错误: %+v", list.Items)
	}

	// 3) 保存：rev=1，1 个片段
	put := `{"rev":1,"name":"demo_粗剪","timeline":` + edlTimelineJSON + `,"clips":[` +
		`{"clipId":"c_00000001","assetId":"a_0000000a","file":"demo/a_01.mp4","inMs":82400,"outMs":225000,"speed":1,"sourceDurationMs":600000}]}`
	w = doEDLRequest(t, r, http.MethodPut, "/api/edl/projects/"+created.ID, put)
	if w.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", w.Code, w.Body.String())
	}
	var saved struct {
		Rev     int       `json:"rev"`
		SavedAt time.Time `json:"savedAt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatalf("保存响应不可解析: %v", err)
	}
	if saved.Rev != 2 || saved.SavedAt.IsZero() {
		t.Fatalf("保存响应错误: %+v", saved)
	}

	// 4) 旧 rev 重放 → 409 E_REV_CONFLICT（03 §4.3）
	w = doEDLRequest(t, r, http.MethodPut, "/api/edl/projects/"+created.ID, put)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), errCodeRevConflict) {
		t.Fatalf("旧 rev 应返回 409 %s，实际 %d %s", errCodeRevConflict, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"serverRev":2`) || !strings.Contains(w.Body.String(), `"clientRev":1`) {
		t.Fatalf("409 detail 缺少 serverRev/clientRev: %s", w.Body.String())
	}

	// 5) 详情：片段与派生字段
	w = doEDLRequest(t, r, http.MethodGet, "/api/edl/projects/"+created.ID, "")
	var got Project
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("详情响应不可解析: %v", err)
	}
	if len(got.Clips) != 1 || got.Clips[0].ClipID != "c_00000001" || got.Clips[0].File != "demo/a_01.mp4" {
		t.Fatalf("片段未持久化: %+v", got.Clips)
	}
	if got.ClipCount != 1 || got.TotalMs != 142600 {
		t.Fatalf("派生字段错误: clipCount=%d totalMs=%d", got.ClipCount, got.TotalMs)
	}

	// 6) 重名 → 409
	w = doEDLRequest(t, r, http.MethodPost, "/api/edl/projects",
		`{"name":"demo_粗剪","timeline":`+edlTimelineJSON+`}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), errCodeProjectNameUse) {
		t.Fatalf("重名应返回 409 %s，实际 %d %s", errCodeProjectNameUse, w.Code, w.Body.String())
	}

	// 7) PUT 缺 rev → 400
	w = doEDLRequest(t, r, http.MethodPut, "/api/edl/projects/"+created.ID, `{"name":"demo_粗剪"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), errCodeEDLInvalid) {
		t.Fatalf("缺 rev 应返回 400 %s，实际 %d %s", errCodeEDLInvalid, w.Code, w.Body.String())
	}

	// 8) 删除 + 幂等
	if w = doEDLRequest(t, r, http.MethodDelete, "/api/edl/projects/"+created.ID, ""); w.Code != http.StatusOK {
		t.Fatalf("删除失败: %d %s", w.Code, w.Body.String())
	}
	w = doEDLRequest(t, r, http.MethodDelete, "/api/edl/projects/"+created.ID, "")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), errCodeProjectMissing) {
		t.Fatalf("重复删除应返回 404 %s，实际 %d %s", errCodeProjectMissing, w.Code, w.Body.String())
	}
	if w = doEDLRequest(t, r, http.MethodGet, "/api/edl/projects/p_deadbeef", ""); w.Code != http.StatusNotFound {
		t.Fatalf("不存在项目应返回 404，实际 %d", w.Code)
	}
}

func TestEDLProjectValidationGate(t *testing.T) {
	r, _ := newEDLTestRouter(t, t.TempDir())

	clip := func(mut func(*EDLClip)) string {
		c := EDLClip{
			ClipID: "c_00000001", AssetID: "a_0000000a", File: "demo/a_01.mp4",
			InMs: 0, OutMs: 5000, Speed: 1, SourceDurationMs: 600000,
		}
		mut(&c)
		b, _ := json.Marshal([]EDLClip{c})
		return `{"name":"p","timeline":` + edlTimelineJSON + `,"clips":` + string(b) + `}`
	}

	cases := []struct {
		name     string
		body     string
		wantCode int
		wantSub  string
	}{
		{"正常", clip(func(*EDLClip) {}), http.StatusOK, ""},
		{"出点不晚于入点", clip(func(c *EDLClip) { c.OutMs = c.InMs }), 400, `"index":0`},
		{"片段过短", clip(func(c *EDLClip) { c.OutMs = c.InMs + 99 }), 400, `"durationMs":99`},
		{"出点超素材", clip(func(c *EDLClip) { c.OutMs = 600001 }), 400, `"maxOutMs":600000`},
		{"变速", clip(func(c *EDLClip) { c.Speed = 2 }), 400, "speed"},
		{"转场", clip(func(c *EDLClip) { c.Transition = &Transition{Type: "fade", DurationMs: 500} }), 400, "transition"},
		{"反斜杠路径", clip(func(c *EDLClip) { c.File = `demo\a_01.mp4` }), 400, "POSIX"},
		{"上跳路径", clip(func(c *EDLClip) { c.File = "demo/../secret.mp4" }), 400, "非法路径段"},
		{"绝对路径", clip(func(c *EDLClip) { c.File = "/etc/passwd" }), 400, "POSIX"},
		{"fps 越界", `{"name":"p","timeline":{"width":1920,"height":1080,"fps":200,"sampleRate":48000,"audio":true}}`, 400, "timeline.fps"},
		{"宽为奇数", `{"name":"p","timeline":{"width":1921,"height":1080,"fps":30,"sampleRate":48000,"audio":true}}`, 400, "timeline.width"},
		{"关闭音频", `{"name":"p","timeline":{"width":1920,"height":1080,"fps":30,"sampleRate":48000,"audio":false}}`, 400, "timeline.audio"},
		{"名称为空", `{"name":"   ","timeline":` + edlTimelineJSON + `}`, 400, "name"},
	}
	for _, tc := range cases {
		w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects", tc.body)
		if w.Code != tc.wantCode {
			t.Fatalf("%s：期望 %d，实际 %d（%s）", tc.name, tc.wantCode, w.Code, w.Body.String())
		}
		if tc.wantSub != "" && !strings.Contains(w.Body.String(), tc.wantSub) {
			t.Fatalf("%s：响应缺少 %q（%s）", tc.name, tc.wantSub, w.Body.String())
		}
	}
}

func TestEDLProjectLimits(t *testing.T) {
	// clips 数量上限 200（03 §2.5）
	clips := make([]EDLClip, 0, edlMaxClips+1)
	for i := 0; i <= edlMaxClips; i++ {
		clips = append(clips, EDLClip{
			ClipID: fmt.Sprintf("c_%08d", i), AssetID: "a_0000000a", File: "demo/a.mp4",
			InMs: 0, OutMs: 1000, Speed: 1, SourceDurationMs: 600000,
		})
	}
	b, _ := json.Marshal(clips)
	if v := validateProjectInput("p", &Timeline{Width: 1920, Height: 1080, FPS: 30, SampleRate: 48000, Audio: true}, clips); v == nil {
		t.Fatalf("201 个片段必须被拒绝")
	} else if v.Detail["max"] != edlMaxClips {
		t.Fatalf("超限 detail 应带 max：%+v", v.Detail)
	}

	// 总时长上限 6 小时
	long := []EDLClip{
		{ClipID: "c_1", AssetID: "a_1", File: "a.mp4", InMs: 0, OutMs: edlMaxTotalMs / 2, Speed: 1},
		{ClipID: "c_2", AssetID: "a_2", File: "b.mp4", InMs: 0, OutMs: edlMaxTotalMs/2 + 1, Speed: 1},
	}
	if v := validateProjectInput("p", &Timeline{Width: 1920, Height: 1080, FPS: 30, SampleRate: 48000, Audio: true}, long); v == nil {
		t.Fatalf("超过 6 小时必须被拒绝")
	}
	_ = b
}
