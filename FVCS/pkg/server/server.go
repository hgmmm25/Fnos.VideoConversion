package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/smb"
	"Fnos.VC_Service/pkg/task"

	"github.com/gorilla/websocket"
)

// WS 保活与超时参数（06 §5.1 / 07 §4.5）
const (
	wsReadTimeout      = 120 * time.Second // 单轮读 deadline（数据帧与 Pong 均刷新）
	wsWriteTimeout     = 10 * time.Second  // 单帧写 deadline
	wsHeartbeatTimeout = 150 * time.Second // 心跳静默上限，超过即主动断开该连接
	wsPingInterval     = 30 * time.Second  // Ping 探测周期
	wsHeartbeatTick    = 30 * time.Second  // 心跳巡检周期
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type ClientConnection struct {
	Conn          *websocket.Conn
	ClientID      string
	Authenticated bool
	LastHeartbeat time.Time
	Send          chan []byte
	ConnID        string
	closeOnce     sync.Once
	done          chan struct{}
}

type Server struct {
	wsAddr      string
	httpAddr    string
	httpPort    int
	clients     map[string]*ClientConnection
	clientMutex sync.Mutex
	wsServer    *http.Server
	httpServer  *http.Server
	uploadSem   chan struct{}
	maxUploads  int
	// httpLimiter 通用接口限流（07 §4.5，D-04）：按来源 IP 的令牌桶
	httpLimiter *ipRateLimiter
}

var serverInstance *Server

func Init(wsPort, httpPort int) error {
	cfg := config.Get()
	serverInstance = &Server{
		httpPort:    httpPort,
		clients:     make(map[string]*ClientConnection),
		maxUploads:  100,
		uploadSem:   make(chan struct{}, 100),
		httpLimiter: newIPRateLimiter(cfg.QpsLimit, cfg.QpsLimit),
	}

	// 监听地址（07 §4.4）：listen_addr 显式指定优先；否则按 listen_local_only 推导
	// （默认 0.0.0.0 以兼容内网直连；纯本机模式收敛到回环）。
	host := strings.TrimSpace(cfg.ListenAddr)
	if host == "" {
		host = "0.0.0.0"
		if cfg.ListenLocalOnly {
			host = "127.0.0.1"
		}
	}
	// WS 单节点并发上限（07 §4.5：≤3 条）
	setWSConnLimit(cfg.MaxWSConns)

	// 本机凭据档案库（07 §5.3）：初始化失败不阻断服务，仅记错误（挂载时按无档案处理）
	if err := InitCredentialAdmin(); err != nil {
		logger.Error("server", "Credential admin init failed: %v", err)
	}

	serverInstance.wsAddr = fmt.Sprintf("%s:%d", host, wsPort)
	serverInstance.httpAddr = fmt.Sprintf("%s:%d", host, httpPort)

	// WSS 数据面加密（P1-3 / SECURITY.md §4）：证书与私钥必须成对配置，半配置属错误，
	// 拒绝启动而非静默降级明文（避免部署方误以为已加密）。
	if err := validateWSConfig(cfg); err != nil {
		return err
	}

	go startWebSocketServer()
	go startHTTPServer()

	task.SetHTTPPort(httpPort)
	task.SetTaskUpdateCallback(onTaskUpdate)
	logger.Info("server", "WebSocket server started on: %s", serverInstance.wsAddr)
	logger.Info("server", "HTTP server started on: %s", serverInstance.httpAddr)
	if cfg.WSTLSCert != "" {
		logger.Info("server", "WSS enabled: ws_tls_cert=%s (data plane TLS on WS port)", cfg.WSTLSCert)
	} else {
		logger.Info("server", "WSS disabled: plain WS (inner-network isolation as fallback, see SECURITY.md §4)")
	}
	logger.Info("server", "Security: headers=%v rate_limit=%d/s ws_conns=%d",
		!cfg.DisableSecurityHeaders, cfg.QpsLimit, cfg.MaxWSConns)

	// 07 §4.4：禁止 0.0.0.0 直连公网 —— 非回环监听且未做端口收敛时给出显式告警，
	// 便于部署方（NAS / 路由器）确认未把 8080/HTTP 端口映射到公网。
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		logger.Warn("server", "Listening on %s (non-loopback): ensure ports are NOT exposed to the Internet (07 §4.4)", host)
	}

	return nil
}

func onTaskUpdate(taskID string) {
	t := task.GetTask(taskID)
	if t == nil {
		return
	}

	// 阶段化进度：RenderEDL / GenProxy 任务附带 stage / 生效方案 / 降级标记（03 §3.3）
	progressMsg, err := protocol.BuildProgressEx(
		t.TaskID, t.Progress, t.TaskType, t.Stage, t.StageIndex, t.StageTotal, t.ProfileKey, t.Degraded)
	if err != nil {
		logger.Error("server", "Failed to build progress message for task %s: %v", taskID, err)
		return
	}

	serverInstance.clientMutex.Lock()
	defer serverInstance.clientMutex.Unlock()

	targetConnID := t.ClientConnID
	sent := false
	if targetConnID != "" {
		if c, ok := serverInstance.clients[targetConnID]; ok {
			select {
			case c.Send <- progressMsg:
				sent = true
			default:
				logger.Warn("server", "Client send buffer full, skipping progress push for task %s to conn %s", taskID, targetConnID)
			}
		}
	}

	if !sent {
		for _, c := range serverInstance.clients {
			if c.Authenticated {
				select {
				case c.Send <- progressMsg:
				default:
				}
			}
		}
	}
}

// validateWSConfig 校验 WSS 配置（P1-3 / SECURITY.md §4）：
// 证书与私钥必须成对配置；空配置（明文）合法。
func validateWSConfig(cfg *config.Config) error {
	if (cfg.WSTLSCert == "") != (cfg.WSTLSKey == "") {
		return fmt.Errorf("ws_tls_cert / ws_tls_key 必须成对配置（当前仅配置其一）")
	}
	return nil
}

func startWebSocketServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWebSocket)

	serverInstance.wsServer = &http.Server{
		Addr:         serverInstance.wsAddr,
		Handler:      securityHeadersHandler(mux), // 07 §4.4
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	cfg := config.Get()
	if cfg.WSTLSCert != "" && cfg.WSTLSKey != "" {
		// WSS（P1-3 / SECURITY.md §4）：自签或内网 CA 证书，握手失败在客户端按 TLS 错误单独计数
		if err := serverInstance.wsServer.ListenAndServeTLS(cfg.WSTLSCert, cfg.WSTLSKey); err != nil && err != http.ErrServerClosed {
			logger.Error("server", "WebSocket TLS server failed: %v", err)
		}
		return
	}

	if err := serverInstance.wsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server", "WebSocket server failed: %v", err)
	}
}

func startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", handleUpload)
	mux.HandleFunc("/download", handleDownload)
	// 本机凭据管理接口（07 §5.4）：handler 内强制环回来源校验
	mux.HandleFunc("/local/credentials", handleLocalCredentials)

	// 安全头 + 通用接口限流（07 §4.4 / §4.5）：限流在最外层，超限请求不进入业务处理
	var handler http.Handler = mux
	if serverInstance != nil {
		handler = rateLimitMiddleware(serverInstance.httpLimiter)(handler)
	}
	handler = securityHeadersHandler(handler)

	serverInstance.httpServer = &http.Server{
		Addr:           serverInstance.httpAddr,
		Handler:        loggingMiddleware(handler),
		ReadTimeout:    300 * time.Second,
		WriteTimeout:   300 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1024 * 1024,
	}

	logger.Info("server", "HTTP server starting on %s...", serverInstance.httpAddr)
	if err := serverInstance.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server", "HTTP server failed: %v", err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("server", "HTTP request panic: %v", r)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 鉴权失败冷却检查（07 §4.5）
	if authFailGate(w, r) {
		return
	}

	// WS 单节点并发上限（07 §4.5：≤3 条）：超限在升级前拒绝，返回 429
	if !nodeWSConns.acquire() {
		logger.Warn("server", "WS connection rejected (limit %d reached) from %s", nodeWSConns.maxCount(), clientIP(r))
		w.Header().Set("Retry-After", "30")
		writeJSONError(w, http.StatusTooManyRequests, errCodeTooManyConns, "节点 WebSocket 连接数已达上限，请稍后重试")
		return
	}
	acquired := true
	defer func() {
		if acquired {
			nodeWSConns.release()
		}
	}()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("server", "Failed to upgrade WebSocket: %v", err)
		return
	}
	acquired = false // 名额转交 ClientConnection.close() 释放（与连接生命周期 1:1）

	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		clientID = generateClientID()
	}

	client := &ClientConnection{
		Conn:          conn,
		ClientID:      clientID,
		Authenticated: false,
		LastHeartbeat: time.Now(),
		Send:          make(chan []byte, 100),
		ConnID:        generateClientID(),
		done:          make(chan struct{}),
	}

	serverInstance.clientMutex.Lock()
	serverInstance.clients[client.ConnID] = client
	serverInstance.clientMutex.Unlock()

	go client.readPump()
	go client.writePump()
	go client.heartbeatMonitor()
	go client.pingLoop()
}

func generateClientID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// remoteIP 取该 WS 连接的来源 IP（剥端口，07 §4.5 鉴权失败按 IP 计数）。
func (c *ClientConnection) remoteIP() string {
	if c == nil || c.Conn == nil || c.Conn.RemoteAddr() == nil {
		return ""
	}
	addr := c.Conn.RemoteAddr().String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func (c *ClientConnection) readPump() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("server", "readPump panic: %v", r)
		}
		c.close()
	}()

	c.Conn.SetReadLimit(1024 * 1024)

	// 保活补强（06 §5.1）：Ping/Pong 属控制帧，不会从 ReadMessage 返回，
	// 若只依赖数据帧刷新，则对端仅发 Ping 而不发数据帧时读 deadline 与心跳时间都不推进，
	// 连接会在读超时/心跳静默后被静默断开。此处令 Pong 同样刷新两者。
	c.Conn.SetPongHandler(func(string) error {
		c.LastHeartbeat = time.Now()
		if err := c.Conn.SetReadDeadline(time.Now().Add(wsReadTimeout)); err != nil {
			logger.Warn("server", "pong: refresh read deadline failed for client: %s, err=%v", c.ClientID, err)
		}
		return nil
	})

	for {
		c.Conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			// 读错误/读超时补 WARN（06 §5.1）：排障需可区分「静默超时断链」与「对端异常断开」
			var netErr net.Error
			switch {
			case errors.As(err, &netErr) && netErr.Timeout():
				logger.Warn("server", "readPump: read timeout after %v, closing client: %s", wsReadTimeout, c.ClientID)
			case websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway):
				logger.Info("server", "readPump: client closed: %s", c.ClientID)
			default:
				logger.Warn("server", "readPump: read error for client: %s, err=%v", c.ClientID, err)
			}
			break
		}

		c.LastHeartbeat = time.Now()

		if err := c.handleMessage(message); err != nil {
			logger.Error("server", "Error handling message: %v", err)
		}
	}
}

func (c *ClientConnection) writePump() {
	logger.Info("server", "writePump started for client: %s", c.ClientID)
	defer func() {
		if r := recover(); r != nil {
			logger.Error("server", "writePump panic: %v", r)
		}
		logger.Info("server", "writePump exiting for client: %s", c.ClientID)
		c.close()
	}()

	for {
		select {
		case <-c.done:
			return
		case message := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			var err error
			if len(message) == 0 {
				err = c.Conn.WriteMessage(websocket.PingMessage, nil)
			} else {
				err = c.Conn.WriteMessage(websocket.TextMessage, message)
			}
			if err != nil {
				logger.Error("server", "writePump WriteMessage failed for client: %s, err=%v", c.ClientID, err)
				return
			}
		}
	}
}

func (c *ClientConnection) heartbeatMonitor() {
	ticker := time.NewTicker(wsHeartbeatTick)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if idle := time.Since(c.LastHeartbeat); idle > wsHeartbeatTimeout {
				// 心跳超时补 WARN（06 §5.1）：用于区分「静默无心跳」与「对端主动关闭」
				logger.Warn("server", "heartbeatMonitor: client %s idle for %v (>%v), closing", c.ClientID, idle, wsHeartbeatTimeout)
				c.close()
				return
			}
		}
	}
}

func (c *ClientConnection) pingLoop() {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			select {
			case <-c.done:
				return
			case c.Send <- []byte{}:
			default:
				// Send channel满时跳过本次ping，不要关闭连接
				// 在转码过程中进度推送频繁，channel可能暂时满
				logger.Warn("server", "pingLoop: Send channel full for client %s, skipping ping", c.ClientID)
			}
		}
	}
}

func (c *ClientConnection) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.Conn.Close()

		serverInstance.clientMutex.Lock()
		delete(serverInstance.clients, c.ConnID)
		serverInstance.clientMutex.Unlock()

		nodeWSConns.release() // 归还 WS 并发名额（07 §4.5）

		if c.Authenticated {
			task.HandleClientDisconnect(c.ConnID)
		}
	})
}

func (c *ClientConnection) safeSend(data []byte) {
	select {
	case <-c.done:
		return
	case c.Send <- data:
	default:
		logger.Warn("server", "Send channel full, dropping message for client: %s", c.ClientID)
	}
}

func (c *ClientConnection) handleMessage(message []byte) error {
	req, err := protocol.ParseWebSocketRequest(message)
	if err != nil {
		resp, _ := protocol.BuildErrorResponse("Invalid request")
		c.safeSend(resp)
		return err
	}

	if !c.Authenticated && req.Cmd != string(protocol.CmdAuth) {
		resp, _ := protocol.BuildErrorResponse("Unauthorized")
		c.safeSend(resp)
		return nil
	}

	switch req.Cmd {
	case string(protocol.CmdAuth):
		return c.handleAuth(req)
	case string(protocol.CmdHello):
		return c.handleHello(req)
	case string(protocol.CmdCreateTask):
		return c.handleCreateTask(req)
	case string(protocol.CmdCreateSMBTask):
		return c.handleCreateSMBTask(req)
	case string(protocol.CmdCreateRenderEDL):
		return c.handleCreateRenderEDL(req)
	case string(protocol.CmdCreateGenProxy):
		return c.handleCreateGenProxy(req)
	case string(protocol.CmdGetTask):
		return c.handleGetTask(req)
	case string(protocol.CmdQueryTask):
		return c.handleQueryTask(req)
	case string(protocol.CmdGetTasks):
		return c.handleGetTasks(req)
	case string(protocol.CmdStopTask):
		return c.handleStopTask(req)
	case string(protocol.CmdCancelTask):
		return c.handleCancelTask(req)
	case string(protocol.CmdPauseTask):
		return c.handlePauseTask(req)
	case string(protocol.CmdResumeTask):
		return c.handleResumeTask(req)
	case string(protocol.CmdUploadFinish):
		return c.handleUploadFinish(req)
	case string(protocol.CmdDownloadFinish):
		return c.handleDownloadFinish(req)
	case string(protocol.CmdGetHttpPort):
		return c.handleGetHttpPort(req)
	case string(protocol.CmdGetSystemStatus):
		return c.handleGetSystemStatus(req)
	case string(protocol.CmdHeartbeat):
		return c.handleHeartbeat(req)
	default:
		resp, _ := protocol.BuildErrorResponse("Unknown command: " + req.Cmd)
		c.safeSend(resp)
	}

	return nil
}

func (c *ClientConnection) handleAuth(req *protocol.WebSocketRequest) error {
	logger.Info("server", "Received Auth request from client: %s", c.ClientID)

	expectedKey := config.DecryptAuthKey()

	if req.Key != expectedKey {
		logger.Error("server", "Auth failed: key mismatch")
		resp, _ := protocol.BuildErrorResponse("Invalid auth key")
		c.safeSend(resp)
		// 鉴权失败计数（07 §4.5）：达阈值进入冷却，并断开该连接避免继续试探
		if authFailNote(c.remoteIP(), "ws") {
			c.close()
		}
		return fmt.Errorf("invalid auth key")
	}

	c.Authenticated = true
	c.LastHeartbeat = time.Now()

	cfg := config.Get()
	chunkSize := int(cfg.ChunkSize / (1024 * 1024))
	if chunkSize == 0 {
		chunkSize = 10
	}

	respData := protocol.GetHttpPortResponseData{
		HttpPort:  serverInstance.httpPort,
		ChunkSize: chunkSize,
	}
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	logger.Info("server", "Client authenticated: %s, sending HTTP port: %d", c.ClientID, serverInstance.httpPort)

	// A-07 / 06 §5.1：认证成功后由节点主动推送能力上报（Hello），
	// 使调度端无需再发 Hello 请求即可完成 node_caps 落库与 node_status 广播。
	// 必须排在认证响应之后，避免被调度端握手期的读响应逻辑消费掉。
	c.pushHello()

	return nil
}

func (c *ClientConnection) handleCreateTask(req *protocol.WebSocketRequest) error {
	logger.Info("server", "Received CreateTask request from client: %s", c.ClientID)
	logger.Info("server", "CreateTask params: SourceFileName=%s, OutputName=%s, FFmpegArgs=%s, Priority=%d",
		req.SourceFileName, req.OutputName, req.FFmpegArgs, req.Priority)

	if req.SourceFileName == "" {
		resp, _ := protocol.BuildErrorResponse("SourceFileName is required")
		c.safeSend(resp)
		return fmt.Errorf("SourceFileName is required")
	}

	if !protocol.ValidateExtraFFmpegArgs(req.FFmpegArgs) {
		logger.Error("server", "FFmpeg args validation failed: %s", req.FFmpegArgs)
		resp, _ := protocol.BuildErrorResponse("Invalid FFmpeg args")
		c.safeSend(resp)
		return fmt.Errorf("invalid FFmpeg args")
	}

	taskID := generateTaskID()
	priority := task.PriorityNormal
	if req.Priority > 0 {
		priority = task.PriorityUrgent
	}

	newTask := task.CreateTask(taskID, req.SourceFileName, req.OutputName, c.ConnID, req.FFmpegArgs)
	newTask.Priority = priority

	respData := protocol.CreateTaskResponseData{
		TaskId: taskID,
	}
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	logger.Info("server", "Task created: %s, FFmpegArgs=%s", taskID, req.FFmpegArgs)
	auditSecurity(c.remoteIP(), actionTaskSubmit, taskID, "ok", "type=transcode")
	return nil
}

func (c *ClientConnection) handleCreateSMBTask(req *protocol.WebSocketRequest) error {
	logger.Info("server", "Received CreateSMBTask request from client: %s", c.ClientID)
	logger.Info("server", "CreateSMBTask params: SourceFileName=%s, OutputName=%s, FFmpegArgs=%s, Priority=%d, SMBPath=%s",
		req.SourceFileName, req.OutputName, req.FFmpegArgs, req.Priority, req.SMBPath)

	if req.SourceFileName == "" {
		resp, _ := protocol.BuildErrorResponse("SourceFileName is required")
		c.safeSend(resp)
		return fmt.Errorf("SourceFileName is required")
	}

	if req.SMBPath == "" {
		resp, _ := protocol.BuildErrorResponse("SMBPath is required")
		c.safeSend(resp)
		return fmt.Errorf("SMBPath is required")
	}

	if !protocol.ValidateExtraFFmpegArgs(req.FFmpegArgs) {
		logger.Error("server", "FFmpeg args validation failed: %s", req.FFmpegArgs)
		resp, _ := protocol.BuildErrorResponse("Invalid FFmpeg args")
		c.safeSend(resp)
		return fmt.Errorf("invalid FFmpeg args")
	}

	taskID := generateTaskID()
	priority := task.PriorityNormal
	if req.Priority > 0 {
		priority = task.PriorityUrgent
	}

	var newTask *task.Task
	if credID := strings.TrimSpace(req.CredentialID); credID != "" {
		// 07 §5.3 前置拒绝：目标越出 Scope 时不建任务
		if err := precheckCredentialTarget(credID, req.SMBPath); err != nil {
			raw, code := buildPayloadErrorResponse(err)
			logger.ErrorT("server", req.TraceId, "CreateSMBTask rejected: taskID=%s, code=%s", taskID, code)
			c.safeSend(raw)
			return nil
		}
		// 07 §5.3 M1 支路：只下发 credentialId，明文口令不落库
		logger.InfoT("server", req.TraceId, "CreateSMBTask via credential archive: taskID=%s, credentialId=%s", taskID, credID)
		newTask = task.CreateSMBTaskExWithTrace(taskID, req.SourceFileName, req.OutputName, c.ConnID, req.FFmpegArgs, req.SMBPath, "", "", credID, req.TraceId)
	} else {
		newTask = task.CreateSMBTaskExWithTrace(taskID, req.SourceFileName, req.OutputName, c.ConnID, req.FFmpegArgs, req.SMBPath, req.SMBUser, req.SMBPassword, "", req.TraceId)
	}
	newTask.Priority = priority
	task.MarkUploadComplete(taskID)

	respData := protocol.CreateTaskResponseData{
		TaskId: taskID,
	}
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	logger.InfoT("server", req.TraceId, "SMB Task created: %s, FFmpegArgs=%s", taskID, req.FFmpegArgs)
	auditSecurity(c.remoteIP(), actionTaskSubmit, taskID, "ok", "type=smb_transcode")
	return nil
}

func generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}

// ============================================================
// RenderEDL / GenProxy 创建（03 §3.1、04 §3.2）
// ============================================================

// requestTaskID 采纳客户端幂等 TaskId（03 §3.1：同一 checksum 重发须复用TaskId），为空则新生成
func requestTaskID(req *protocol.WebSocketRequest) string {
	if id := strings.TrimSpace(req.TaskId); id != "" {
		return id
	}
	return generateTaskID()
}

// buildPayloadErrorResponse 将 E_* 错误码透出给前端（03 §5.3）
func buildPayloadErrorResponse(err error) ([]byte, string) {
	msg := err.Error()
	code := "ERROR"
	if idx := strings.Index(msg, ":"); idx > 0 {
		code = strings.TrimSpace(msg[:idx])
	}
	buf, _ := protocol.BuildResponse(500, msg, map[string]string{"code": code, "msg": msg})
	return buf, code
}

// precheckCredentialTarget 任务创建阶段的前置拒绝（07 §5.3）：带 credentialId 时先校验共享根与
// 绝对 UNC 目标是否与档案一致/在 Scope 内，越界直接以 E_CREDENTIAL_SCOPE_DENIED 拒单，
// 避免建出必然失败的挂载任务。相对路径（由执行期再做逐路径校验）不在此处拦截。
func precheckCredentialTarget(credentialID, shareBase string, extra ...string) error {
	credID := strings.TrimSpace(credentialID)
	if credID == "" {
		return nil
	}
	store := smb.DefaultCredStore()
	if store == nil {
		logger.Error("server", "Credential precheck failed: store unavailable, credentialId=%s", credID)
		return smb.ErrCredentialStoreUnavailable
	}
	return store.VerifyTaskTarget(credID, shareBase, extra...)
}

func (c *ClientConnection) handleCreateRenderEDL(req *protocol.WebSocketRequest) error {
	logger.Info("server", "Received CreateRenderEDL request from client: %s, SMBPath=%s, payloadLen=%d",
		c.ClientID, req.SMBPath, len(req.Payload))

	taskID := requestTaskID(req)
	// 字段冲突防护（03 §5.2）：结构化任务不得携带既有直通 ffmpeg 参数
	if req.HasLegacyArgs() {
		raw, _ := buildPayloadErrorResponse(fmt.Errorf("%s: FFmpegArgs 与结构化载荷互斥", protocol.ErrCodeProtoFieldConflict))
		logger.Error("server", "CreateRenderEDL rejected: TaskId=%s, FFmpegArgs 与 payload 冲突", taskID)
		auditReject(c.remoteIP(), taskID, protocol.ErrCodeProtoFieldConflict, "CreateRenderEDL")
		c.safeSend(raw)
		return nil
	}
	// 07 §5.3 前置拒绝：素材共享 / 成品目录越出 Scope 时不建任务
	if err := precheckCredentialTarget(req.CredentialID, req.SMBPath, req.SMBOutputPath); err != nil {
		raw, code := buildPayloadErrorResponse(err)
		logger.Error("server", "CreateRenderEDL rejected: taskID=%s, code=%s", taskID, code)
		auditReject(c.remoteIP(), taskID, code, "CreateRenderEDL")
		c.safeSend(raw)
		return nil
	}
	created, err := task.CreateRenderEDLTask(task.RenderEDLRequest{
		TaskID:        taskID,
		Payload:       req.Payload,
		ProfileKey:    "",
		ClientConnID:  c.ConnID,
		SMBPath:       req.SMBPath,
		SMBUser:       req.SMBUser,
		SMBPassword:   req.SMBPassword,
		CredentialID:  req.CredentialID,
		SMBOutputPath: req.SMBOutputPath,
		TraceID:       req.TraceId,
	})
	if err != nil {
		raw, code := buildPayloadErrorResponse(err)
		logger.ErrorT("server", req.TraceId, "CreateRenderEDL rejected: taskID=%s, code=%s, err=%v", taskID, code, err)
		auditReject(c.remoteIP(), taskID, code, "CreateRenderEDL")
		c.safeSend(raw)
		return nil
	}

	// 立即尝试调度（SMB 直读无需上传，建即入队）
	task.MarkUploadComplete(taskID)

	respData := protocol.CreateRenderEDLResponseData{
		TaskId:             created.Task.TaskID,
		TaskType:           protocol.TaskTypeRenderEDL,
		Status:             string(created.Task.Status),
		ProjectID:          created.ProjectID,
		ProjectRev:         created.ProjectRev,
		Checksum:           created.Checksum,
		FastCopyAllowed:    created.FastCopyAllowed,
		EffectivePresetKey: created.Task.ProfileKey,
		Warnings:           created.Warnings,
	}
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	logger.InfoT("server", req.TraceId, "RenderEDL task created: %s, fastCopy=%v, preset=%s",
		taskID, created.FastCopyAllowed, created.Task.ProfileKey)
	auditSecurity(c.remoteIP(), actionRenderSubmit, taskID, "ok",
		fmt.Sprintf("project=%s rev=%d fastCopy=%v", created.ProjectID, created.ProjectRev, created.FastCopyAllowed))
	return nil
}

func (c *ClientConnection) handleCreateGenProxy(req *protocol.WebSocketRequest) error {
	logger.Info("server", "Received CreateGenProxy request from client: %s, SMBPath=%s, payloadLen=%d",
		c.ClientID, req.SMBPath, len(req.Payload))

	taskID := requestTaskID(req)
	if req.HasLegacyArgs() {
		raw, _ := buildPayloadErrorResponse(fmt.Errorf("%s: FFmpegArgs 与结构化载荷互斥", protocol.ErrCodeProtoFieldConflict))
		logger.Error("server", "CreateGenProxy rejected: TaskId=%s, FFmpegArgs 与 payload 冲突", taskID)
		auditReject(c.remoteIP(), taskID, protocol.ErrCodeProtoFieldConflict, "CreateGenProxy")
		c.safeSend(raw)
		return nil
	}
	// 07 §5.3 前置拒绝：素材共享 / 代理输出目录越出 Scope 时不建任务
	if err := precheckCredentialTarget(req.CredentialID, req.SMBPath, req.SMBOutputPath); err != nil {
		raw, code := buildPayloadErrorResponse(err)
		logger.Error("server", "CreateGenProxy rejected: taskID=%s, code=%s", taskID, code)
		auditReject(c.remoteIP(), taskID, code, "CreateGenProxy")
		c.safeSend(raw)
		return nil
	}
	created, err := task.CreateGenProxyTask(task.GenProxyRequest{
		TaskID:        taskID,
		Payload:       req.Payload,
		ClientConnID:  c.ConnID,
		SMBPath:       req.SMBPath,
		SMBUser:       req.SMBUser,
		SMBPassword:   req.SMBPassword,
		CredentialID:  req.CredentialID,
		SMBOutputPath: req.SMBOutputPath,
	})
	if err != nil {
		raw, code := buildPayloadErrorResponse(err)
		logger.Error("server", "CreateGenProxy rejected: taskID=%s, code=%s, err=%v", taskID, code, err)
		auditReject(c.remoteIP(), taskID, code, "CreateGenProxy")
		c.safeSend(raw)
		return nil
	}

	task.MarkUploadComplete(taskID)

	respData := map[string]interface{}{
		"TaskId":    created.Task.TaskID,
		"TaskType":  protocol.TaskTypeGenProxy,
		"Status":    string(created.Task.Status),
		"ProxyFile": created.ProxyFile,
		"PresetKey": created.Template,
	}
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	logger.InfoT("server", req.TraceId, "GenProxy task created: %s, proxyFile=%s", taskID, created.ProxyFile)
	auditSecurity(c.remoteIP(), actionProxySubmit, taskID, "ok", fmt.Sprintf("preset=%s", created.Template))
	return nil
}

// buildTaskResp 组装任务详情（含阶段化字段，03 §3.3）
func buildTaskResp(t *task.Task) protocol.GetTaskResponseData {
	return protocol.GetTaskResponseData{
		TaskId:         t.TaskID,
		Status:         string(t.Status),
		SourceFileName: t.SourceFileName,
		OutputFilePath: t.OutputFilePath,
		Progress:       t.Progress,
		Resolution:     t.Resolution,
		Bitrate:        t.Bitrate,
		CreateTime:     t.CreatedAt.Format(time.RFC3339),
		TaskType:       t.TaskType,
		Stage:          t.Stage,
		StageIndex:     t.StageIndex,
		StageTotal:     t.StageTotal,
		ProfileKey:     t.ProfileKey,
		Degraded:       t.Degraded,
		Warnings:       t.Warnings,
		// 失败节点细化上报：Status=Failed 时携带具体错误码与原因（03 §3.3）。
		ErrorCode:    t.ErrorCode,
		ErrorMessage: t.ErrorMessage,
	}
}

func (c *ClientConnection) handleGetTask(req *protocol.WebSocketRequest) error {
	return c.handleQueryTask(req)
}

func (c *ClientConnection) handleQueryTask(req *protocol.WebSocketRequest) error {
	if req.TaskId == "" {
		resp, _ := protocol.BuildErrorResponse("TaskId is required")
		c.safeSend(resp)
		return fmt.Errorf("TaskId is required")
	}

	t := task.GetTask(req.TaskId)
	if t == nil {
		logger.Warn("server", "handleQueryTask: task not found, taskID=%s", req.TaskId)
		resp, _ := protocol.BuildErrorResponse("Task not found")
		c.safeSend(resp)
		return nil
	}

	respData := buildTaskResp(t)
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	return nil
}

func (c *ClientConnection) handleGetTasks(req *protocol.WebSocketRequest) error {
	tasks := task.GetAllTasks()
	respData := make([]protocol.GetTaskResponseData, 0, len(tasks))

	for _, t := range tasks {
		respData = append(respData, buildTaskResp(t))
	}

	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	return nil
}

func (c *ClientConnection) handleStopTask(req *protocol.WebSocketRequest) error {
	return c.handleCancelTask(req)
}

func (c *ClientConnection) handleCancelTask(req *protocol.WebSocketRequest) error {
	if req.TaskId == "" {
		resp, _ := protocol.BuildErrorResponse("TaskId is required")
		c.safeSend(resp)
		return fmt.Errorf("TaskId is required")
	}

	task.CancelTask(req.TaskId)

	resp, _ := protocol.BuildSuccessResponse(nil)
	c.safeSend(resp)

	logger.Info("server", "Task stopped: %v", req.TaskId)
	return nil
}

func (c *ClientConnection) handlePauseTask(req *protocol.WebSocketRequest) error {
	if req.TaskId == "" {
		resp, _ := protocol.BuildErrorResponse("TaskId is required")
		c.safeSend(resp)
		return fmt.Errorf("TaskId is required")
	}

	task.PauseTask(req.TaskId)

	resp, _ := protocol.BuildSuccessResponse(nil)
	c.safeSend(resp)

	logger.Info("server", "Task paused: %v", req.TaskId)
	return nil
}

func (c *ClientConnection) handleResumeTask(req *protocol.WebSocketRequest) error {
	if req.TaskId == "" {
		resp, _ := protocol.BuildErrorResponse("TaskId is required")
		c.safeSend(resp)
		return fmt.Errorf("TaskId is required")
	}

	task.ResumeTask(req.TaskId)

	resp, _ := protocol.BuildSuccessResponse(nil)
	c.safeSend(resp)

	logger.Info("server", "Task resumed: %v", req.TaskId)
	return nil
}

func (c *ClientConnection) handleUploadFinish(req *protocol.WebSocketRequest) error {
	if req.TaskId == "" {
		resp, _ := protocol.BuildErrorResponse("TaskId is required")
		c.safeSend(resp)
		return fmt.Errorf("TaskId is required")
	}

	logger.Info("server", "Received UploadFinish request: taskID=%s", req.TaskId)
	task.MarkUploadComplete(req.TaskId)

	resp, _ := protocol.BuildSuccessResponse(nil)
	c.safeSend(resp)

	return nil
}

func (c *ClientConnection) handleDownloadFinish(req *protocol.WebSocketRequest) error {
	if req.TaskId == "" {
		resp, _ := protocol.BuildErrorResponse("TaskId is required")
		c.safeSend(resp)
		return fmt.Errorf("TaskId is required")
	}

	t := task.GetTask(req.TaskId)
	if t != nil {
		task.UpdateTask(req.TaskId, map[string]interface{}{
			"status": task.StatusSuccess,
		})

		go func(taskID string) {
			task.CleanupTaskFiles(taskID)
			logger.Info("server", "Cleaned up temp files for task: %s", taskID)
		}(req.TaskId)
	}

	resp, _ := protocol.BuildSuccessResponse(nil)
	c.safeSend(resp)

	return nil
}

func (c *ClientConnection) handleGetHttpPort(req *protocol.WebSocketRequest) error {
	cfg := config.Get()
	chunkSize := int(cfg.ChunkSize / (1024 * 1024))
	if chunkSize == 0 {
		chunkSize = 10
	}

	respData := protocol.GetHttpPortResponseData{
		HttpPort:  serverInstance.httpPort,
		ChunkSize: chunkSize,
	}
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	return nil
}

func (c *ClientConnection) handleGetSystemStatus(req *protocol.WebSocketRequest) error {
	cfg := config.Get()

	respData := protocol.SystemStatusData{
		WsPort:        cfg.WsPort,
		HttpPort:      serverInstance.httpPort,
		RunningTasks:  task.GetRunningCount(),
		WaitingTasks:  task.GetWaitingCount(),
		OnlineClients: len(serverInstance.clients),
	}

	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	return nil
}

func (c *ClientConnection) handleHeartbeat(req *protocol.WebSocketRequest) error {
	c.LastHeartbeat = time.Now()
	resp, _ := protocol.BuildSuccessResponse(nil)
	c.safeSend(resp)
	return nil
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("server", "handleUpload panic: %v", r)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}()

	// 鉴权失败冷却检查（07 §4.5）
	if authFailGate(w, r) {
		return
	}

	select {
	case serverInstance.uploadSem <- struct{}{}:
	default:
		logger.Warn("server", "Upload rejected: too many concurrent uploads")
		http.Error(w, "Too many concurrent uploads", http.StatusTooManyRequests)
		return
	}
	defer func() { <-serverInstance.uploadSem }()

	if r.Method != http.MethodPost {
		logger.Warn("server", "Method not allowed: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authKey := r.Header.Get("X-Auth-Key")
	if authKey == "" {
		logger.Warn("server", "Missing auth key")
		authFailNote(clientIP(r), "/upload")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	expectedKey := config.DecryptAuthKey()
	if authKey != expectedKey {
		logger.Warn("server", "Auth key mismatch")
		authFailNote(clientIP(r), "/upload")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	taskID := r.FormValue("taskId")
	chunkIndex := r.FormValue("index")
	isLastChunk := r.FormValue("isLast")

	if taskID == "" || chunkIndex == "" {
		logger.Warn("server", "Missing parameters: taskID=%s, index=%s", taskID, chunkIndex)
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}

	index, err := strconv.Atoi(chunkIndex)
	if err != nil {
		logger.Warn("server", "Invalid chunk index: %s, err=%v", chunkIndex, err)
		http.Error(w, "Invalid chunk index", http.StatusBadRequest)
		return
	}

	contentLength := r.ContentLength

	if contentLength > 500*1024*1024 {
		logger.Warn("server", "Chunk too large: %d bytes", contentLength)
		http.Error(w, "Chunk too large", http.StatusRequestEntityTooLarge)
		return
	}

	task.MarkUploading(taskID)
	err = task.StoreChunk(taskID, index, r.Body)
	if err != nil {
		logger.Error("server", "Failed to store chunk: taskID=%s, index=%d, err=%v",
			taskID, index, err)
		http.Error(w, "Failed to store chunk", http.StatusInternalServerError)
		return
	}

	if isLastChunk == "1" || isLastChunk == "true" {
		task.MarkUploadComplete(taskID)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"Code":0,"Msg":"success"}`))
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 鉴权失败冷却检查（07 §4.5）
	if authFailGate(w, r) {
		return
	}

	authKey := r.Header.Get("X-Auth-Key")
	if authKey == "" {
		authFailNote(clientIP(r), "/download")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	expectedKey := config.DecryptAuthKey()
	if authKey != expectedKey {
		authFailNote(clientIP(r), "/download")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	taskID := r.URL.Query().Get("taskId")
	if taskID == "" {
		http.Error(w, "Missing taskId", http.StatusBadRequest)
		return
	}

	t := task.GetTask(taskID)
	if t == nil || t.Status != task.StatusSuccess {
		http.Error(w, "Task not found or not completed", http.StatusNotFound)
		return
	}

	if t.OutputFilePath == "" {
		http.Error(w, "Output file not available", http.StatusNotFound)
		return
	}

	file, err := os.Open(t.OutputFilePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to stat file", http.StatusInternalServerError)
		return
	}

	cfg := config.Get()
	if cfg.EnableRangeDownload {
		handleRangeDownload(w, r, file, fileInfo)
	} else {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", strconv.FormatInt(fileInfo.Size(), 10))
		http.ServeFile(w, r, t.OutputFilePath)
	}
}

func handleRangeDownload(w http.ResponseWriter, r *http.Request, file *os.File, fileInfo os.FileInfo) {
	fileSize := fileInfo.Size()
	rangeHeader := r.Header.Get("Range")

	if rangeHeader == "" {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
		w.WriteHeader(http.StatusOK)
		io.Copy(w, file)
		return
	}

	var start, end int64
	fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)

	if end == 0 || end > fileSize-1 {
		end = fileSize - 1
	}

	contentLength := end - start + 1

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.WriteHeader(http.StatusPartialContent)

	file.Seek(start, io.SeekStart)
	buffer := make([]byte, 32*1024)
	for contentLength > 0 {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			break
		}
		if n > int(contentLength) {
			n = int(contentLength)
		}
		w.Write(buffer[:n])
		contentLength -= int64(n)
	}
}

func GetOnlineClientCount() int {
	serverInstance.clientMutex.Lock()
	defer serverInstance.clientMutex.Unlock()
	return len(serverInstance.clients)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Stop() {
	if serverInstance.wsServer != nil {
		serverInstance.wsServer.Close()
	}
	if serverInstance.httpServer != nil {
		serverInstance.httpServer.Close()
	}

	serverInstance.clientMutex.Lock()
	for _, client := range serverInstance.clients {
		client.close()
	}
	serverInstance.clients = make(map[string]*ClientConnection)
	serverInstance.clientMutex.Unlock()
}

func IsPortInUse(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	listener.Close()
	return false
}
