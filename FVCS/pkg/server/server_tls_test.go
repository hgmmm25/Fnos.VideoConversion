package server

import (
	"testing"

	"Fnos.VC_Service/pkg/config"
)

// TestValidateWSConfig 校验 WSS 半配置拒绝语义（P1-3 / SECURITY.md §4）：
// 证书与私钥必须成对出现，防止部署方只配其一误以为已启用加密。
func TestValidateWSConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		wantErr bool
	}{
		{"empty-plain", &config.Config{}, false},
		{"pair-ok", &config.Config{WSTLSCert: "cert.pem", WSTLSKey: "key.pem"}, false},
		{"cert-only", &config.Config{WSTLSCert: "cert.pem"}, true},
		{"key-only", &config.Config{WSTLSKey: "key.pem"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWSConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateWSConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestStartWebSocketServerTLSBranch 端到端验证：配置自签证书后 WS 端口以 WSS 服务，
// 客户端携带 CA 信任即可完成 TLS 握手并升级到 WebSocket；不信任时握手失败。
func TestStartWebSocketServerTLSBranch(t *testing.T) {
	cfg := config.Get()
	prevCert, prevKey := cfg.WSTLSCert, cfg.WSTLSKey
	defer func() {
		cfg.WSTLSCert, cfg.WSTLSKey = prevCert, prevKey
	}()

	certFile, keyFile, caPool := genSelfSignedCert(t)
	cfg.WSTLSCert, cfg.WSTLSKey = certFile, keyFile
	if err := validateWSConfig(cfg); err != nil {
		t.Fatalf("validateWSConfig 不应报错: %v", err)
	}

	// 独立 TLS 监听（不占用全局 serverInstance）：验证 FVCS 使用的
	// http.Server.ListenAndServeTLS 路径可被 wss 客户端正常握手。
	ln, err := tlsListen(certFile, keyFile)
	if err != nil {
		t.Fatalf("TLS 监听失败: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// 信任 CA 的客户端：应握手成功
	wsURL := "wss://" + addr + "/ws"
	conn, _, err := dialWSS(wsURL, caPool, false)
	if err != nil {
		t.Fatalf("携带 CA 的 wss 客户端握手失败: %v", err)
	}
	conn.Close()

	// 不信任 CA 的客户端：应握手失败（TLS 证书校验错误）
	_, _, err = dialWSS(wsURL, nil, false)
	if err == nil {
		t.Fatal("未信任 CA 的 wss 客户端应握手失败，实际成功")
	}

	// SkipVerify 客户端：应握手成功（供显式降级场景测试）
	conn2, _, err := dialWSS(wsURL, nil, true)
	if err != nil {
		t.Fatalf("SkipVerify wss 客户端握手失败: %v", err)
	}
	conn2.Close()
}
