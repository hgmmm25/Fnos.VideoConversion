package api

// C-02 回归用例（2026-09-16）：剪辑项目列表不显示历史项目。
//
// 现象：历史项目已落盘 projects.json，但剪辑标签页列表为空，
// 而新建同名项目时提示「已存在」。
// 根因：前端 api.listProjects 按 {projects,total} 解析，服务端只回 {items}，
// 字段不匹配导致 list 恒为空数组（同名拦截走 CreateProject 的 ErrProjectNameUsed，故仍生效）。
// 本用例锁定修复后的契约：列表同时下发 items 与 projects/total，且同名拦截不受影响。

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestEDLProjectListFieldsContract(t *testing.T) {
	r, _ := newEDLTestRouter(t, t.TempDir())

	// 1) 造两个「历史项目」（落盘后列表接口应能全部列出）
	for _, name := range []string{"历史项目_A", "历史项目_B"} {
		w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects",
			`{"name":"`+name+`","timeline":`+edlTimelineJSON+`}`)
		if w.Code != http.StatusOK {
			t.Fatalf("创建项目 %s 失败: %d %s", name, w.Code, w.Body.String())
		}
	}

	// 2) 列表接口字段契约
	w := doEDLRequest(t, r, http.MethodGet, "/api/edl/projects", "")
	if w.Code != http.StatusOK {
		t.Fatalf("列表接口失败: %d %s", w.Code, w.Body.String())
	}

	type listItem struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var resp struct {
		Items    []listItem `json:"items"`
		Projects []listItem `json:"projects"`
		Total    int        `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("列表响应不可解析: %v (%s)", err, w.Body.String())
	}

	if len(resp.Items) != 2 {
		t.Fatalf("items 应为 2 条，实际 %d 条: %s", len(resp.Items), w.Body.String())
	}
	if len(resp.Projects) != len(resp.Items) {
		t.Fatalf("projects 别名缺失或数量不符: projects=%d items=%d", len(resp.Projects), len(resp.Items))
	}
	if resp.Total != len(resp.Items) {
		t.Fatalf("total 与实际条数不符: total=%d items=%d", resp.Total, len(resp.Items))
	}
	for _, p := range resp.Projects {
		if p.ID == "" || p.Name == "" {
			t.Fatalf("列表项缺少前端渲染所需字段: %+v", p)
		}
	}

	// 3) 同名拦截仍须生效（409 E_PROJECT_NAME_USED 语义）
	w = doEDLRequest(t, r, http.MethodPost, "/api/edl/projects",
		`{"name":"历史项目_A","timeline":`+edlTimelineJSON+`}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("同名项目应返回 409，实际 %d %s", w.Code, w.Body.String())
	}
}

// TestEDLProjectListAfterReload 复现并锁定真实 BUG 场景：
// 项目在上一次运行中创建并落盘（如 fnOS 重启应用），本次启动重新 Load，
// 剪辑页应能列出这些历史项目。
func TestEDLProjectListAfterReload(t *testing.T) {
	dir := t.TempDir()

	// 第一次生命周期：创建 3 个历史项目并落盘
	r1, _ := newEDLTestRouter(t, dir)
	for _, name := range []string{"旧项目_一", "旧项目_二", "旧项目_三"} {
		w := doEDLRequest(t, r1, http.MethodPost, "/api/edl/projects",
			`{"name":"`+name+`","timeline":`+edlTimelineJSON+`}`)
		if w.Code != http.StatusOK {
			t.Fatalf("创建 %s 失败: %d %s", name, w.Code, w.Body.String())
		}
	}

	// 第二次生命周期：同一数据目录重新加载
	r2, _ := newEDLTestRouter(t, dir)
	w := doEDLRequest(t, r2, http.MethodGet, "/api/edl/projects", "")
	if w.Code != http.StatusOK {
		t.Fatalf("列表接口失败: %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		Items    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("列表响应不可解析: %v (%s)", err, w.Body.String())
	}
	if len(resp.Items) != 3 || len(resp.Projects) != 3 || resp.Total != 3 {
		t.Fatalf("重载后历史项目未完整列出: items=%d projects=%d total=%d (%s)",
			len(resp.Items), len(resp.Projects), resp.Total, w.Body.String())
	}

	// 重载后同名拦截依然生效
	w = doEDLRequest(t, r2, http.MethodPost, "/api/edl/projects",
		`{"name":"旧项目_一","timeline":`+edlTimelineJSON+`}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("重载后同名项目应返回 409，实际 %d %s", w.Code, w.Body.String())
	}
}
