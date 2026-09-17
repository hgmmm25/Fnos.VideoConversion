// P1-3 验收测试（WSS 数据面加密，SECURITY.md §4）：
//  1) buildDialer 分支：明文不携带 TLS 配置；wss+CA 加载到 RootCAs；wss 无 CA 用系统根；
//     tlsSkipVerify=true 显式跳过校验（危险开关）；
//  2) 端到端：FVCC remote 以 wss:// 连接启用 TLS 的假 FVCS 节点，完成鉴权握手；
//  3) 证书信任语义：未信任 CA 握手失败；携带 CA 成功；skipVerify 放行。

package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
)

// newTLSFVCS 返回一个启用 TLS 的假 FVCS 节点（/ws 升级 + Auth 应答）。
func newTLSFVCS(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil { // Auth 指令
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"Code":0,"Msg":"ok","Data":{"HttpPort":18080,"ChunkSize":4}}`))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// certPoolOf 将 httptest TLS 服务端证书导出为可信任的 RootCAs。
func certPoolOf(srv *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return pool
}

func TestBuildDialerBranches(t *testing.T) {
	tests := []struct {
		name   string
		server Server
		wantTLS bool // 期望 TLSClientConfig 非 nil
		wantSkip bool // 期望 InsecureSkipVerify
	}{
		{"plain", Server{IP: "127.0.0.1", Port: 8080}, false, false},
		{"wss-system-roots", Server{IP: "127.0.0.1", Port: 8080, UseWSS: true}, true, false},
		{"wss-with-ca", Server{IP: "127.0.0.1", Port: 8080, UseWSS: true, TLSCACert: "unused.pem"}, true, false},
		{"wss-skip-verify", Server{IP: "127.0.0.1", Port: 8080, UseWSS: true, TLSSkipVerify: true}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := buildDialer(tt.server)
			gotTLS := d.TLSClientConfig != nil
			if gotTLS != tt.wantTLS {
				t.Fatalf("TLSClientConfig 存在 = %v, want %v", gotTLS, tt.wantTLS)
			}
			if tt.wantTLS {
				if d.TLSClientConfig.MinVersion != tls.VersionTLS12 {
					t.Errorf("MinVersion 应不低于 TLS1.2, got %v", d.TLSClientConfig.MinVersion)
				}
				if d.TLSClientConfig.InsecureSkipVerify != tt.wantSkip {
					t.Errorf("InsecureSkipVerify = %v, want %v", d.TLSClientConfig.InsecureSkipVerify, tt.wantSkip)
				}
			}
		})
	}
}

func TestDialAndAuthWSS(t *testing.T) {
	srv := newTLSFVCS(t)
	server := Server{
		ID:      "wss-node",
		IP:      "127.0.0.1",
		Port:    mustPort(t, srv.Listener.Addr().String()),
		AuthKey: "test-key",
		UseWSS:  true,
	}

	t.Run("trusted-CA-success", func(t *testing.T) {
		server.TLSCACert = ""
		server.TLSSkipVerify = false
		rc := &RemoteClient{conns: make(map[string]*wsConn)}
		// 走 buildDialer 同款信任：此处直接把 CA 池注入系统不可行，
		// 用 TLSCACert 临时证书文件路径验证「CA 加载 → 握手成功」路径。
		certFile := writeCertFile(t, srv.Certificate())
		server.TLSCACert = certFile
		httpPort, chunkSize, err := rc.dialAndAuth(server, nil)
		if err != nil {
			t.Fatalf("wss + 信任 CA 应连接成功: %v", err)
		}
		if httpPort != 18080 || chunkSize != 4 {
			t.Fatalf("鉴权结果不符: httpPort=%d chunkSize=%d", httpPort, chunkSize)
		}
		rc.CloseConn(server.ID)
	})

	t.Run("untrusted-fails", func(t *testing.T) {
		server.TLSCACert = ""
		server.TLSSkipVerify = false
		rc := &RemoteClient{conns: make(map[string]*wsConn)}
		_, _, err := rc.dialAndAuth(server, nil)
		if err == nil {
			t.Fatal("未信任 CA 的 wss 应握手失败，实际成功")
		}
	})

	t.Run("skip-verify-success", func(t *testing.T) {
		server.TLSCACert = ""
		server.TLSSkipVerify = true
		rc := &RemoteClient{conns: make(map[string]*wsConn)}
		if _, _, err := rc.dialAndAuth(server, nil); err != nil {
			t.Fatalf("tlsSkipVerify=true 应放行: %v", err)
		}
		rc.CloseConn(server.ID)
	})
}

func TestDialAndAuthPlainCompat(t *testing.T) {
	// 明文兼容：未开启 WSS 的节点仍走 ws://（既有链路不被破坏）。
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"Code":0,"Msg":"ok","Data":{"HttpPort":18081,"ChunkSize":8}}`))
	}))
	defer srv.Close()

	rc := &RemoteClient{conns: make(map[string]*wsConn)}
	server := Server{ID: "plain-node", IP: "127.0.0.1", Port: mustPort(t, srv.Listener.Addr().String()), AuthKey: "k"}
	httpPort, chunkSize, err := rc.dialAndAuth(server, nil)
	if err != nil {
		t.Fatalf("明文链路应保持可用: %v", err)
	}
	if httpPort != 18081 || chunkSize != 8 {
		t.Fatalf("鉴权结果不符: httpPort=%d chunkSize=%d", httpPort, chunkSize)
	}
	rc.CloseConn(server.ID)
}

// mustPort 从 "127.0.0.1:PORT" 提取端口号。
func mustPort(t *testing.T, addr string) int {
	t.Helper()
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			var port int
			for j := i + 1; j < len(addr); j++ {
				port = port*10 + int(addr[j]-'0')
			}
			return port
		}
	}
	t.Fatalf("无法从地址解析端口: %s", addr)
	return 0
}

// writeCertFile 将 x509 证书以 PEM 格式写入临时文件并返回路径。
func writeCertFile(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if pemBytes == nil {
		t.Fatal("证书 PEM 编码失败")
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pemBytes, 0600); err != nil {
		t.Fatalf("写 CA 文件失败: %v", err)
	}
	return path
}
