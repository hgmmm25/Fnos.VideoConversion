package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/server"
	"Fnos.VC_Service/pkg/task"
	"Fnos.VC_Service/pkg/winapi"
)

const ipcListenAddr = "127.0.0.1:5000"

type IPCCommand string

const (
	CmdStartService    IPCCommand = "StartService"
	CmdStopService     IPCCommand = "StopService"
	CmdGetConfig       IPCCommand = "GetConfig"
	CmdSetConfig       IPCCommand = "SetConfig"
	CmdGetTasks        IPCCommand = "GetTasks"
	CmdStopTask        IPCCommand = "StopTask"
	CmdClearWaiting    IPCCommand = "ClearWaiting"
	CmdClearAll        IPCCommand = "ClearAll"
	CmdGetLogs         IPCCommand = "GetLogs"
	CmdGetSystemStatus IPCCommand = "GetSystemStatus"
	CmdGetHttpPort     IPCCommand = "GetHttpPort"
	CmdGetAuthKey      IPCCommand = "GetAuthKey"
)

type IPCRequest struct {
	Cmd  IPCCommand      `json:"Cmd"`
	Data json.RawMessage `json:"Data,omitempty"`
}

type IPCResponse struct {
	Code int             `json:"Code"`
	Msg  string          `json:"Msg"`
	Data json.RawMessage `json:"Data,omitempty"`
}

type SetConfigData struct {
	WsPort             int    `json:"ws_port"`
	MaxConcurrentTasks int    `json:"max_concurrent_tasks"`
	ProcessPriority    int    `json:"process_priority"`
	FFmpegPath         string `json:"ffmpeg_path"`
	TempDir            string `json:"temp_dir"`
	ListenLocalOnly    bool   `json:"listen_local_only"`
	AutoStart          bool   `json:"auto_start"`
	AutoRunOnBoot      bool   `json:"auto_run_on_boot"`
}

type SystemStatusData struct {
	WsPort        int    `json:"ws_port"`
	HttpPort      int    `json:"http_port"`
	RunningTasks  int    `json:"running_tasks"`
	WaitingTasks  int    `json:"waiting_tasks"`
	OnlineClients int    `json:"online_clients"`
	DiskFreeMB    int64  `json:"disk_free_mb"`
	MemoryUsageMB uint64 `json:"memory_usage_mb"`
}

var (
	serverRunning bool
	serverMutex   sync.Mutex
	listener      net.Listener
)

func StartIPCServer() error {
	serverMutex.Lock()
	if serverRunning {
		serverMutex.Unlock()
		return fmt.Errorf("IPC server already running")
	}

	var err error
	listener, err = net.Listen("tcp", ipcListenAddr)
	if err != nil {
		serverMutex.Unlock()
		return err
	}

	serverRunning = true
	serverMutex.Unlock()

	go acceptConnections()

	logger.Info("ipc", "IPC server started on: %s", ipcListenAddr)
	return nil
}

func acceptConnections() {
	for {
		conn, err := listener.Accept()
		if err != nil {
			serverMutex.Lock()
			if !serverRunning {
				serverMutex.Unlock()
				return
			}
			serverMutex.Unlock()
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}

	var req IPCRequest
	if err := json.Unmarshal(buf[:n], &req); err != nil {
		sendResponse(conn, -1, "Invalid request", nil)
		return
	}

	handleRequest(conn, req)
}

func handleRequest(conn net.Conn, req IPCRequest) {
	switch req.Cmd {
	case CmdStartService:
		handleStartService(conn)
	case CmdStopService:
		handleStopService(conn)
	case CmdGetConfig:
		handleGetConfig(conn)
	case CmdSetConfig:
		handleSetConfig(conn, req.Data)
	case CmdGetTasks:
		handleGetTasks(conn)
	case CmdStopTask:
		handleStopTask(conn, req.Data)
	case CmdClearWaiting:
		handleClearWaiting(conn)
	case CmdClearAll:
		handleClearAll(conn)
	case CmdGetLogs:
		handleGetLogs(conn)
	case CmdGetSystemStatus:
		handleGetSystemStatus(conn)
	case CmdGetHttpPort:
		handleGetHttpPort(conn)
	case CmdGetAuthKey:
		handleGetAuthKey(conn)
	default:
		sendResponse(conn, -1, "Unknown command", nil)
	}
}

func handleStartService(conn net.Conn) {
	cfg := config.Get()

	if server.IsPortInUse(cfg.WsPort) {
		sendResponse(conn, -1, "WebSocket port already in use", nil)
		return
	}

	httpPort, err := winapi.FindFreePort(10000, 65535)
	if err != nil {
		sendResponse(conn, -1, "Failed to find free HTTP port", nil)
		return
	}

	if err := task.Init(); err != nil {
		sendResponse(conn, -1, "Failed to init task manager: "+err.Error(), nil)
		return
	}

	if err := server.Init(cfg.WsPort, httpPort); err != nil {
		sendResponse(conn, -1, "Failed to start server: "+err.Error(), nil)
		return
	}

	sendResponse(conn, 0, "success", map[string]int{"http_port": httpPort})
	logger.Info("ipc", "Service started via IPC")
}

func handleStopService(conn net.Conn) {
	server.Stop()
	task.Stop()

	sendResponse(conn, 0, "success", nil)
	logger.Info("ipc", "Service stopped via IPC")
}

func handleGetConfig(conn net.Conn) {
	cfg := config.Get()

	data := map[string]interface{}{
		"ws_port":              cfg.WsPort,
		"max_concurrent_tasks": cfg.MaxConcurrentTasks,
		"process_priority":     cfg.ProcessPriority,
		"ffmpeg_path":          cfg.FFmpegPath,
		"temp_dir":             cfg.TempDir,
		"listen_local_only":    cfg.ListenLocalOnly,
		"auto_start":           cfg.AutoStart,
		"auto_run_on_boot":     cfg.AutoRunOnBoot,
	}

	sendResponse(conn, 0, "success", data)
}

func handleSetConfig(conn net.Conn, rawData json.RawMessage) {
	var data SetConfigData
	if err := json.Unmarshal(rawData, &data); err != nil {
		sendResponse(conn, -1, "Invalid config data", nil)
		return
	}

	if !ValidatePort(data.WsPort) {
		sendResponse(conn, -1, "Invalid WebSocket port", nil)
		return
	}

	cfg := config.Get()
	cfg.WsPort = data.WsPort
	cfg.MaxConcurrentTasks = data.MaxConcurrentTasks
	cfg.ProcessPriority = data.ProcessPriority
	cfg.FFmpegPath = data.FFmpegPath
	cfg.TempDir = data.TempDir
	cfg.ListenLocalOnly = data.ListenLocalOnly
	cfg.AutoStart = data.AutoStart
	cfg.AutoRunOnBoot = data.AutoRunOnBoot

	if err := config.Save(); err != nil {
		sendResponse(conn, -1, "Failed to save config", nil)
		return
	}

	sendResponse(conn, 0, "success", nil)
	logger.Info("ipc", "Config updated via IPC")
}

func handleGetTasks(conn net.Conn) {
	tasks := task.GetAllTasks()
	data := make([]map[string]interface{}, 0, len(tasks))

	for _, t := range tasks {
		data = append(data, map[string]interface{}{
			"task_id":          t.TaskID,
			"status":           string(t.Status),
			"source_file_name": t.SourceFileName,
			"output_file_path": t.OutputFilePath,
			"progress":         t.Progress,
			"resolution":       t.Resolution,
			"bitrate":          t.Bitrate,
			"create_time":      t.CreatedAt.Format(time.RFC3339),
		})
	}

	sendResponse(conn, 0, "success", data)
}

func handleStopTask(conn net.Conn, rawData json.RawMessage) {
	var data map[string]string
	if err := json.Unmarshal(rawData, &data); err != nil {
		sendResponse(conn, -1, "Invalid request", nil)
		return
	}

	taskID := data["task_id"]
	task.CancelTask(taskID)

	sendResponse(conn, 0, "success", nil)
}

func handleClearWaiting(conn net.Conn) {
	task.ClearWaitingTasks()
	sendResponse(conn, 0, "success", nil)
}

func handleClearAll(conn net.Conn) {
	task.ClearAllTasks()
	sendResponse(conn, 0, "success", nil)
}

func handleGetLogs(conn net.Conn) {
	logs := logger.GetRecentLogs(500)
	data := make([]map[string]interface{}, 0, len(logs))

	for _, log := range logs {
		data = append(data, map[string]interface{}{
			"time":    log.Time.Format("2006-01-02 15:04:05"),
			"level":   log.Level,
			"module":  log.Module,
			"message": log.Message,
		})
	}

	sendResponse(conn, 0, "success", data)
}

func handleGetSystemStatus(conn net.Conn) {
	cfg := config.Get()

	diskFreeBytes, _ := winapi.GetDiskFreeSpace(".")
	totalMem, availMem, _ := winapi.GetMemoryInfo()

	data := SystemStatusData{
		WsPort:        cfg.WsPort,
		HttpPort:      0,
		RunningTasks:  0,
		WaitingTasks:  0,
		OnlineClients: 0,
		DiskFreeMB:    diskFreeBytes / (1024 * 1024),
		MemoryUsageMB: (totalMem - availMem) / (1024 * 1024),
	}

	sendResponse(conn, 0, "success", data)
}

func handleGetHttpPort(conn net.Conn) {
	data := map[string]int{
		"http_port": task.GetHTTPPort(),
	}
	sendResponse(conn, 0, "success", data)
}

func handleGetAuthKey(conn net.Conn) {
	authKey := config.GetAuthKey()
	data := map[string]string{
		"auth_key": authKey,
	}
	sendResponse(conn, 0, "success", data)
}

func sendResponse(conn net.Conn, code int, msg string, data interface{}) {
	var rawData json.RawMessage
	if data != nil {
		jsonData, _ := json.Marshal(data)
		rawData = jsonData
	}

	resp := IPCResponse{
		Code: code,
		Msg:  msg,
		Data: rawData,
	}

	jsonResp, _ := json.Marshal(resp)
	conn.Write(jsonResp)
}

func StopIPCServer() {
	serverMutex.Lock()
	if serverRunning && listener != nil {
		listener.Close()
		serverRunning = false
	}
	serverMutex.Unlock()
}

func SendCommand(cmd IPCCommand, data interface{}) (*IPCResponse, error) {
	conn, err := net.DialTimeout("tcp", ipcListenAddr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to IPC server: %v", err)
	}
	defer conn.Close()

	var rawData json.RawMessage
	if data != nil {
		jsonData, _ := json.Marshal(data)
		rawData = jsonData
	}

	req := IPCRequest{
		Cmd:  cmd,
		Data: rawData,
	}

	jsonReq, _ := json.Marshal(req)
	conn.Write(jsonReq)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	var resp IPCResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return &resp, nil
}

func IsServiceRunning() bool {
	_, err := SendCommand(CmdGetSystemStatus, nil)
	return err == nil
}
