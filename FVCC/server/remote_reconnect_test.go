// B-09 验收测试（WS 断线自愈 + 单飞 + 优雅关闭）：
//  1) 单飞：同一节点并发 Connect 只产生一次拨号（FVCS 侧不再出现 1s 内多连，
//     避免打满 FVCS 节点级 WS 连接上限导致新任务被拒）；
//  2) 断线自愈：FVCS 异常断链（无 Close 帧，close 1006）后自动指数退避重连，
//     无需人工在管理端点「测试连接」；
//  3) 主动关闭停止自愈：CloseConn 后不再自动重连；
//  4) 优雅关闭：替换/关闭旧连接时先发 WS Close 帧，对端读到正常关闭而非 1006。

package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// countingFVCS 统计接受次数的假 FVCS 节点。
// dropAfterAuth=true 时在鉴权应答后立即关闭底层 TCP（模拟对端异常断链，不发 Close 帧）。
type countingFVCS struct {
	srv    *httptest.Server
	accepts int32

	dropAfterAuth bool
	mu            sync.Mutex
	closeCodes    []int
}

func newCountingFVCS(t *testing.T, dropAfterAuth bool) *countingFVCS {
	t.Helper()
	f := &countingFVCS{dropAfterAuth: dropAfterAuth}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		atomic.AddInt32(&f.accepts, 1)
		defer conn.Close()

		if _, _, err := conn.ReadMessage(); err != nil { // Auth 指令
			return
		}
		authReply := `{"Code":0,"Msg":"ok","Data":{"HttpPort":18080,"ChunkSize":4}}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(authReply)); err != nil {
			return
		}
		if dropAfterAuth {
			// 模拟对端异常断开：直接关底层连接，不发 Close 帧 → 客户端应读到 close 1006
			_ = conn.UnderlyingConn().Close()
			return
		}
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					f.mu.Lock()
					f.closeCodes = append(f.closeCodes, websocket.CloseNormalClosure)
					f.mu.Unlock()
				}
				return
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *countingFVCS) acceptCount() int32 {
	return atomic.LoadInt32(&f.accepts)
}

func (f *countingFVCS) server(t *testing.T, id string) Server {
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

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// TestB09SingleFlightConnect：并发 Connect 只拨号一次。
func TestB09SingleFlightConnect(t *testing.T) {
	f := newCountingFVCS(t, false)
	rc := NewRemoteClient()
	sv := f.server(t, "srv_sf")

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := rc.Connect(sv)
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发 Connect #%d 失败: %v", i, err)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if got := f.acceptCount(); got != 1 {
		t.Fatalf("并发 %d 路 Connect 应只产生 1 次拨号，实际 FVCS 收到 %d 次", n, got)
	}
	rc.CloseConn(sv.ID)
}

// TestB09ReconnectAfterAbnormalDrop：FVCS 异常断链后自动重连。
func TestB09ReconnectAfterAbnormalDrop(t *testing.T) {
	f := newCountingFVCS(t, true)
	rc := NewRemoteClient()

	var disc int32
	rc.OnDisconnect = func(serverID string) { atomic.AddInt32(&disc, 1) }

	sv := f.server(t, "srv_rc")
	if _, _, err := rc.Connect(sv); err != nil {
		t.Fatalf("首次 Connect 失败: %v", err)
	}
	if got := f.acceptCount(); got != 1 {
		t.Fatalf("首次 Connect 后应只有 1 次拨号，实际 %d", got)
	}

	// 服务端已断链 → 客户端应在退避后自动重连（1s 内首轮重试）。
	if !waitFor(t, 5*time.Second, func() bool { return f.acceptCount() >= 2 }) {
		t.Fatalf("断线后未自动重连，accepts=%d, disconnect=%d", f.acceptCount(), atomic.LoadInt32(&disc))
	}
	if atomic.LoadInt32(&disc) == 0 {
		t.Fatalf("断线未触发 OnDisconnect 回调")
	}

	rc.CloseConn(sv.ID)
	time.Sleep(300 * time.Millisecond)
}

// TestB09GracefulCloseSendsCloseFrame：CloseConn 发送 WS Close 帧，对端记录正常关闭。
func TestB09GracefulCloseSendsCloseFrame(t *testing.T) {
	f := newCountingFVCS(t, false)
	rc := NewRemoteClient()
	sv := f.server(t, "srv_gc")

	if _, _, err := rc.Connect(sv); err != nil {
		t.Fatalf("Connect 失败: %v", err)
	}
	rc.CloseConn(sv.ID)

	if !waitFor(t, 2*time.Second, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.closeCodes) > 0
	}) {
		t.Fatalf("对端未收到 WS Close 帧（仍将表现为 1006 异常断链）")
	}
}

// TestB09ActiveCloseStopsReconnect：主动 CloseConn 后不再自动重连。
func TestB09ActiveCloseStopsReconnect(t *testing.T) {
	f := newCountingFVCS(t, false)
	rc := NewRemoteClient()
	sv := f.server(t, "srv_ac")

	if _, _, err := rc.Connect(sv); err != nil {
		t.Fatalf("Connect 失败: %v", err)
	}
	rc.CloseConn(sv.ID)

	time.Sleep(2500 * time.Millisecond)
	if got := f.acceptCount(); got != 1 {
		t.Fatalf("主动关闭后不应自动重连，实际 FVCS 收到 %d 次连接", got)
	}
}
