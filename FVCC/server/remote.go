package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"fvcc/logger"
	"fvcc/smbshare"
)

// WS 断线自愈参数（B-09）：指数退避 1s→2s→4s…上限 30s，节点抖动时避免风暴式重连。
const (
	reconnectBaseDelay = 1 * time.Second
	reconnectMaxDelay  = 30 * time.Second
	wsCloseTimeout     = 2 * time.Second
)

type RemoteClient struct {
	mu    sync.Mutex
	conns map[string]*wsConn
	// connMu 节点级连接互斥（单飞）：同一节点并发 Connect 只放行一次拨号，其余等待复用，
	// 消除 FVCS 侧同一节点短时间重复连接（日志中 1s 内 3 连即由此产生）。
	connMu map[string]*sync.Mutex
	// serverCfg 节点连接参数快照：断线自愈重连（reconnectLoop）无需调用方再次传入。
	serverCfg  map[string]Server
	httpClient *http.Client
	// OnPushProgress FVCS 经 WS 回传的渲染/转码进度（B-07：由 main.go 接到调度侧）。
	OnPushProgress func(serverID string, p RemoteProgress)
	// OnPushHello FVCS 建链后上报的节点能力（B-08：由 main.go 接到 ApplyNodeHello 落库）。
	OnPushHello  func(serverID string, caps NodeCaps)
	OnDisconnect func(serverID string)
	// getSettings 设置读取器（B-06，main.go 注入）：渲染下发时用 smbSharePath / smbUser
	// 把共享根解析为 FVCS 可挂载的 UNC。未注入时仅支持 UNC 直传。
	getSettings func() Settings
}

func NewRemoteClient() *RemoteClient {
	return &RemoteClient{
		conns:     map[string]*wsConn{},
		connMu:    map[string]*sync.Mutex{},
		serverCfg: map[string]Server{},
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
	}
}

type wsConn struct {
	conn      *websocket.Conn
	serverID  string
	httpPort  int
	chunkSize int
	mu        sync.Mutex
	done      chan struct{}
	pingDone  chan struct{}
	pumpDone  chan struct{}
	respCh    chan wsResp
}

type wsCmd struct {
	Cmd            string `json:"Cmd"`
	Key            string `json:"Key,omitempty"`
	SourceFileName string `json:"SourceFileName,omitempty"`
	OutputName     string `json:"OutputName,omitempty"`
	FFmpegArgs     string `json:"FFmpegArgs,omitempty"`
	TaskId         string `json:"TaskId,omitempty"`
	SMBPath        string `json:"SMBPath,omitempty"`
	SMBUser        string `json:"SMBUser,omitempty"`
	SMBPassword    string `json:"SMBPassword,omitempty"`
	// ===== B-06：渲染类任务下发字段（06 §4.2、03 §3.1）=====
	// 安全红线（07 §5.2/§5.5）：渲染下发严禁设置 SMBUser / SMBPassword，
	// 挂载凭据由 credentialId 在 FVCS 侧解析（见 CreateRenderEDL/CreateGenProxy）。
	TaskType      string          `json:"TaskType,omitempty"`
	PriorityLevel string          `json:"PriorityLevel,omitempty"`
	Payload       json.RawMessage `json:"Payload,omitempty"`
	SMBOutputPath string          `json:"SMBOutputPath,omitempty"`
	CredentialID  string          `json:"CredentialId,omitempty"`
	TraceId       string          `json:"TraceId,omitempty"` // 可观测性（P2-1）：跨端链路追踪 ID
}

type wsResp struct {
	Code int             `json:"Code"`
	Msg  string          `json:"Msg"`
	Cmd  string          `json:"Cmd"`
	Data json.RawMessage `json:"Data"`
}

type authData struct {
	HttpPort  int `json:"HttpPort"`
	ChunkSize int `json:"ChunkSize"`
}

type createTaskData struct {
	TaskId string `json:"TaskId"`
}

// RemoteProgress FVCS 经 WS 回传的进度（06 §4.2 进度 loop / §6 事件聚合，B-07）。
// 除 progress 外补齐 stage/seg/out_time_ms/total_ms/speed，供调度侧落库并聚合为前端文案。
type RemoteProgress struct {
	TaskID    string  // FVCS 侧任务 ID（B-06 约定复用本端 TaskID）
	Progress  float64 // 0-100
	Stage     string  // prepare|segment|concat|mux|finalize
	SegIndex  int     // 当前分段序号（1-based）
	SegTotal  int     // 分段总数
	OutTimeMs int64   // 输出时间（毫秒）
	TotalMs   int64   // 段级进度加权基准（Σ(outMs-inMs)）
	Speed     string  // ffmpeg 倍速
	Message   string  // 节点侧附加说明（可选）
}

// progressPush FVCS Progress 报文的线上结构（06 §4.2）。
type progressPush struct {
	TaskID    string  `json:"task_id"`
	Progress  float64 `json:"progress"`
	Stage     string  `json:"stage,omitempty"`
	Seg       int     `json:"seg,omitempty"`
	SegTotal  int     `json:"seg_total,omitempty"`
	OutTimeMs int64   `json:"out_time_ms,omitempty"`
	TotalMs   int64   `json:"total_ms,omitempty"`
	Speed     string  `json:"speed,omitempty"`
	Msg       string  `json:"msg,omitempty"`
}

// HelloPush FVCS Hello 报文的线上结构（B-08，06 §5.1；内部协议段，snake_case 边界见 API_CONTRACT §3.2）。
// 兼容两种风格：camelCase（NodeCaps 直出）与 snake_case（节点侧习惯），
// 并允许能力对象嵌套在 caps / nodeCaps 字段内。
type helloPush struct {
	ServerID      string          `json:"server_id,omitempty"`
	AgentVersion  string          `json:"agent_version,omitempty"`
	OS            string          `json:"os,omitempty"`
	CPUCores      int             `json:"cpu_cores,omitempty"`
	GPU           []GPUInfo       `json:"gpu,omitempty"`
	Encoders      []string        `json:"encoders,omitempty"`
	MaxConcurrent int             `json:"max_concurrent,omitempty"`
	FFmpegPath    string          `json:"ffmpeg_path,omitempty"`
	Caps          json.RawMessage `json:"caps,omitempty"`
	NodeCaps      json.RawMessage `json:"nodeCaps,omitempty"`
}

// parseHelloCaps 解析 Hello 能力上报为 NodeCaps（B-08）。serverID 为 WS 连接身份，
// 作为载荷缺失时的权威兜底。ok=false 表示载荷不携带任何能力信息（忽略，不落库）。
// 逐字段合并 camelCase / snake_case 两种命名，camelCase 优先，缺失项回落到 snake_case。
func parseHelloCaps(serverID string, data json.RawMessage) (NodeCaps, bool) {
	inner := data
	var wrap helloPush
	if err := json.Unmarshal(data, &wrap); err == nil {
		if len(wrap.Caps) > 0 {
			inner = wrap.Caps
		} else if len(wrap.NodeCaps) > 0 {
			inner = wrap.NodeCaps
		}
	}

	var camel NodeCaps
	_ = json.Unmarshal(inner, &camel)
	var snake helloPush
	_ = json.Unmarshal(inner, &snake)

	caps := camel
	if caps.AgentVersion == "" {
		caps.AgentVersion = snake.AgentVersion
	}
	if caps.OS == "" {
		caps.OS = snake.OS
	}
	if caps.CPUCores == 0 {
		caps.CPUCores = snake.CPUCores
	}
	if caps.MaxConcurrent == 0 {
		caps.MaxConcurrent = snake.MaxConcurrent
	}
	if len(caps.Encoders) == 0 {
		caps.Encoders = snake.Encoders
	}
	if len(caps.GPU) == 0 {
		caps.GPU = snake.GPU
	}
	if caps.FFmpegPath == "" {
		caps.FFmpegPath = snake.FFmpegPath
	}
	if caps.ServerID == "" {
		caps.ServerID = snake.ServerID
	}
	if caps.ServerID == "" {
		caps.ServerID = serverID
	}

	if caps.AgentVersion == "" && caps.OS == "" && caps.CPUCores == 0 && caps.MaxConcurrent == 0 &&
		len(caps.Encoders) == 0 && len(caps.GPU) == 0 && caps.FFmpegPath == "" {
		return NodeCaps{}, false
	}
	return caps, true
}

func (rc *RemoteClient) Connect(server Server) (int, int, error) {
	// 快照节点连接参数：断线自愈无需调用方再次传入。
	rc.mu.Lock()
	if rc.serverCfg == nil {
		rc.serverCfg = map[string]Server{}
	}
	rc.serverCfg[server.ID] = server
	rc.mu.Unlock()

	// 单飞：同一节点并发 Connect 只放行一次拨号，其余排队复用已有连接。
	mu := rc.connectMuFor(server.ID)
	mu.Lock()
	defer mu.Unlock()

	// 双检：排队期间可能已有连接建立。
	rc.mu.Lock()
	wc, exists := rc.conns[server.ID]
	rc.mu.Unlock()
	if exists && wc != nil {
		return wc.httpPort, wc.chunkSize, nil
	}

	httpPort, chunkSize, err := rc.dialAndAuth(server, wc)
	if err != nil {
		return 0, 0, err
	}
	return httpPort, chunkSize, nil
}

func (rc *RemoteClient) connectMuFor(serverID string) *sync.Mutex {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.connMu == nil {
		rc.connMu = map[string]*sync.Mutex{}
	}
	mu, ok := rc.connMu[serverID]
	if !ok {
		mu = &sync.Mutex{}
		rc.connMu[serverID] = mu
	}
	return mu
}

// buildDialer 按节点 WSS 配置构造拨号器（改进方向 P1-3 / SECURITY.md §4）：
//   - 明文：不设置 TLSClientConfig；
//   - wss + TLSCACert：加载指定 CA 至 RootCAs（自签/内网 CA）；
//   - wss + 无 CA：使用系统根证书池（自签 CA 需导入系统信任）；
//   - TLSSkipVerify=true：显式跳过证书校验（危险开关，调用方已输出 WARN）。
func buildDialer(server Server) *websocket.Dialer {
	d := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   1024 * 1024,
		WriteBufferSize:  1024 * 1024,
	}
	if !server.UseWSS {
		return d
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if server.TLSCACert != "" {
		pemData, err := os.ReadFile(server.TLSCACert)
		if err != nil {
			logger.Warn("remote", "read tlsCACert failed (fallback to system roots): server=%s path=%s err=%v", server.ID, server.TLSCACert, err)
		} else {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pemData) {
				logger.Warn("remote", "tlsCACert contains no valid PEM (fallback to system roots): server=%s", server.ID)
			} else {
				tc.RootCAs = pool
			}
		}
	}
	if server.TLSSkipVerify {
		logger.Warn("remote", "TLS certificate verification DISABLED for server=%s (tlsSkipVerify=true, production forbidden)", server.ID)
		tc.InsecureSkipVerify = true //nolint:gosec // 显式危险开关，由部署方配置
	}
	d.TLSClientConfig = tc
	return d
}

// dialAndAuth 实际拨号+鉴权+登记连接；old 为同节点将被替换的旧连接（若存在），
// 新连接就绪后优雅关闭旧连接，避免 ghost 连接残留占用 FVCS 侧连接配额。
func (rc *RemoteClient) dialAndAuth(server Server, old *wsConn) (int, int, error) {
	scheme := "ws"
	if server.UseWSS {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s:%d/ws", scheme, server.IP, server.Port)
	dialer := buildDialer(server)

	logger.Debug("remote", "connecting to server %s at %s", server.ID, wsURL)
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		logger.Warn("remote", "WS connection failed: server=%s url=%s err=%v", server.ID, wsURL, err)
		return 0, 0, fmt.Errorf("WS 连接失败: %w", err)
	}

	authCmd := wsCmd{Cmd: "Auth", Key: server.AuthKey}
	if err := conn.WriteJSON(authCmd); err != nil {
		conn.Close()
		logger.Warn("remote", "auth command failed: server=%s err=%v", server.ID, err)
		return 0, 0, fmt.Errorf("发送鉴权指令失败: %w", err)
	}

	var resp wsResp
	if err := readAuthResp(conn, &resp); err != nil {
		conn.Close()
		logger.Warn("remote", "auth response failed: server=%s err=%v", server.ID, err)
		return 0, 0, fmt.Errorf("读取鉴权响应失败: %w", err)
	}
	if resp.Code != 0 {
		conn.Close()
		logger.Warn("remote", "auth failed: server=%s msg=%s", server.ID, resp.Msg)
		return 0, 0, fmt.Errorf("鉴权失败: %s", resp.Msg)
	}

	var ad authData
	if err := json.Unmarshal(resp.Data, &ad); err != nil {
		conn.Close()
		logger.Warn("remote", "auth data parse failed: server=%s err=%v", server.ID, err)
		return 0, 0, fmt.Errorf("解析 HttpPort 失败: %w", err)
	}

	chunkSize := ad.ChunkSize
	if chunkSize < 1 {
		chunkSize = 4
	}

	wc := &wsConn{
		conn:      conn,
		serverID:  server.ID,
		httpPort:  ad.HttpPort,
		chunkSize: chunkSize,
		done:      make(chan struct{}),
		pingDone:  make(chan struct{}),
		pumpDone:  make(chan struct{}),
		respCh:    make(chan wsResp, 16),
	}
	rc.mu.Lock()
	rc.conns[server.ID] = wc
	rc.mu.Unlock()

	go wc.pingLoop(server.ID)
	go rc.readPump(server.ID, wc)

	// 替换旧连接：关闭 done 使 readPump/pingLoop 退出，并先发 WS Close 帧再关底层连接，
	// FVCS 侧记录为「client closed」而非 1006 异常断链。
	if old != nil {
		close(old.done)
		gracefulCloseWS(old.conn)
		old.conn.Close()
		<-old.pumpDone
		<-old.pingDone
	}

	logger.Info("remote", "connected to %s:%d, httpPort=%d, chunkSize=%dMB", server.IP, server.Port, ad.HttpPort, chunkSize)
	return ad.HttpPort, chunkSize, nil
}

func readAuthResp(c *websocket.Conn, resp *wsResp) error {
	for {
		if err := c.ReadJSON(resp); err != nil {
			return err
		}
		if resp.Cmd == "" {
			return nil
		}
	}
}

func (rc *RemoteClient) readPump(serverID string, wc *wsConn) {
	defer close(wc.pumpDone)

	for {
		select {
		case <-wc.done:
			return
		default:
		}

		var resp wsResp
		if err := wc.conn.ReadJSON(&resp); err != nil {
			select {
			case <-wc.done:
			default:
				logger.Warn("remote", "readPump: server=%s read err: %v", serverID, err)
				rc.mu.Lock()
				if rc.conns[serverID] == wc {
					delete(rc.conns, serverID)
				}
				rc.mu.Unlock()
				if rc.OnDisconnect != nil {
					rc.OnDisconnect(serverID)
				}
				// 断线自愈：非主动关闭的断链由后台退避重连恢复，无需人工「测试连接」。
				go rc.reconnectLoop(serverID, wc)
			}
			return
		}

		if resp.Cmd != "" {
			if resp.Cmd == "Progress" && len(resp.Data) > 0 {
				var p progressPush
				if err := json.Unmarshal(resp.Data, &p); err == nil && p.TaskID != "" {
					if rc.OnPushProgress != nil {
						rc.OnPushProgress(serverID, RemoteProgress{
							TaskID:    p.TaskID,
							Progress:  p.Progress,
							Stage:     p.Stage,
							SegIndex:  p.Seg,
							SegTotal:  p.SegTotal,
							OutTimeMs: p.OutTimeMs,
							TotalMs:   p.TotalMs,
							Speed:     p.Speed,
							Message:   p.Msg,
						})
					}
				}
			}
			// B-08：Hello 能力上报 → node_caps 落库 + node_status 广播（06 §5.1）。
			if resp.Cmd == "Hello" && len(resp.Data) > 0 && rc.OnPushHello != nil {
				if caps, ok := parseHelloCaps(serverID, resp.Data); ok {
					rc.OnPushHello(serverID, caps)
				} else {
					logger.Warn("remote", "Hello 载荷无有效能力信息，已忽略: server=%s", serverID)
				}
			}
			continue
		}

		select {
		case wc.respCh <- resp:
		default:
			logger.Warn("remote", "readPump: respCh full, dropping response for server=%s", serverID)
		}
	}
}

func (wc *wsConn) pingLoop(serverID string) {
	defer close(wc.pingDone)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-wc.done:
			return
		case <-ticker.C:
			wc.mu.Lock()
			wc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err := wc.conn.WriteMessage(websocket.PingMessage, nil)
			wc.conn.SetWriteDeadline(time.Time{})
			wc.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (rc *RemoteClient) CloseConn(serverID string) {
	rc.mu.Lock()
	wc, ok := rc.conns[serverID]
	delete(rc.conns, serverID)
	// 停止断线自愈：主动关闭后不再自动重连（节点删除 / 连通性测试重连由调用方决定）。
	delete(rc.serverCfg, serverID)
	rc.mu.Unlock()
	if ok && wc != nil {
		close(wc.done)
		gracefulCloseWS(wc.conn)
		wc.conn.Close()
		<-wc.pumpDone
		<-wc.pingDone
	}
}

func (rc *RemoteClient) CloseAll() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for id, wc := range rc.conns {
		if wc != nil {
			close(wc.done)
			gracefulCloseWS(wc.conn)
			wc.conn.Close()
			<-wc.pumpDone
			<-wc.pingDone
		}
		delete(rc.conns, id)
	}
	rc.serverCfg = map[string]Server{}
	rc.connMu = map[string]*sync.Mutex{}
}

// reconnectLoop 断线自愈：指数退避重连（1s→2s→4s…≤30s），直至成功、主动关闭或节点被删除。
func (rc *RemoteClient) reconnectLoop(serverID string, oldWC *wsConn) {
	delay := reconnectBaseDelay
	attempt := 0
	for {
		select {
		case <-oldWC.done:
			return // 主动关闭（CloseConn/CloseAll/替换），停止重连
		case <-time.After(delay):
		}

		rc.mu.Lock()
		cfg, ok := rc.serverCfg[serverID]
		rc.mu.Unlock()
		if !ok {
			return // 节点已删除或主动关闭
		}

		attempt++
		if _, _, err := rc.Connect(cfg); err != nil {
			logger.Warn("remote", "reconnect attempt %d failed for server=%s: %v", attempt, serverID, err)
			delay *= 2
			if delay > reconnectMaxDelay {
				delay = reconnectMaxDelay
			}
			continue
		}
		logger.Info("remote", "reconnected to server=%s after %d attempt(s)", serverID, attempt)
		return
	}
}

// gracefulCloseWS 先发送 WS Close 帧再关闭底层连接，使对端收到正常关闭而非 1006 异常断链。
func gracefulCloseWS(conn *websocket.Conn) {
	if conn == nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(wsCloseTimeout))
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(wsCloseTimeout))
	_ = conn.SetWriteDeadline(time.Time{})
}

func (wc *wsConn) roundTrip(cmd wsCmd, timeout time.Duration) (wsResp, error) {
	var resp wsResp

	// 持锁覆盖发送+接收全过程，防止并发 roundTrip 响应串台
	wc.mu.Lock()
	defer wc.mu.Unlock()

	wc.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	err := wc.conn.WriteJSON(cmd)
	wc.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		return resp, err
	}

	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	select {
	case resp = <-wc.respCh:
		return resp, nil
	case <-time.After(timeout):
		return resp, fmt.Errorf("响应超时 (%.0fs)", timeout.Seconds())
	}
}

func (rc *RemoteClient) CreateTask(server Server, sourceFileName, outputName, ffmpegArgs string) (string, error) {
	return rc.CreateTaskWithTrace(server, sourceFileName, outputName, ffmpegArgs, "")
}

// CreateTaskWithTrace 与 CreateTask 等价，额外透传任务链路追踪 ID（P2-1）。
func (rc *RemoteClient) CreateTaskWithTrace(server Server, sourceFileName, outputName, ffmpegArgs, traceID string) (string, error) {
	wc, err := rc.getConn(server)
	if err != nil {
		return "", err
	}

	cmd := wsCmd{
		Cmd:            "CreateTask",
		Key:            server.AuthKey,
		SourceFileName: sourceFileName,
		OutputName:     outputName,
		FFmpegArgs:     ffmpegArgs,
		TraceId:        traceID,
	}
	resp, err := wc.roundTrip(cmd, 60*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return "", fmt.Errorf("CreateTask 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("CreateTask 失败: %s (code=%d)", resp.Msg, resp.Code)
	}
	var td createTaskData
	if err := json.Unmarshal(resp.Data, &td); err != nil {
		return "", fmt.Errorf("解析 TaskId 失败: %w", err)
	}
	return td.TaskId, nil
}

func (rc *RemoteClient) CreateSMBTask(server Server, sourceFileName, outputName, ffmpegArgs, smbPath, smbUser, smbPassword string) (string, error) {
	return rc.CreateSMBTaskWithTrace(server, sourceFileName, outputName, ffmpegArgs, smbPath, smbUser, smbPassword, "")
}

// CreateSMBTaskWithTrace 与 CreateSMBTask 等价，额外透传任务链路追踪 ID（P2-1）。
func (rc *RemoteClient) CreateSMBTaskWithTrace(server Server, sourceFileName, outputName, ffmpegArgs, smbPath, smbUser, smbPassword, traceID string) (string, error) {
	wc, err := rc.getConn(server)
	if err != nil {
		return "", err
	}

	cmd := wsCmd{
		Cmd:            "CreateSMBTask",
		Key:            server.AuthKey,
		SourceFileName: sourceFileName,
		OutputName:     outputName,
		FFmpegArgs:     ffmpegArgs,
		SMBPath:        smbPath,
		SMBUser:        smbUser,
		SMBPassword:    smbPassword,
		TraceId:        traceID,
	}
	resp, err := wc.roundTrip(cmd, 60*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return "", fmt.Errorf("CreateSMBTask 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("CreateSMBTask 失败: %s (code=%d)", resp.Msg, resp.Code)
	}
	var td createTaskData
	if err := json.Unmarshal(resp.Data, &td); err != nil {
		return "", fmt.Errorf("解析 TaskId 失败: %w", err)
	}
	return td.TaskId, nil
}

// ===== B-06：渲染类任务下发（06 §4.2、03 §3.1/§3.2、07 §5.2/§5.5）=====

// 编译期断言：RemoteClient 即 B-05 定义的渲染下发通道（由 main.go 注入调度器）。
var _ RenderDispatcher = (*RemoteClient)(nil)

// 下发优先级线协议取值（06 §4.1：GEN_PROXY 低优、其余常规）。
const (
	renderPriorityNormal = "normal"
	renderPriorityLow    = "low"
)

// SetSettingsProvider 注入设置读取器（B-06 装配入口）。
// main.go 装配：remote.SetSettingsProvider(store.GetSettings)。
func (rc *RemoteClient) SetSettingsProvider(get func() Settings) {
	rc.getSettings = get
}

// renderShareDirs 渲染载荷中携带的共享根字段（03 §3.2 RenderTaskPayload）。
type renderShareDirs struct {
	SourceRoot string `json:"sourceRoot"`
	DestRoot   string `json:"destRoot"`
}

// CreateRenderEDL 下发 RENDER_EDL 任务（06 §4.2 / 03 §3.2）。
//
// 载荷仅携带 credentialId + 结构化 payload + 共享根，严禁携带 SMBUser/SMBPassword 明文
// （07 §5.2/§5.5）；FVCS 侧凭 credentialId 自行解析挂载凭据。
func (rc *RemoteClient) CreateRenderEDL(server Server, t Task) (string, error) {
	return rc.dispatchRender(server, t, "CreateRenderEDL", TaskTypeRenderEDL, "")
}

// CreateRenderEDLWithTrace 与 CreateRenderEDL 等价，额外透传任务链路追踪 ID（P2-1）。
func (rc *RemoteClient) CreateRenderEDLWithTrace(server Server, t Task, traceID string) (string, error) {
	return rc.dispatchRender(server, t, "CreateRenderEDL", TaskTypeRenderEDL, traceID)
}

// CreateGenProxy 下发 GEN_PROXY 代理生成任务（04 §3.5），
// 低优先级以 PriorityLevel=low 表达（06 §4.1，调度排序已保证不插队）。
func (rc *RemoteClient) CreateGenProxy(server Server, t Task) (string, error) {
	return rc.dispatchRender(server, t, "CreateGenProxy", TaskTypeGenProxy, "")
}

// CreateGenProxyWithTrace 与 CreateGenProxy 等价，额外透传任务链路追踪 ID（P2-1）。
func (rc *RemoteClient) CreateGenProxyWithTrace(server Server, t Task, traceID string) (string, error) {
	return rc.dispatchRender(server, t, "CreateGenProxy", TaskTypeGenProxy, traceID)
}

// dispatchRender 渲染类任务共用下发实现（06 §4.2）：
// 载荷校验 → 共享根解析（UNC）→ WS 下发 → 返回 FVCS 侧任务 ID。
func (rc *RemoteClient) dispatchRender(server Server, t Task, cmd string, want TaskType, traceID string) (string, error) {
	payload := json.RawMessage(strings.TrimSpace(t.PayloadJSON))
	if len(payload) == 0 {
		return "", fmt.Errorf("%s: 任务载荷为空，无法下发 (task=%s)", errCodePayloadMissing, t.ID)
	}
	if !json.Valid(payload) {
		return "", fmt.Errorf("%s: 任务载荷不是合法 JSON (task=%s)", errCodeEDLInvalid, t.ID)
	}

	smbPath, smbOutputPath, err := rc.resolveRenderSharePaths(want, payload)
	if err != nil {
		return "", err
	}

	// 任务 ID 复用：FVCS 侧直接采用本端 TaskID（03 §3.1 同一 checksum 重发须复用 TaskId），
	// 跨端日志与 B-07 进度聚合按同一 ID 对齐。
	wire := wsCmd{
		Cmd:           cmd,
		Key:           server.AuthKey,
		TaskId:        t.ID,
		TaskType:      string(want),
		PriorityLevel: renderPriorityLevel(want),
		Payload:       payload,
		SMBPath:       smbPath,
		SMBOutputPath: smbOutputPath,
		CredentialID:  t.CredentialID,
	}
	// 修复③（代理 E_RENDER_FAILED 闭环）：渲染/代理下发补齐挂载凭据。
	credSource := rc.applyRenderMountCredential(&wire)
	if credSource == "" {
		logger.Warn("remote", "%s: 无可用挂载凭据（credentialId 与 settings 账号均缺失），FVCS 侧挂载共享将失败: task=%s server=%s",
			cmd, t.ID, server.ID)
	}

	wc, err := rc.getConn(server)
	if err != nil {
		return "", err
	}
	resp, err := wc.roundTrip(wire, 60*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return "", fmt.Errorf("%s 调用失败: %w", cmd, err)
	}
	if resp.Code != 0 {
		// 透出 FVCS 的 E_* 错误码：调度侧 classifyRenderError 据此判定冷却/终止（06 §4.3）。
		return "", fmt.Errorf("%s 失败: %s (code=%d)", cmd, strings.TrimSpace(resp.Msg), resp.Code)
	}
	var td createTaskData
	if err := json.Unmarshal(resp.Data, &td); err != nil {
		return "", fmt.Errorf("%s 返回解析失败: %w", cmd, err)
	}
	if strings.TrimSpace(td.TaskId) == "" {
		return "", fmt.Errorf("%s: FVCS 未返回 TaskId (task=%s, code=%s)", cmd, t.ID, errCodeRenderFailed)
	}

	logger.Info("remote", "%s 下发成功: task=%s server=%s remote=%s priority=%s credSource=%s",
		cmd, t.ID, server.ID, td.TaskId, wire.PriorityLevel, credSource)
	return td.TaskId, nil
}

// applyRenderMountCredential 渲染/代理下发补齐 SMB 挂载凭据，返回凭据来源
// （archive=凭据档案 / plaintext=设置明文回落 / ""=无可用凭据），供审计与排障。
//
// 07 §5.2/§5.5 的优先级是凭据档案（credentialId）；但 FVCS 侧 mountTaskShare 在
// "档案与明文全空"时会以空账号执行 SMB 挂载，结果必然是 System error 1223
// （GEN_PROXY 代理失败的直接原因）。因此 credentialId 为空时，与既有转码下发
// （scheduler.go CreateSMBTask）同口径回落 settings 明文账号口令；仅在两者都非空时下发，
// 避免下发空凭据把失败伪装成"节点状态 Failed"。
func (rc *RemoteClient) applyRenderMountCredential(wire *wsCmd) string {
	if wire == nil {
		return ""
	}
	if strings.TrimSpace(wire.CredentialID) != "" {
		return "archive"
	}
	cfg := rc.settings()
	user, pass := strings.TrimSpace(cfg.SMBUser), cfg.SMBPassword
	if user == "" || strings.TrimSpace(pass) == "" {
		return ""
	}
	wire.SMBUser = user
	wire.SMBPassword = pass
	return "plaintext"
}

// renderPriorityLevel 渲染下发优先级（06 §4.1）。
func renderPriorityLevel(t TaskType) string {
	if t == TaskTypeGenProxy {
		return renderPriorityLow
	}
	return renderPriorityNormal
}

// resolveRenderSharePaths 解析下发的共享根 UNC：
//   - RENDER_EDL：载荷 sourceRoot → SMBPath（素材根）、destRoot → SMBOutputPath（输出根）；
//   - GEN_PROXY：素材共享根取载荷 sourceRoot（非缺省素材根时）或 settings.videoRoot
//     （与转码下发同源推导；videoRoot 未配置时才回退 settings.smbSharePath）；
//     输出根为素材共享根下的 _proxy 目录（04 §3.2：smbOutputPath = \\NAS\media\videos\_proxy）。
func (rc *RemoteClient) resolveRenderSharePaths(t TaskType, payload json.RawMessage) (string, string, error) {
	var dirs renderShareDirs
	_ = json.Unmarshal(payload, &dirs)

	if t == TaskTypeGenProxy {
		cfg := rc.settings()
		base := strings.TrimSpace(cfg.SMBSharePath)
		// 修复①：剪辑页在多个授权目录间切换时，代理任务会携带实际素材根（sourceRoot）。
		// 该根与缺省素材根（videoRoot）不同时，挂载该根对应的共享，
		// 否则 FVCS 会把根 B 下的相对路径错误解析到缺省共享根上。
		if sr := strings.TrimSpace(dirs.SourceRoot); sr != "" &&
			normalizeRootForCompare(sr) != normalizeRootForCompare(cfg.VideoRoot) {
			base = sr
		} else if vr := strings.TrimSpace(cfg.VideoRoot); vr != "" {
			// 修复②（代理 E_RENDER_FAILED 闭环）：GEN_PROXY 的素材共享根与转码同源。
			// 转码经 smbshare.BuildSMBURL(videoRoot) 解析出"实际承载素材的共享"
			// （例如 \\192.168.1.106\Test），而本处原先直接取已弃用的 settings.smbSharePath
			// （例如 \\192.168.1.106\T），两者指向不同共享：转码可挂载、代理必失败
			// （FVCS 侧 net use 报 System error 1223）。此处改为同样由 videoRoot 推导。
			base = vr
		}
		if base == "" {
			return "", "", fmt.Errorf("%s: GEN_PROXY 下发缺少共享根，请在设置中配置素材根 videoRoot（或 smbSharePath）（06 §4.2）", errCodeSMBMountFailed)
		}
		root, err := rc.resolveRenderRoot(base)
		if err != nil {
			return "", "", err
		}
		// 输出根：素材共享根下的 _proxy 目录（与提交侧 handlers_proxy.go、
		// 校验侧 proxyLocalForSrcRoot 的 <素材根>/_proxy 口径一致，避免代理落到共享根）。
		outputRoot := strings.TrimRight(root, "\\/") + "\\_proxy"
		return root, outputRoot, nil
	}

	src, err := rc.toSMBUNC(dirs.SourceRoot)
	if err != nil {
		return "", "", err
	}
	if src == "" {
		return "", "", fmt.Errorf("%s: 载荷缺少 sourceRoot，无法解析素材共享根 (task)", errCodePayloadMissing)
	}
	dst, err := rc.toSMBUNC(dirs.DestRoot)
	if err != nil {
		return "", "", err
	}
	if dst == "" {
		return "", "", fmt.Errorf("%s: 载荷缺少 destRoot，无法解析输出共享根", errCodePayloadMissing)
	}
	return src, dst, nil
}

// resolveRenderRoot 解析 GEN_PROXY 素材共享根为可挂载 UNC（修复②：与转码同机制）。
//
// 与转码下发（scheduler.go CreateSMBTask → smbshare.BuildSMBURL）保持同一口径：
// 本地绝对路径必须经 fnOS samba 实际共享配置映射（找不到共享即为配置错误）；
// 不再回落到"smbSharePath 前缀拼接"——那会把 /vol1/... 之类本地路径拼成
// \\NAS\share\vol1\... 这类不存在的 UNC，把"共享根分裂"掩盖成 System error 1223。
func (rc *RemoteClient) resolveRenderRoot(base string) (string, error) {
	if _, ok := normalizeUNC(base); ok {
		// 已是 UNC：按既有语义归一化透传。
		return rc.toSMBUNC(base)
	}

	cfg := rc.settings()
	if !filepath.IsAbs(filepath.FromSlash(base)) {
		// 共享相对路径（如 "videos"）：沿用 smbSharePath 前缀语义。
		return rc.toSMBUNC(base)
	}
	if strings.TrimSpace(cfg.SMBUser) == "" {
		return "", fmt.Errorf("%s: 素材根 %s 为本地路径且未配置 smbUser，无法解析对应共享（与转码同机制需 smbUser）", errCodeSMBMountFailed, base)
	}

	ip, err := smbshare.GetLocalIP()
	if err != nil {
		return "", fmt.Errorf("%s: 获取本机 IP 失败，无法解析素材根共享: %w", errCodeSMBMountFailed, err)
	}
	unc, err := smbshare.BuildSMBURL(ip, cfg.SMBUser, filepath.FromSlash(base))
	if err != nil || strings.TrimSpace(unc) == "" {
		return "", fmt.Errorf("%s: 素材根 %s 未匹配任何 samba 共享（与转码同机制解析失败）: %v", errCodeSMBMountFailed, base, err)
	}
	return unc, nil
}

// toSMBUNC 把共享根解析为 FVCS 可挂载的 UNC（03 §3.2）。解析顺序：
//  1. 已是 UNC（\\host\share 或 //host/share）→ 归一化为反斜杠形式直接透传；
//  2. settings.smbUser 已配置 → 按 fnOS samba 实际共享配置映射本地路径（与既有转码下发同机制）；
//  3. settings.smbSharePath（UNC）已配置 → 作为共享根前缀拼接共享相对路径（09 §2 语义）；
//  4. 以上均不可用 → 返回 E_SMB_MOUNT_FAILED（可重试白名单），提示补齐设置，绝不猜测映射。
func (rc *RemoteClient) toSMBUNC(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", nil
	}
	if unc, ok := normalizeUNC(root); ok {
		return unc, nil
	}

	cfg := rc.settings()

	// 2) 本地路径 → 依 fnOS samba 共享配置映射（复用既有转码下发路径的机制）
	if strings.TrimSpace(cfg.SMBUser) != "" {
		if ip, err := smbshare.GetLocalIP(); err == nil {
			if unc, err := smbshare.BuildSMBURL(ip, cfg.SMBUser, filepath.FromSlash(root)); err == nil && strings.TrimSpace(unc) != "" {
				return unc, nil
			}
		}
		logger.Debug("remote", "本地路径未匹配 samba 共享，回落到 smbSharePath 前缀: %s", root)
	}

	// 3) smbSharePath（UNC）作为共享根前缀 + 共享相对路径
	if base, ok := normalizeUNC(cfg.SMBSharePath); ok {
		rel := strings.Trim(strings.ReplaceAll(root, "\\", "/"), "/")
		if rel == "" {
			return base, nil
		}
		return base + `\` + strings.ReplaceAll(rel, "/", `\`), nil
	}

	return "", fmt.Errorf("%s: 无法解析为 SMB 共享路径（%s）：请在设置中配置 smbSharePath（共享根 UNC）或 smbUser",
		errCodeSMBMountFailed, root)
}

// settings 返回设置快照；未注入读取器时返回零值（此时仅 UNC 直传可用）。
func (rc *RemoteClient) settings() Settings {
	if rc.getSettings == nil {
		return Settings{}
	}
	return rc.getSettings()
}

// normalizeUNC 判断并归一化 UNC（\\host\share 或 //host/share）为反斜杠形式。
func normalizeUNC(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, `\\`) && !strings.HasPrefix(p, "//") {
		return "", false
	}
	// 统一分隔符 → 折叠重复/首尾分隔符 → 重组为标准 UNC
	segments := strings.Split(strings.Trim(strings.ReplaceAll(p, "/", `\`), `\`), `\`)
	kept := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg != "" {
			kept = append(kept, seg)
		}
	}
	if len(kept) == 0 {
		return "", false
	}
	return `\\` + strings.Join(kept, `\`), true
}

func (rc *RemoteClient) UploadFinish(server Server, remoteTaskID string) error {
	wc, err := rc.getConn(server)
	if err != nil {
		return err
	}
	resp, err := wc.roundTrip(wsCmd{Cmd: "UploadFinish", Key: server.AuthKey, TaskId: remoteTaskID}, 60*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return fmt.Errorf("UploadFinish 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("UploadFinish 失败: %s (code=%d)", resp.Msg, resp.Code)
	}
	return nil
}

func (rc *RemoteClient) DownloadFinish(server Server, remoteTaskID string) error {
	wc, err := rc.getConn(server)
	if err != nil {
		return err
	}
	resp, err := wc.roundTrip(wsCmd{Cmd: "DownloadFinish", Key: server.AuthKey, TaskId: remoteTaskID}, 60*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return fmt.Errorf("DownloadFinish 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("DownloadFinish 失败: %s (code=%d)", resp.Msg, resp.Code)
	}
	return nil
}

func (rc *RemoteClient) CancelTask(server Server, remoteTaskID string) error {
	wc, err := rc.getConn(server)
	if err != nil {
		return err
	}
	resp, err := wc.roundTrip(wsCmd{Cmd: "CancelTask", Key: server.AuthKey, TaskId: remoteTaskID}, 30*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return fmt.Errorf("CancelTask 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("CancelTask 失败: %s (code=%d)", resp.Msg, resp.Code)
	}
	return nil
}

func (rc *RemoteClient) PauseTask(server Server, remoteTaskID string) error {
	wc, err := rc.getConn(server)
	if err != nil {
		return err
	}
	resp, err := wc.roundTrip(wsCmd{Cmd: "PauseTask", Key: server.AuthKey, TaskId: remoteTaskID}, 30*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return fmt.Errorf("PauseTask 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("PauseTask 失败: %s (code=%d)", resp.Msg, resp.Code)
	}
	return nil
}

func (rc *RemoteClient) ResumeTask(server Server, remoteTaskID string) error {
	wc, err := rc.getConn(server)
	if err != nil {
		return err
	}
	resp, err := wc.roundTrip(wsCmd{Cmd: "ResumeTask", Key: server.AuthKey, TaskId: remoteTaskID}, 30*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return fmt.Errorf("ResumeTask 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("ResumeTask 失败: %s (code=%d)", resp.Msg, resp.Code)
	}
	return nil
}

// RemoteTaskStatus 远端任务状态快照（03 §3.3）。
// ErrorCode / ErrorMessage / Stage 用于把"失败节点"细化上报到本端任务，
// 取代原先只能拿到 Status=Failed 的泛化信息。
type RemoteTaskStatus struct {
	TaskID       string
	Status       string
	Progress     float64
	Stage        string
	ErrorCode    string
	ErrorMessage string
}

// QueryTask 查询远端任务状态（保持既有签名：调度侧仅需状态与进度）。
func (rc *RemoteClient) QueryTask(server Server, remoteTaskID string) (status string, progress float64, err error) {
	st, err := rc.QueryTaskDetail(server, remoteTaskID)
	if err != nil {
		return "", 0, err
	}
	return st.Status, st.Progress, nil
}

// QueryTaskDetail 查询远端任务详情（QueryTask 的增强版，附带失败节点信息）。
func (rc *RemoteClient) QueryTaskDetail(server Server, remoteTaskID string) (RemoteTaskStatus, error) {
	var out RemoteTaskStatus
	out.TaskID = remoteTaskID

	wc, err := rc.getConn(server)
	if err != nil {
		return out, err
	}

	resp, err := wc.roundTrip(wsCmd{Cmd: "QueryTask", Key: server.AuthKey, TaskId: remoteTaskID}, 30*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return out, fmt.Errorf("QueryTask 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return out, fmt.Errorf("QueryTask 失败: %s", resp.Msg)
	}
	var qd struct {
		TaskId       string  `json:"TaskId"`
		Status       string  `json:"Status"`
		Progress     float64 `json:"Progress"`
		Stage        string  `json:"Stage"`
		ErrorCode    string  `json:"ErrorCode"`
		ErrorMessage string  `json:"ErrorMessage"`
	}
	_ = json.Unmarshal(resp.Data, &qd)

	if id := strings.TrimSpace(qd.TaskId); id != "" {
		out.TaskID = id
	}
	out.Status = qd.Status
	out.Progress = qd.Progress
	out.Stage = strings.TrimSpace(qd.Stage)
	out.ErrorCode = strings.TrimSpace(qd.ErrorCode)
	out.ErrorMessage = strings.TrimSpace(qd.ErrorMessage)
	return out, nil
}

func (rc *RemoteClient) getConn(server Server) (*wsConn, error) {
	rc.mu.Lock()
	wc, ok := rc.conns[server.ID]
	rc.mu.Unlock()
	if !ok || wc == nil {
		if _, _, err := rc.Connect(server); err != nil {
			return nil, err
		}
		rc.mu.Lock()
		wc = rc.conns[server.ID]
		rc.mu.Unlock()
	}
	if wc == nil {
		return nil, fmt.Errorf("连接不可用")
	}
	return wc, nil
}

func (rc *RemoteClient) UploadChunk(ctx context.Context, server Server, remoteTaskID string, index int, isLast bool, data []byte) error {
	httpPort, err := rc.getHttpPort(server)
	if err != nil {
		return err
	}

	crc := crc32.ChecksumIEEE(data)
	url := fmt.Sprintf("http://%s:%d/upload?taskId=%s&index=%d&isLast=%d&crc32=%d",
		server.IP, httpPort, remoteTaskID, index, boolToInt(isLast), crc)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("X-Auth-Key", server.AuthKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("上传分片 %d 失败: %w", index, err)
	}
	defer resp.Body.Close()

	var r wsResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("解析上传响应失败: %w", err)
	}
	if r.Code != 0 {
		return fmt.Errorf("分片 %d 上传被拒: %s (code=%d)", index, r.Msg, r.Code)
	}
	return nil
}

func (rc *RemoteClient) DownloadFile(server Server, remoteTaskID string, offset int64, writer io.Writer) (int64, error) {
	return rc.DownloadFileWithProgress(server, remoteTaskID, offset, writer, nil)
}

func (rc *RemoteClient) DownloadFileWithProgress(server Server, remoteTaskID string, offset int64, writer io.Writer, onProgress func(int64, int64)) (int64, error) {
	httpPort, err := rc.getHttpPort(server)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("http://%s:%d/download?taskId=%s", server.IP, httpPort, remoteTaskID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Auth-Key", server.AuthKey)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("下载响应异常: %s", resp.Status)
	}

	contentLength := resp.ContentLength
	if contentLength < 0 {
		contentLength = 0
	}

	var totalWritten int64
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := writer.Write(buf[:n]); wErr != nil {
				return totalWritten, wErr
			}
			totalWritten += int64(n)
			if onProgress != nil && contentLength > 0 {
				onProgress(totalWritten, contentLength)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return totalWritten, err
		}
	}
	return totalWritten, nil
}

func (rc *RemoteClient) getHttpPort(server Server) (int, error) {
	wc, err := rc.getConn(server)
	if err != nil {
		return 0, err
	}
	return wc.httpPort, nil
}

func buildBinaryChunk(taskID string, index int, isLast bool, data []byte) []byte {
	tid := []byte(taskID)
	hdr := new(bytes.Buffer)
	hdr.WriteByte(0x01)
	binary.Write(hdr, binary.BigEndian, uint32(len(tid)))
	hdr.Write(tid)
	binary.Write(hdr, binary.BigEndian, uint32(index))
	hdr.WriteByte(boolToByte(isLast))
	binary.Write(hdr, binary.BigEndian, uint64(len(data)))
	dataCRC := crc32.ChecksumIEEE(data)
	binary.Write(hdr, binary.BigEndian, dataCRC)
	hdrCRCPos := hdr.Len()
	binary.Write(hdr, binary.BigEndian, uint32(0))
	headerCRC := crc32.ChecksumIEEE(hdr.Bytes())
	binary.BigEndian.PutUint32(hdr.Bytes()[hdrCRCPos:hdrCRCPos+4], headerCRC)
	hdr.Write(data)
	return hdr.Bytes()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func boolToByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}
