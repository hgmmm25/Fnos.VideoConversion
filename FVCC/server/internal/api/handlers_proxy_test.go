package api

// M4 代理提交与素材扫描验收测试：
//  1) POST /proxy 契约（04 §3.2）：入参 {assetId,file,root?}，出参 {taskId,status,proxyFile}；
//  2) 闸门：素材缺失 404 / file 结构非法 400 / assetId 缺失 400 / root 非 src 400；
//  3) 同素材去重（04 §3.5、决策⑤）：已有 QUEUE/RUNNING 代理任务时返回既有 taskId（HTTP 200），不重复建任务；
//  4) scanDirectory 过滤 `_` 前缀目录（04 §2.4 一致性约束）。

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestM4ProxySubmitContract(t *testing.T) {
	e := newM4Env(t)
	e.writeFile(t, "demo/a_01.mp4", 3000)

	// 1) 素材缺失 → 404 E_ASSET_MISSING
	w := doEDLRequest(t, e.r, http.MethodPost, "/api/proxy",
		`{"assetId":"a_0000000a","file":"demo/missing.mp4"}`)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "E_ASSET_MISSING") {
		t.Fatalf("缺失素材应 404 E_ASSET_MISSING，实际 %d %s", w.Code, w.Body.String())
	}

	// 2) file 结构非法（.. 穿越）→ 400 E_EDL_INVALID
	w = doEDLRequest(t, e.r, http.MethodPost, "/api/proxy",
		`{"assetId":"a_0000000a","file":"../a_01.mp4"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "E_EDL_INVALID") {
		t.Fatalf("非法 file 应 400 E_EDL_INVALID，实际 %d %s", w.Code, w.Body.String())
	}

	// 3) assetId 缺失 → 400 E_ASSET_ID_INVALID
	w = doEDLRequest(t, e.r, http.MethodPost, "/api/proxy", `{"file":"demo/a_01.mp4"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "E_ASSET_ID_INVALID") {
		t.Fatalf("缺失 assetId 应 400 E_ASSET_ID_INVALID，实际 %d %s", w.Code, w.Body.String())
	}

	// 4) root 仅支持 src → proxy 报 400 E_ROOT_UNKNOWN
	w = doEDLRequest(t, e.r, http.MethodPost, "/api/proxy",
		`{"assetId":"a_0000000a","file":"demo/a_01.mp4","root":"proxy"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "E_ROOT_UNKNOWN") {
		t.Fatalf("root=proxy 应 400 E_ROOT_UNKNOWN，实际 %d %s", w.Code, w.Body.String())
	}

	// 5) 正常提交 → 200 {taskId,status,proxyFile}
	const submit = `{"assetId":"a_0000000a","file":"demo/a_01.mp4"}`
	w = doEDLRequest(t, e.r, http.MethodPost, "/api/proxy", submit)
	if w.Code != http.StatusOK {
		t.Fatalf("代理提交应 200，实际 %d %s", w.Code, w.Body.String())
	}
	var res struct {
		TaskID    string `json:"taskId"`
		Status    string `json:"status"`
		ProxyFile string `json:"proxyFile"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("代理响应解析失败: %v (%s)", err, w.Body.String())
	}
	if !strings.HasPrefix(res.TaskID, "t_") {
		t.Fatalf("taskId 形态应为 t_*，实际 %q", res.TaskID)
	}
	if res.Status != string(StatusQueue) {
		t.Fatalf("新建代理任务状态应为 QUEUE，实际 %q", res.Status)
	}
	if res.ProxyFile != "demo/a_01.proxy.mp4" {
		t.Fatalf("proxyFile 应为 demo/a_01.proxy.mp4，实际 %q", res.ProxyFile)
	}

	// 6) 任务已入库且为 GEN_PROXY，载荷含源/代理相对路径
	task, ok := e.store.GetTask(res.TaskID)
	if !ok {
		t.Fatalf("代理任务未入库: %s", res.TaskID)
	}
	if task.TaskType != TaskTypeGenProxy {
		t.Fatalf("taskType 应为 GEN_PROXY，实际 %q", task.TaskType)
	}
	if task.SourceFile != "demo/a_01.mp4" {
		t.Fatalf("任务 SourceFile 应为素材相对路径，实际 %q", task.SourceFile)
	}
	var payload GenProxyPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		t.Fatalf("任务载荷解析失败: %v", err)
	}
	if payload.AssetID != "a_0000000a" || payload.SrcFile != "demo/a_01.mp4" || payload.ProxyFile != "demo/a_01.proxy.mp4" {
		t.Fatalf("载荷字段不符: %+v", payload)
	}
	if payload.Template.PresetKey == "" || payload.Template.Height != 720 {
		t.Fatalf("载荷模板未按 04 §3.3 缺省填充: %+v", payload.Template)
	}

	// 7) 同素材重复提交 → 返回既有 taskId（HTTP 200，不重复建任务）
	w = doEDLRequest(t, e.r, http.MethodPost, "/api/proxy", submit)
	if w.Code != http.StatusOK {
		t.Fatalf("重复提交应 200（返回既有任务），实际 %d %s", w.Code, w.Body.String())
	}
	var again struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &again); err != nil {
		t.Fatalf("重复提交响应解析失败: %v", err)
	}
	if again.TaskID != res.TaskID {
		t.Fatalf("重复提交必须复用既有 taskId（%s），实际 %q", res.TaskID, again.TaskID)
	}
	count := 0
	for _, tk := range e.store.GetTasks() {
		if tk.TaskType == TaskTypeGenProxy {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("同素材代理任务应只有 1 个，实际 %d", count)
	}
}

func TestM4ScanDirectoryFiltersReservedDirs(t *testing.T) {
	e := newM4Env(t)
	e.writeFile(t, "demo/a.mp4", 16)
	e.writeFile(t, "normal/b.mkv", 16)
	e.writeFile(t, "_proxy/demo/a.proxy.mp4", 16)
	e.writeFile(t, "_wve_cache/junk.mp4", 16)
	e.writeFile(t, "_exports/out.mp4", 16)

	body, _ := json.Marshal(map[string]string{"path": e.root})
	w := doEDLRequest(t, e.r, http.MethodPost, "/api/video/scan", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("扫描应 200，实际 %d %s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	if !strings.Contains(out, "a.mp4") || !strings.Contains(out, "b.mkv") {
		t.Fatalf("正常素材必须被召回: %s", out)
	}
	for _, reserved := range []string{"_proxy", "_wve_cache", "_exports", "junk.mp4", "out.mp4", "a.proxy.mp4"} {
		if strings.Contains(out, reserved) {
			t.Fatalf("保留目录/文件 %q 不得出现在扫描结果中: %s", reserved, out)
		}
	}
	if !strings.Contains(out, `"total":2`) {
		t.Fatalf("扫描结果应只含 2 个素材: %s", out)
	}
}
