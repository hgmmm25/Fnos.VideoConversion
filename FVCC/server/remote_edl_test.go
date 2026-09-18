package main

// B-06 验收测试（06 §4.2 下发 / 07 §5.2、§5.5 凭据禁传）：
//  1) 线协议：CreateRenderEDL / CreateGenProxy 的 Cmd、TaskType、PriorityLevel、payload 透传、
//     credentialId 携带，且下发报文中不出现 SMBUser / SMBPassword 明文（含任何 password 字样）；
//  2) 共享根解析：UNC 直传、斜杠 UNC 归一化、smbSharePath 前缀拼接、无配置时报
//     E_SMB_MOUNT_FAILED（可重试码）而不静默返回空路径；
//  3) 载荷守卫：空载荷 → E_PAYLOAD_MISSING，非法 JSON → E_EDL_INVALID，缺 sourceRoot/destRoot 直接失败；
//  4) FVCS 拒绝：透出 E_* 错误码供调度侧 classifyRenderError 分类；应答缺 TaskId → E_RENDER_FAILED；
//  5) 编译期断言 RemoteClient 实现 B-05 的 RenderDispatcher 接口。

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// fakeFVCSNode 扮演 FVCS 节点：完成 Auth 握手、记录并应答渲染下发指令。
type fakeFVCSNode struct {
	srv *httptest.Server

	mu       sync.Mutex
	received [][]byte
	reply    string
}

func newFakeFVCSNode(t *testing.T) *fakeFVCSNode {
	t.Helper()
	f := &fakeFVCSNode{}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		if _, _, err := conn.ReadMessage(); err != nil { // Auth
			return
		}
		authReply := `{"Code":0,"Msg":"ok","Data":{"HttpPort":18080,"ChunkSize":4}}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(authReply)); err != nil {
			return
		}

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.received = append(f.received, data)
			reply := f.reply
			f.mu.Unlock()
			if reply == "" {
				reply = `{"Code":0,"Msg":"ok","Data":{"TaskId":"t_1_abc123"}}`
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(reply)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeFVCSNode) setReply(reply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reply = reply
}

func (f *fakeFVCSNode) server(t *testing.T, id string) Server {
	t.Helper()
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(f.srv.URL, "http://"))
	if err != nil {
		t.Fatalf("解析测试服务器地址失败: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("解析测试服务器端口失败: %v", err)
	}
	return Server{ID: id, Name: "Fake FVCS", IP: host, Port: port, AuthKey: "k_test", Status: "online"}
}

// rawLast 返回最近一条业务指令的原始报文（用于口令字段禁传检查）。
func (f *fakeFVCSNode) rawLast(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.received) == 0 {
		t.Fatalf("FVCS 未收到任何业务指令")
	}
	return string(f.received[len(f.received)-1])
}

func (f *fakeFVCSNode) lastCmd(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(f.rawLast(t)), &m); err != nil {
		t.Fatalf("解析下发指令失败: %v", err)
	}
	return m
}

func rawStr(t *testing.T, m map[string]json.RawMessage, key string) string {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("下发指令缺少字段 %s（实际字段: %v）", key, keysOf(m))
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("字段 %s 不是字符串: %v", key, err)
	}
	return s
}

// rawStrOpt 读取可选字符串字段；字段缺失（omitempty 省略）视为空串。
func rawStrOpt(t *testing.T, m map[string]json.RawMessage, key string) string {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("字段 %s 不是字符串: %v", key, err)
	}
	return s
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------- 1) 线协议：分发指令结构 + 凭据禁传 ----------

func TestB06RenderDispatchWireContract(t *testing.T) {
	cases := []struct {
		name     string
		taskType TaskType
		payload  string
		dispatch func(rc *RemoteClient, srv Server, tk Task) (string, error)
		wantCmd  string
		wantPrio string
		wantSrc  string
		wantDst  string
	}{
		{
			name:     "RENDER_EDL 常规优先级",
			taskType: TaskTypeRenderEDL,
			payload:  `{"type":"RenderEDL","projectId":"p_1","sourceRoot":"videos","destRoot":"exports","output":"a.mp4","checksum":"abc"}`,
			dispatch: (*RemoteClient).CreateRenderEDL,
			wantCmd:  "CreateRenderEDL",
			wantPrio: renderPriorityNormal,
			wantSrc:  `\\192.168.1.10\media\videos`,
			wantDst:  `\\192.168.1.10\media\exports`,
		},
		{
			name:     "GEN_PROXY 低优先级",
			taskType: TaskTypeGenProxy,
			payload:  `{"type":"GenProxy","assetId":"a_1","srcFile":"videos/a.mp4","proxyFile":"proxies/a.mp4"}`,
			dispatch: (*RemoteClient).CreateGenProxy,
			wantCmd:  "CreateGenProxy",
			wantPrio: renderPriorityLow,
			wantSrc:  `\\192.168.1.10\media`,
			// 04 §3.2：代理输出根为素材共享根下的 _proxy 目录（不再留空）。
			wantDst: `\\192.168.1.10\media\_proxy`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node := newFakeFVCSNode(t)
			rc := NewRemoteClient()
			t.Cleanup(rc.CloseAll)
			// 仅配置共享根（不配 smbUser，避免依赖宿主机 samba 配置）
			rc.SetSettingsProvider(func() Settings {
				return Settings{SMBSharePath: `\\192.168.1.10\media`}
			})
			srv := node.server(t, "srv1")

			task := Task{
				ID: "t_1_abc123", TaskType: c.taskType, ServerID: srv.ID,
				PayloadJSON: c.payload, CredentialID: "cred_node_1",
			}
			got, err := c.dispatch(rc, srv, task)
			if err != nil {
				t.Fatalf("%s 下发失败: %v", c.wantCmd, err)
			}
			if got != "t_1_abc123" {
				t.Fatalf("远端任务 ID = %q, 期望复用本端 TaskId", got)
			}

			// 07 §5.2/§5.5：明文口令字段绝不允许出现在下发报文中
			raw := node.rawLast(t)
			for _, banned := range []string{"SMBUser", "SMBPassword", "smbUser", "smbPassword", "Password", "password"} {
				if strings.Contains(raw, banned) {
					t.Fatalf("下发报文出现口令字段 %q（07 §5.2/§5.5 禁止）: %s", banned, raw)
				}
			}

			cmd := node.lastCmd(t)
			if got := rawStr(t, cmd, "Cmd"); got != c.wantCmd {
				t.Fatalf("Cmd = %q, 期望 %q", got, c.wantCmd)
			}
			if got := rawStr(t, cmd, "TaskType"); got != string(c.taskType) {
				t.Fatalf("TaskType = %q, 期望 %q", got, c.taskType)
			}
			if got := rawStr(t, cmd, "PriorityLevel"); got != c.wantPrio {
				t.Fatalf("PriorityLevel = %q, 期望 %q", got, c.wantPrio)
			}
			if got := rawStr(t, cmd, "CredentialId"); got != "cred_node_1" {
				t.Fatalf("CredentialId = %q, 期望 cred_node_1", got)
			}
			if got := rawStr(t, cmd, "TaskId"); got != "t_1_abc123" {
				t.Fatalf("TaskId = %q, 期望 t_1_abc123", got)
			}
			if got := rawStr(t, cmd, "SMBPath"); got != c.wantSrc {
				t.Fatalf("SMBPath = %q, 期望 %q", got, c.wantSrc)
			}
			if got := rawStrOpt(t, cmd, "SMBOutputPath"); got != c.wantDst {
				t.Fatalf("SMBOutputPath = %q, 期望 %q", got, c.wantDst)
			}
			// 结构化载荷必须原样透传（不得重排/裁剪）
			var gotPayload, wantPayload interface{}
			if err := json.Unmarshal(cmd["Payload"], &gotPayload); err != nil {
				t.Fatalf("Payload 不是合法 JSON: %v", err)
			}
			if err := json.Unmarshal([]byte(c.payload), &wantPayload); err != nil {
				t.Fatalf("用例载荷非法: %v", err)
			}
			if !reflect.DeepEqual(gotPayload, wantPayload) {
				t.Fatalf("Payload 透传不一致:\n got=%v\nwant=%v", gotPayload, wantPayload)
			}
			// 直通 ffmpeg 参数与结构化载荷互斥（03 §5.2）：不得携带
			for _, k := range []string{"FFmpegArgs", "SourceFileName", "OutputName"} {
				if _, ok := cmd[k]; ok {
					t.Fatalf("渲染下发不得携带直通字段 %s（03 §5.2）", k)
				}
			}
		})
	}
}

// ---------- 2) 共享根 → UNC 解析 ----------

func TestB06SharePathResolution(t *testing.T) {
	rc := NewRemoteClient()
	rc.SetSettingsProvider(func() Settings {
		return Settings{SMBSharePath: `\\192.168.1.10\media`}
	})

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"UNC 直传", `\\10.0.0.5\share`, `\\10.0.0.5\share`},
		{"斜杠 UNC 归一化", `//10.0.0.5/media/exports/`, `\\10.0.0.5\media\exports`},
		{"共享相对路径拼接", "videos", `\\192.168.1.10\media\videos`},
		{"共享相对路径多级拼接", "videos/2024", `\\192.168.1.10\media\videos\2024`},
		{"共享相对路径带前导斜杠", "/exports", `\\192.168.1.10\media\exports`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := rc.toSMBUNC(c.in)
			if err != nil {
				t.Fatalf("toSMBUNC(%q) 报错: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("toSMBUNC(%q) = %q, 期望 %q", c.in, got, c.want)
			}
		})
	}

	// 无任何共享配置：必须报可重试的 E_SMB_MOUNT_FAILED，绝不猜测映射
	rcNoCfg := NewRemoteClient()
	rcNoCfg.SetSettingsProvider(func() Settings { return Settings{} })
	_, err := rcNoCfg.toSMBUNC("/media/videos")
	if err == nil || !strings.Contains(err.Error(), errCodeSMBMountFailed) {
		t.Fatalf("无共享配置时期望 %s，实际: %v", errCodeSMBMountFailed, err)
	}

	// 空路径由调用方判定缺失，不作为映射失败
	if got, err := rcNoCfg.toSMBUNC("   "); err != nil || got != "" {
		t.Fatalf("空路径应返回空值且无错误, got=%q err=%v", got, err)
	}
}

// ---------- 3) 载荷守卫 ----------

func TestB06RenderDispatchPayloadGuards(t *testing.T) {
	rc := NewRemoteClient()
	rc.SetSettingsProvider(func() Settings {
		return Settings{SMBSharePath: `\\192.168.1.10\media`}
	})
	srv := Server{ID: "srv1", IP: "127.0.0.1", Port: 1}

	if _, err := rc.CreateRenderEDL(srv, Task{ID: "t_empty"}); err == nil || !strings.Contains(err.Error(), errCodePayloadMissing) {
		t.Fatalf("空载荷期望 %s，实际: %v", errCodePayloadMissing, err)
	}
	if _, err := rc.CreateRenderEDL(srv, Task{ID: "t_bad", PayloadJSON: "{oops"}); err == nil || !strings.Contains(err.Error(), errCodeEDLInvalid) {
		t.Fatalf("非法 JSON 期望 %s，实际: %v", errCodeEDLInvalid, err)
	}
	if _, err := rc.CreateRenderEDL(srv, Task{ID: "t_noroot", PayloadJSON: `{"type":"RenderEDL"}`}); err == nil || !strings.Contains(err.Error(), errCodePayloadMissing) {
		t.Fatalf("缺 sourceRoot 期望 %s，实际: %v", errCodePayloadMissing, err)
	}
	if _, err := rc.CreateRenderEDL(srv, Task{ID: "t_nodest", PayloadJSON: `{"type":"RenderEDL","sourceRoot":"videos"}`}); err == nil || !strings.Contains(err.Error(), errCodePayloadMissing) {
		t.Fatalf("缺 destRoot 期望 %s，实际: %v", errCodePayloadMissing, err)
	}
	// GEN_PROXY 无显式根：未配置共享根时直接报可重试错误
	rcNoCfg := NewRemoteClient()
	if _, err := rcNoCfg.CreateGenProxy(srv, Task{ID: "t_proxy", PayloadJSON: `{"type":"GenProxy","srcFile":"a.mp4"}`}); err == nil || !strings.Contains(err.Error(), errCodeSMBMountFailed) {
		t.Fatalf("GEN_PROXY 缺共享根期望 %s，实际: %v", errCodeSMBMountFailed, err)
	}
}

// ---------- 4) FVCS 拒绝与异常应答 ----------

func TestB06RenderDispatchSurfacesFVCSErrors(t *testing.T) {
	node := newFakeFVCSNode(t)
	rc := NewRemoteClient()
	t.Cleanup(rc.CloseAll)
	rc.SetSettingsProvider(func() Settings {
		return Settings{SMBSharePath: `\\192.168.1.10\media`}
	})
	srv := node.server(t, "srv1")
	task := Task{
		ID: "t_1_abc123", TaskType: TaskTypeRenderEDL, ServerID: srv.ID,
		PayloadJSON:  `{"type":"RenderEDL","sourceRoot":"videos","destRoot":"exports","output":"a.mp4"}`,
		CredentialID: "cred_node_1",
	}

	// FVCS 业务拒绝：错误码须透出，供调度侧 classifyRenderError 判定冷却/终止（06 §4.3）
	node.setReply(`{"Code":500,"Msg":"E_SMB_MOUNT_FAILED: credential cred_node_1 mount failed","Data":null}`)
	if _, err := rc.CreateRenderEDL(srv, task); err == nil || !strings.Contains(err.Error(), errCodeSMBMountFailed) {
		t.Fatalf("FVCS 拒绝时期望透出 %s，实际: %v", errCodeSMBMountFailed, err)
	}

	// 应答缺 TaskId：不得把任务误标为已下发
	node.setReply(`{"Code":0,"Msg":"ok","Data":{"TaskId":""}}`)
	if _, err := rc.CreateRenderEDL(srv, task); err == nil || !strings.Contains(err.Error(), errCodeRenderFailed) {
		t.Fatalf("空 TaskId 期望 %s，实际: %v", errCodeRenderFailed, err)
	}
	rc.CloseConn(srv.ID)
}
