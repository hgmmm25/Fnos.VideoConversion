package main

// D-04 端到端安全链验收（10 §3「下一步」E2E-08）：
// 在「完整生产路由（含 securityHeaders / 鉴权限流 / 渲染提交限流）+ D-02 严格闸门环境 +
// 全局审计注入」下，用真实 HTTP 请求走通整条拒绝链，验证：
//  1) 路径穿越 / 编码穿越 / 控制字符注入 / 输出名穿越 四类向量均被拒绝，且拒绝码与状态码符合契约；
//  2) 拒绝不产生任何副作用——任务零入队（闸门先于入队）；
//  3) 拒绝类错误码（07 §7：E_EDL_INVALID / E_PAYLOAD_INVALID / E_ASSET_NOT_IN_ROOT）逐次落入
//     audit_log，非拒绝类（如 E_ASSET_MISSING）不记账；
//  4) 审计 Detail 保持脱敏（不含素材根绝对路径）；拒绝响应仍带全安全头（中间件早于业务 handler）；
//  5) 越权扫描告警阈值以真实请求触发：E_ASSET_NOT_IN_ROOT 第 3 次落 1 条 security.alert（仅 1 条）；
//  6) 正向对照：合法提交经同一链路放行并正常入队。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fvcc/internal/security"
)

// e2eSecurityHeaders 拒绝响应也必须带全的 D-04 安全头（07 §4.4）。
var e2eSecurityHeaders = map[string]string{
	"X-Content-Type-Options": "nosniff",
	"Referrer-Policy":        "no-referrer",
	"X-Frame-Options":        "SAMEORIGIN",
}

func assertE2ESecurityHeaders(t *testing.T, w interface {
	Header() http.Header
}) {
	t.Helper()
	for k, want := range e2eSecurityHeaders {
		if got := w.Header().Get(k); got != want {
			t.Errorf("拒绝路径安全头 %s = %q，期望 %q", k, got, want)
		}
	}
}

// TestD04SecurityE2ERejectChain 全链路拒绝链（向量 × 审计 × 无副作用 × 安全头）。
func TestD04SecurityE2ERejectChain(t *testing.T) {
	resetRateLimiters()
	r, s, srcDir, _, _ := newD02RenderEnv(t)
	withGlobalStore(t, s)
	resetAlertCounters := security.ResetAlertCounters
	resetAlertCounters()
	t.Cleanup(resetAlertCounters)

	cases := []struct {
		name      string
		presetKey string // 空表示用 copy_same_source
		file      string // 覆盖到全部片段；空表示沿用合法素材
		output    string
		mutate    func([]EDLClip) []EDLClip // 改时间码等结构时使用
		// 状态码 / 错误码 / 是否属 07 §7 拒绝记账类
		wantStatus int
		wantCode   string
		wantAudit  bool
	}{
		{"raw-traversal", "", "../../etc/passwd", "e2e_raw", nil,
			http.StatusBadRequest, errCodeEDLInvalid, true},
		{"absolute-path", "", "/media/videos/../exports/x.mp4", "e2e_abs", nil,
			http.StatusBadRequest, errCodeEDLInvalid, true},
		{"encoded-traversal", "", "a%2F..%2F..%2Fsecret.mp4", "e2e_enc", nil,
			http.StatusNotFound, errCodeAssetMissing, false},
		{"control-char", "", "demo/a\n_01.mp4", "e2e_ctl", nil,
			http.StatusBadRequest, errCodeEDLInvalid, true},
		{"bad-timecode", "", "", "e2e_tc", func(c []EDLClip) []EDLClip {
			c[0].OutMs = c[0].InMs // 出点不晚于入点
			return c
		}, http.StatusBadRequest, errCodeEDLInvalid, true},
		{"bad-preset", "libx264 -y /etc/x", "", "e2e_prf", nil,
			http.StatusBadRequest, errCodeProfileInvalid, false},
		{"output-traversal", "", "", "../../windows/system32/x", nil,
			http.StatusBadRequest, errCodeEDLInvalid, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clips := renderTestClips()
			if tc.mutate != nil {
				clips = tc.mutate(clips)
			}
			if tc.file != "" {
				for i := range clips {
					clips[i].File = tc.file
				}
			}
			preset := tc.presetKey
			if preset == "" {
				preset = "copy_same_source"
			}
			p := mustCreateRenderProject(t, s, "e2e_"+tc.name, renderTestTimeline(), clips)

			before := len(s.ListAudit(0, security.AuditActionValidateReject))
			w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+p.ID+"/render",
				`{"presetKey":"`+preset+`","outputName":"`+tc.output+`"}`)

			if w.Code != tc.wantStatus {
				t.Fatalf("状态码应为 %d，实际 %d（body=%s）", tc.wantStatus, w.Code, w.Body.String())
			}
			var resp struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("响应不可解析: %v (%s)", err, w.Body.String())
			}
			if resp.Code != tc.wantCode {
				t.Fatalf("错误码应为 %s，实际 %q", tc.wantCode, resp.Code)
			}
			assertE2ESecurityHeaders(t, w)

			// 拒绝必须发生在入队之前：不得留下任何任务
			if tasks := s.GetTasks(); len(tasks) != 0 {
				t.Fatalf("拒绝请求不得入队任务，实际 %d 个: %+v", len(tasks), tasks)
			}

			rejects := s.ListAudit(0, security.AuditActionValidateReject)
			wantDelta := 0
			if tc.wantAudit {
				wantDelta = 1
			}
			if got := len(rejects) - before; got != wantDelta {
				t.Fatalf("validate.reject 增量应为 %d，实际 %d", wantDelta, got)
			}
			if !tc.wantAudit {
				return
			}
			// ListAudit 按 At 倒序返回（最新在前），故取 [0] 即为本次请求的拒绝记录。
			last := rejects[0]
			if last.Result != tc.wantCode {
				t.Errorf("审计 Result 应为拒绝码 %s，实际 %q", tc.wantCode, last.Result)
			}
			if last.Target != p.ID {
				t.Errorf("审计 Target 应为项目 id %s，实际 %q", p.ID, last.Target)
			}
			if !strings.Contains(last.Detail, "/api/edl/projects/"+p.ID+"/render") {
				t.Errorf("审计 Detail 应记录方法与路径，实际 %q", last.Detail)
			}
			// 脱敏：Detail 不得泄露素材根绝对路径（07 §7）
			if srcDir != "" && strings.Contains(last.Detail, srcDir) {
				t.Errorf("审计 Detail 泄露素材根绝对路径: %q", last.Detail)
			}
			if alerts := s.ListAudit(0, security.AuditActionSecurityAlert); len(alerts) != 0 {
				t.Errorf("本轮向量不应触发告警（阈值 %d），实际 %d 条", security.AlertAssetScanThreshold, len(alerts))
			}
		})
	}

	// 正向对照：同一链路下合法提交放行且入队 1 个任务（换独立环境，保证计数干净）。
	t.Run("legal-submit-ok", func(t *testing.T) {
		r2, s2, _, _, _ := newD02RenderEnv(t)
		withGlobalStore(t, s2)
		p := mustCreateRenderProject(t, s2, "e2e_ok", renderTestTimeline(), renderTestClips())
		w := doEDLRequest(t, r2, http.MethodPost, "/api/edl/projects/"+p.ID+"/render",
			`{"presetKey":"copy_same_source","outputName":"e2e_ok"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("合法提交应 200，实际 %d（body=%s）", w.Code, w.Body.String())
		}
		assertE2ESecurityHeaders(t, w)
		if tasks := s2.GetTasks(); len(tasks) != 1 {
			t.Fatalf("合法提交应入队 1 个任务，实际 %d", len(tasks))
		}
		if got := len(s2.ListAudit(0, security.AuditActionValidateReject)); got != 0 {
			t.Fatalf("合法提交不应产生拒绝审计，实际 %d 条", got)
		}
	})
}

// TestD04SecurityE2EAssetScanAlert 越权扫描告警阈值：以真实 HTTP 请求（软链接逃逸）触发。
func TestD04SecurityE2EAssetScanAlert(t *testing.T) {
	resetRateLimiters()
	r, s, srcDir, _, _ := newD02RenderEnv(t)
	withGlobalStore(t, s)
	resetAlertCounters := security.ResetAlertCounters
	resetAlertCounters()
	t.Cleanup(resetAlertCounters)

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.mp4"), make([]byte, 32), 0o644); err != nil {
		t.Fatalf("写入外部素材失败: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(srcDir, "link")); err != nil {
		t.Skipf("当前环境不支持创建软链接，跳过越权告警端到端用例: %v", err)
	}

	clips := renderTestClips()
	clips[0].File = "link/secret.mp4"
	clips[1].File = "link/secret.mp4"
	p := mustCreateRenderProject(t, s, "e2e_scan", renderTestTimeline(), clips)

	for i := 1; i <= security.AlertAssetScanThreshold; i++ {
		w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+p.ID+"/render",
			`{"presetKey":"copy_same_source","outputName":"e2e_scan"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("第 %d 次越权请求应 403，实际 %d（body=%s）", i, w.Code, w.Body.String())
		}
		var resp struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不可解析: %v", err)
		}
		if resp.Code != errCodeAssetNotInRoot {
			t.Fatalf("第 %d 次错误码应为 %s，实际 %q", i, errCodeAssetNotInRoot, resp.Code)
		}

		alerts := s.ListAudit(0, security.AuditActionSecurityAlert)
		if i < security.AlertAssetScanThreshold && len(alerts) != 0 {
			t.Fatalf("未达阈值（第 %d 次）不应告警，实际 %d 条", i, len(alerts))
		}
		if i == security.AlertAssetScanThreshold {
			if len(alerts) != 1 {
				t.Fatalf("达阈值应恰好 1 条 %s，实际 %d 条", security.AuditActionSecurityAlert, len(alerts))
			}
			if alerts[0].Result != errCodeAssetNotInRoot || !strings.Contains(alerts[0].Detail, "疑似路径越权扫描") {
				t.Fatalf("告警审计字段异常: %+v", alerts[0])
			}
		}
	}

	if got := len(s.ListAudit(0, security.AuditActionValidateReject)); got != security.AlertAssetScanThreshold {
		t.Fatalf("应累计 %d 条拒绝审计，实际 %d 条", security.AlertAssetScanThreshold, got)
	}
	if tasks := s.GetTasks(); len(tasks) != 0 {
		t.Fatalf("越权请求不得入队任务，实际 %d 个", len(tasks))
	}
}
