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

	// ---- 03 §3.1 新增字段（RenderEDL / GenProxy）----
	// 说明：既有 FVCC 客户端使用 PascalCase 线上字段名，新增字段延续 PascalCase 以保持一致性；
	// 同时兼容设计文档中的小写驼峰写法，见 wire_compat.go 的 UnmarshalJSON 归一化。
	TaskType      string          `json:"TaskType,omitempty"`      // "TRANSCODE"(默认) | "RENDER_EDL" | "GEN_PROXY"
	PriorityLevel string          `json:"PriorityLevel,omitempty"` // "low" | "normal" | "high"（与既有 Priority int 并存）
	SMBOutputPath string          `json:"SMBOutputPath,omitempty"` // 目标目录 UNC
	CredentialID  string          `json:"CredentialId,omitempty"`  // 本地凭据档案键；空=默认档案
	Payload       json.RawMessage `json:"Payload,omitempty"`       // RenderEDL / GenProxy 结构化载荷
	TraceId       string          `json:"TraceId,omitempty"`       // P2-1：任务链路追踪 ID（FVCC 下发时生成，跨端日志聚合）
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

// CreateRenderEDLResponseData CreateRenderEDL 回包（03 §3.1）
type CreateRenderEDLResponseData struct {
	TaskId             string   `json:"TaskId"`
	TaskType           string   `json:"TaskType"`
	Status             string   `json:"Status"`
	ProjectID          string   `json:"ProjectId,omitempty"`
	ProjectRev         int      `json:"ProjectRev,omitempty"`
	Checksum           string   `json:"Checksum,omitempty"`
	FastCopyAllowed    bool     `json:"FastCopyAllowed"`
	EffectivePresetKey string   `json:"EffectivePresetKey,omitempty"`
	Warnings           []string `json:"Warnings,omitempty"`
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

	// ---- 阶段化进度（03 §3.3 / 05 §6.1）----
	TaskType   string `json:"TaskType,omitempty"`
	Stage      string `json:"Stage,omitempty"`
	StageIndex int    `json:"StageIndex,omitempty"`
	StageTotal int    `json:"StageTotal,omitempty"`
	ProfileKey string `json:"ProfileKey,omitempty"`
	Degraded   bool   `json:"Degraded,omitempty"`
	Warnings   string `json:"Warnings,omitempty"`

	// ---- 失败节点细化上报（03 §3.3；代理 E_RENDER_FAILED 闭环）----
	// Status=Failed 时携带具体错误码与失败原因，供 FVCC 侧任务面板直接展示，
	// 避免只剩"节点状态 Failed"这类无法定位的泛化信息。
	ErrorCode    string `json:"ErrorCode,omitempty"`
	ErrorMessage string `json:"ErrorMessage,omitempty"`
}

type GetHttpPortResponseData struct {
	HttpPort  int `json:"HttpPort"`
	ChunkSize int `json:"ChunkSize"`
}

type ProgressData struct {
	TaskId   string  `json:"task_id"`
	Progress float64 `json:"progress"`

	// ---- 阶段化进度（03 §3.3 / 05 §6.1，仅 RenderEDL 任务有值）----
	TaskType   string `json:"TaskType,omitempty"`
	Stage      string `json:"Stage,omitempty"`
	StageIndex int    `json:"StageIndex,omitempty"`
	StageTotal int    `json:"StageTotal,omitempty"`
	ProfileKey string `json:"ProfileKey,omitempty"`
	Degraded   bool   `json:"Degraded,omitempty"`
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

	return buildProgressFrame(data)
}

// BuildProgressEx 构建带阶段信息的进度帧（03 §3.3 / 05 §6.1）。
// stageInfo 为 nil 时退化为普通进度帧。
func BuildProgressEx(taskID string, progress float64, taskType, stage string, stageIndex, stageTotal int, profileKey string, degraded bool) ([]byte, error) {
	data := ProgressData{
		TaskId:     taskID,
		Progress:   progress,
		TaskType:   taskType,
		Stage:      stage,
		StageIndex: stageIndex,
		StageTotal: stageTotal,
		ProfileKey: profileKey,
		Degraded:   degraded,
	}

	return buildProgressFrame(data)
}

func buildProgressFrame(data ProgressData) ([]byte, error) {
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
