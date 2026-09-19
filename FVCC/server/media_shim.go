package main

import "fvcc/internal/media"

// media_shim.go：根包兼容层（P2-1 阶段 B media 域叶子文件迁入 internal/media）。
// 经本层转发的符号：NewFFprobe / FFprobe（隐式，仅注释引用）、StartLocalTranscode /
// StopLocalTranscode / GetLocalTranscodeProgress。根包引用点零改动。

// NewFFprobe 创建 ffprobe 封装（实现转发至 internal/media）。
func NewFFprobe() *media.FFprobe { return media.NewFFprobe() }

// StartLocalTranscode 启动本地转码（转发至 internal/media）。
func StartLocalTranscode(taskID, sourceFile, outputFile, ffmpegArgs string, onProgress func(float64), onComplete func(error)) error {
	return media.StartLocalTranscode(taskID, sourceFile, outputFile, ffmpegArgs, onProgress, onComplete)
}

// StopLocalTranscode 停止本地转码（转发至 internal/media）。
func StopLocalTranscode(taskID string) { media.StopLocalTranscode(taskID) }

// GetLocalTranscodeProgress 查询本地转码进度（转发至 internal/media）。
func GetLocalTranscodeProgress(taskID string) float64 { return media.GetLocalTranscodeProgress(taskID) }

// findFFmpeg 查找 ffmpeg 可执行文件（转发至 internal/media）。
func findFFmpeg() string { return media.FindFFmpeg() }

// RunningCount 返回本地转码运行计数（转发至 internal/media）。
func RunningCount() int32 { return media.RunningCount() }

// AddRunningCount 原子增减本地转码运行计数（转发至 internal/media）。
func AddRunningCount(delta int32) int32 { return media.AddRunningCount(delta) }

// SetProxyProbe 注入代理校验探测通道（转发至 internal/media.ProxyWorkflow；单测/装配入口）。
