package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"fvcc/logger"
)

type RemoteClient struct {
	mu             sync.Mutex
	conns          map[string]*wsConn
	httpClient     *http.Client
	OnPushProgress func(serverID, taskID string, progress float64)
	OnDisconnect   func(serverID string)
}

func NewRemoteClient() *RemoteClient {
	return &RemoteClient{
		conns: map[string]*wsConn{},
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

type progressPush struct {
	TaskID   string  `json:"task_id"`
	Progress float64 `json:"progress"`
}

func (rc *RemoteClient) Connect(server Server) (int, int, error) {
	rc.mu.Lock()
	wc, exists := rc.conns[server.ID]
	rc.mu.Unlock()
	if exists && wc != nil {
		return wc.httpPort, wc.chunkSize, nil
	}

	wsURL := fmt.Sprintf("ws://%s:%d/ws", server.IP, server.Port)
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   1024 * 1024,
		WriteBufferSize:  1024 * 1024,
	}

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

	wc = &wsConn{
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
			}
			return
		}

		if resp.Cmd != "" {
			if resp.Cmd == "Progress" && len(resp.Data) > 0 {
				var p progressPush
				if err := json.Unmarshal(resp.Data, &p); err == nil && p.TaskID != "" {
					if rc.OnPushProgress != nil {
						rc.OnPushProgress(serverID, p.TaskID, p.Progress)
					}
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
	rc.mu.Unlock()
	if ok && wc != nil {
		close(wc.done)
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
			wc.conn.Close()
			<-wc.pumpDone
			<-wc.pingDone
		}
		delete(rc.conns, id)
	}
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

func (rc *RemoteClient) QueryTask(server Server, remoteTaskID string) (status string, progress float64, err error) {
	wc, err := rc.getConn(server)
	if err != nil {
		return "", 0, err
	}

	resp, err := wc.roundTrip(wsCmd{Cmd: "QueryTask", Key: server.AuthKey, TaskId: remoteTaskID}, 30*time.Second)
	if err != nil {
		rc.CloseConn(server.ID)
		return "", 0, fmt.Errorf("QueryTask 调用失败: %w", err)
	}
	if resp.Code != 0 {
		return "", 0, fmt.Errorf("QueryTask 失败: %s", resp.Msg)
	}
	var qd struct {
		TaskId   string  `json:"TaskId"`
		Status   string  `json:"Status"`
		Progress float64 `json:"Progress"`
	}
	_ = json.Unmarshal(resp.Data, &qd)
	return qd.Status, qd.Progress, nil
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
