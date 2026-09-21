package protocol

import "encoding/json"

// 线协议字段名兼容层（03 §3.1）
//
// 背景：FVCS 既有线上协议使用 PascalCase（Cmd / TaskId / SMBPath ...），已在旧版 FVCC 客户端
// 与渲染机之间稳定运行；设计文档 03 §3.1 示例使用小写驼峰（cmd / taskId / smbOutputPath ...）。
// 为同时满足"不破坏存量客户端"与"新增字段可与文档示例直接对接"，本文件在 UnmarshalJSON
// 中对两类写法做归一化：PascalCase 优先，小写驼峰作为补充来源，两者都出现时以 PascalCase 为准。

// wsRequestLower 小写驼峰视图（仅用于归一化读取，不参与序列化）
type wsRequestLower struct {
	Cmd            string          `json:"cmd"`
	Key            string          `json:"key"`
	SourceFileName string          `json:"sourceFileName"`
	OutputName     string          `json:"outputName"`
	FFmpegArgs     string          `json:"ffmpegArgs"`
	TaskId         string          `json:"taskId"`
	TaskType       string          `json:"taskType"`
	Priority       string          `json:"priority"`
	SMBPath        string          `json:"smbPath"`
	SMBOutputPath  string          `json:"smbOutputPath"`
	CredentialID   string          `json:"credentialId"`
	TraceId        string          `json:"traceId"`
	SMBUser        string          `json:"smbUser"`
	SMBPassword    string          `json:"smbPassword"`
	Data           json.RawMessage `json:"data"`
	Payload        json.RawMessage `json:"payload"`
}

// UnmarshalJSON 双写法兼容反序列化
func (r *WebSocketRequest) UnmarshalJSON(b []byte) error {
	type plain WebSocketRequest // 避免递归调用自身
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var low wsRequestLower
	_ = json.Unmarshal(b, &low) // 小写视图失败不影响主解析

	if p.Cmd == "" {
		p.Cmd = low.Cmd
	}
	if p.Key == "" {
		p.Key = low.Key
	}
	if p.SourceFileName == "" {
		p.SourceFileName = low.SourceFileName
	}
	if p.OutputName == "" {
		p.OutputName = low.OutputName
	}
	if p.FFmpegArgs == "" {
		p.FFmpegArgs = low.FFmpegArgs
	}
	if p.TaskId == "" {
		p.TaskId = low.TaskId
	}
	if p.TaskType == "" {
		p.TaskType = low.TaskType
	}
	if p.PriorityLevel == "" {
		p.PriorityLevel = low.Priority
	}
	if p.SMBPath == "" {
		p.SMBPath = low.SMBPath
	}
	if p.SMBOutputPath == "" {
		p.SMBOutputPath = low.SMBOutputPath
	}
	if p.CredentialID == "" {
		p.CredentialID = low.CredentialID
	}
	if p.TraceId == "" {
		p.TraceId = low.TraceId
	}
	if p.SMBUser == "" {
		p.SMBUser = low.SMBUser
	}
	if p.SMBPassword == "" {
		p.SMBPassword = low.SMBPassword
	}
	if len(p.Payload) == 0 {
		p.Payload = low.Payload
	}
	if len(p.Data) == 0 {
		p.Data = low.Data
	}

	*r = WebSocketRequest(p)
	return nil
}

// EffectiveTaskType 归一化任务类型：空值按既有行为视为 TRANSCODE
func (r *WebSocketRequest) EffectiveTaskType() string {
	if r.TaskType == "" {
		return TaskTypeTranscode
	}
	return r.TaskType
}

// HasLegacyArgs 是否携带既有直通 ffmpeg 参数（RenderEDL 下必须为空，否则 E_PROTO_FIELD_CONFLICT）
func (r *WebSocketRequest) HasLegacyArgs() bool {
	return r.FFmpegArgs != ""
}
