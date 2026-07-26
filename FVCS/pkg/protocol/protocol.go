package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"regexp"
)

type CmdType string

const (
	CmdAuth            CmdType = "Auth"
	CmdCreateTask      CmdType = "CreateTask"
	CmdCreateSMBTask   CmdType = "CreateSMBTask"
	CmdGetTask         CmdType = "GetTask"
	CmdQueryTask       CmdType = "QueryTask"
	CmdGetTasks        CmdType = "GetTasks"
	CmdStopTask        CmdType = "StopTask"
	CmdCancelTask      CmdType = "CancelTask"
	CmdPauseTask       CmdType = "PauseTask"
	CmdResumeTask      CmdType = "ResumeTask"
	CmdUploadFinish    CmdType = "UploadFinish"
	CmdDownloadFinish  CmdType = "DownloadFinish"
	CmdGetHttpPort     CmdType = "GetHttpPort"
	CmdGetSystemStatus CmdType = "GetSystemStatus"
	CmdHeartbeat       CmdType = "Heartbeat"
	CmdProgress        CmdType = "Progress"
)

type WebSocketRequest struct {
	Cmd            string          `json:"Cmd"`
	Key            string          `json:"Key,omitempty"`
	SourceFileName string          `json:"SourceFileName,omitempty"`
	OutputName     string          `json:"OutputName,omitempty"`
	FFmpegArgs     string          `json:"FFmpegArgs,omitempty"`
	TaskId         string          `json:"TaskId,omitempty"`
	Priority       int             `json:"Priority,omitempty"`
	Data           json.RawMessage `json:"Data,omitempty"`
	SMBPath        string          `json:"SMBPath,omitempty"`
	SMBUser        string          `json:"SMBUser,omitempty"`
	SMBPassword    string          `json:"SMBPassword,omitempty"`
}

type WebSocketResponse struct {
	Code int             `json:"Code"`
	Msg  string          `json:"Msg"`
	Data json.RawMessage `json:"Data"`
}

type AuthRequestData struct {
	AuthKey string `json:"auth_key"`
}

type CreateTaskRequestData struct {
	SourceFileName string `json:"SourceFileName"`
	OutputName     string `json:"OutputName"`
	FFmpegArgs     string `json:"FFmpegArgs"`
	Priority       int    `json:"Priority,omitempty"`
}

type CreateSMBTaskRequestData struct {
	SourceFileName string `json:"SourceFileName"`
	OutputName     string `json:"OutputName"`
	FFmpegArgs     string `json:"FFmpegArgs"`
	Priority       int    `json:"Priority,omitempty"`
	SMBPath        string `json:"SMBPath"`
	SMBUser        string `json:"SMBUser"`
	SMBPassword    string `json:"SMBPassword"`
}

type CreateTaskResponseData struct {
	TaskId string `json:"TaskId"`
}

type GetTaskResponseData struct {
	TaskId         string  `json:"TaskId"`
	Status         string  `json:"Status"`
	SourceFileName string  `json:"SourceFileName"`
	OutputFilePath string  `json:"OutputFilePath"`
	Progress       float64 `json:"Progress"`
	Resolution     string  `json:"Resolution"`
	Bitrate        string  `json:"Bitrate"`
	CreateTime     string  `json:"CreateTime"`
}

type GetHttpPortResponseData struct {
	HttpPort  int `json:"HttpPort"`
	ChunkSize int `json:"ChunkSize"`
}

type ProgressData struct {
	TaskId   string  `json:"task_id"`
	Progress float64 `json:"progress"`
}

type SystemStatusData struct {
	WsPort        int   `json:"ws_port"`
	HttpPort      int   `json:"http_port"`
	RunningTasks  int   `json:"running_tasks"`
	WaitingTasks  int   `json:"waiting_tasks"`
	OnlineClients int   `json:"online_clients"`
	MemoryUsage   int   `json:"memory_usage"`
	DiskFreeMB    int64 `json:"disk_free_mb"`
}

const (
	ChunkTypeData = 0x01
)

type ChunkHeader struct {
	Type         uint8
	TaskIDLength uint32
	TaskID       string
	ChunkIndex   uint32
	IsLastChunk  uint8
	DataLength   uint64
	HeaderCRC32  uint32
	DataCRC32    uint32
}

func ParseWebSocketRequest(data []byte) (*WebSocketRequest, error) {
	var req WebSocketRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func BuildResponse(code int, msg string, data interface{}) ([]byte, error) {
	var rawData json.RawMessage
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		rawData = jsonData
	}

	resp := WebSocketResponse{
		Code: code,
		Msg:  msg,
		Data: rawData,
	}

	return json.Marshal(resp)
}

func BuildSuccessResponse(data interface{}) ([]byte, error) {
	return BuildResponse(0, "success", data)
}

func BuildErrorResponse(msg string) ([]byte, error) {
	return BuildResponse(-1, msg, nil)
}

func BuildProgress(taskID string, progress float64) ([]byte, error) {
	data := ProgressData{
		TaskId:   taskID,
		Progress: progress,
	}

	req := WebSocketRequest{
		Cmd: string(CmdProgress),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	req.Data = jsonData

	return json.Marshal(req)
}

func ValidateExtraFFmpegArgs(args string) bool {
	if args == "" {
		return true
	}

	dangerousPatterns := []string{
		"&",
		";",
		"\\|",
		">",
		"<",
		"`.*`",
	}

	for _, pattern := range dangerousPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(args) {
			return false
		}
	}

	return true
}

func ValidatePort(port int) bool {
	return port >= 1 && port <= 65535
}

func ValidateChunkCount(count int) bool {
	return count > 0 && count <= 10000
}

func BuildChunkHeader(taskID string, chunkIndex int, isLastChunk bool, dataLength int64) ChunkHeader {
	header := ChunkHeader{
		Type:         ChunkTypeData,
		TaskIDLength: uint32(len(taskID)),
		TaskID:       taskID,
		ChunkIndex:   uint32(chunkIndex),
		IsLastChunk:  0,
		DataLength:   uint64(dataLength),
	}

	if isLastChunk {
		header.IsLastChunk = 1
	}

	header.HeaderCRC32 = calculateHeaderCRC32(header)

	return header
}

func calculateHeaderCRC32(header ChunkHeader) uint32 {
	var buf bytes.Buffer
	buf.WriteByte(header.Type)
	binary.Write(&buf, binary.BigEndian, header.TaskIDLength)
	buf.WriteString(header.TaskID)
	binary.Write(&buf, binary.BigEndian, header.ChunkIndex)
	buf.WriteByte(header.IsLastChunk)
	binary.Write(&buf, binary.BigEndian, header.DataLength)

	return crc32.ChecksumIEEE(buf.Bytes())
}

func CalculateDataCRC32(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

func SerializeChunk(taskID string, chunkIndex int, isLastChunk bool, data []byte) []byte {
	header := BuildChunkHeader(taskID, chunkIndex, isLastChunk, int64(len(data)))
	dataCRC32 := CalculateDataCRC32(data)

	var buf bytes.Buffer
	buf.WriteByte(header.Type)
	binary.Write(&buf, binary.BigEndian, header.TaskIDLength)
	buf.WriteString(header.TaskID)
	binary.Write(&buf, binary.BigEndian, header.ChunkIndex)
	buf.WriteByte(header.IsLastChunk)
	binary.Write(&buf, binary.BigEndian, header.DataLength)
	binary.Write(&buf, binary.BigEndian, header.HeaderCRC32)
	binary.Write(&buf, binary.BigEndian, dataCRC32)
	buf.Write(data)

	return buf.Bytes()
}

func ParseChunk(data []byte) (*ChunkHeader, []byte, error) {
	if len(data) < 26 {
		return nil, nil, fmt.Errorf("chunk data too short")
	}

	buf := bytes.NewReader(data)

	var header ChunkHeader
	if err := binary.Read(buf, binary.BigEndian, &header.Type); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.TaskIDLength); err != nil {
		return nil, nil, err
	}

	taskID := make([]byte, header.TaskIDLength)
	if _, err := buf.Read(taskID); err != nil {
		return nil, nil, err
	}
	header.TaskID = string(taskID)

	if err := binary.Read(buf, binary.BigEndian, &header.ChunkIndex); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.IsLastChunk); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.DataLength); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.HeaderCRC32); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.DataCRC32); err != nil {
		return nil, nil, err
	}

	calculatedCRC32 := calculateHeaderCRC32(header)
	if calculatedCRC32 != header.HeaderCRC32 {
		return nil, nil, fmt.Errorf("header CRC32 mismatch")
	}

	chunkData := make([]byte, header.DataLength)
	if _, err := buf.Read(chunkData); err != nil {
		return nil, nil, err
	}

	calculatedDataCRC32 := CalculateDataCRC32(chunkData)
	if calculatedDataCRC32 != header.DataCRC32 {
		return nil, nil, fmt.Errorf("data CRC32 mismatch")
	}

	return &header, chunkData, nil
}
