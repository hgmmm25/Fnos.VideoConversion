package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/task"

	"github.com/gorilla/websocket"
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
}

var serverInstance *Server

func Init(wsPort, httpPort int) error {
	serverInstance = &Server{
		httpPort:   httpPort,
		clients:    make(map[string]*ClientConnection),
		maxUploads: 100,
		uploadSem:  make(chan struct{}, 100),
	}

	cfg := config.Get()
	host := "0.0.0.0"
	if cfg.ListenLocalOnly {
		host = "127.0.0.1"
	}

	serverInstance.wsAddr = fmt.Sprintf("%s:%d", host, wsPort)
	serverInstance.httpAddr = fmt.Sprintf("%s:%d", host, httpPort)

	go startWebSocketServer()
	go startHTTPServer()

	task.SetHTTPPort(httpPort)
	task.SetTaskUpdateCallback(onTaskUpdate)
	logger.Info("server", "WebSocket server started on: %s", serverInstance.wsAddr)
	logger.Info("server", "HTTP server started on: %s", serverInstance.httpAddr)

	return nil
}

func onTaskUpdate(taskID string) {
	t := task.GetTask(taskID)
	if t == nil {
		return
	}

	progressMsg, err := protocol.BuildProgress(t.TaskID, t.Progress)
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

func startWebSocketServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWebSocket)

	serverInstance.wsServer = &http.Server{
		Addr:         serverInstance.wsAddr,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	if err := serverInstance.wsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server", "WebSocket server failed: ", err)
	}
}

func startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", handleUpload)
	mux.HandleFunc("/download", handleDownload)

	serverInstance.httpServer = &http.Server{
		Addr:           serverInstance.httpAddr,
		Handler:        loggingMiddleware(mux),
		ReadTimeout:    300 * time.Second,
		WriteTimeout:   300 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1024 * 1024,
	}

	logger.Info("server", "HTTP server starting on %s...", serverInstance.httpAddr)
	if err := serverInstance.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server", "HTTP server failed: ", err)
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("server", "Failed to upgrade WebSocket: ", err)
		return
	}

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

func (c *ClientConnection) readPump() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("server", "readPump panic: %v", r)
		}
		c.close()
	}()

	c.Conn.SetReadLimit(1024 * 1024)

	for {
		c.Conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}

		c.LastHeartbeat = time.Now()

		if err := c.handleMessage(message); err != nil {
			logger.Error("server", "Error handling message: ", err)
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
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
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
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if time.Since(c.LastHeartbeat) > 150*time.Second {
				c.close()
				return
			}
		}
	}
}

func (c *ClientConnection) pingLoop() {
	ticker := time.NewTicker(30 * time.Second)
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
	case string(protocol.CmdCreateTask):
		return c.handleCreateTask(req)
	case string(protocol.CmdCreateSMBTask):
		return c.handleCreateSMBTask(req)
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

	newTask := task.CreateSMBTask(taskID, req.SourceFileName, req.OutputName, c.ConnID, req.FFmpegArgs, req.SMBPath, req.SMBUser, req.SMBPassword)
	newTask.Priority = priority
	task.MarkUploadComplete(taskID)

	respData := protocol.CreateTaskResponseData{
		TaskId: taskID,
	}
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	logger.Info("server", "SMB Task created: %s, FFmpegArgs=%s", taskID, req.FFmpegArgs)
	return nil
}

func generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
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

	respData := protocol.GetTaskResponseData{
		TaskId:         t.TaskID,
		Status:         string(t.Status),
		SourceFileName: t.SourceFileName,
		OutputFilePath: t.OutputFilePath,
		Progress:       t.Progress,
		Resolution:     t.Resolution,
		Bitrate:        t.Bitrate,
		CreateTime:     t.CreatedAt.Format(time.RFC3339),
	}
	resp, _ := protocol.BuildSuccessResponse(respData)
	c.safeSend(resp)

	return nil
}

func (c *ClientConnection) handleGetTasks(req *protocol.WebSocketRequest) error {
	tasks := task.GetAllTasks()
	respData := make([]protocol.GetTaskResponseData, 0, len(tasks))

	for _, t := range tasks {
		respData = append(respData, protocol.GetTaskResponseData{
			TaskId:         t.TaskID,
			Status:         string(t.Status),
			SourceFileName: t.SourceFileName,
			OutputFilePath: t.OutputFilePath,
			Progress:       t.Progress,
			Resolution:     t.Resolution,
			Bitrate:        t.Bitrate,
			CreateTime:     t.CreatedAt.Format(time.RFC3339),
		})
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

	logger.Info("server", "Task stopped: ", req.TaskId)
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

	logger.Info("server", "Task paused: ", req.TaskId)
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

	logger.Info("server", "Task resumed: ", req.TaskId)
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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	expectedKey := config.DecryptAuthKey()
	if authKey != expectedKey {
		logger.Warn("server", "Auth key mismatch")
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

	authKey := r.Header.Get("X-Auth-Key")
	if authKey == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	expectedKey := config.DecryptAuthKey()
	if authKey != expectedKey {
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
