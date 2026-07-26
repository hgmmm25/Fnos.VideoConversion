package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"fvcc/logger"
)

type FFmpegProcess struct {
	TaskID     string
	Cmd        *exec.Cmd
	Progress   float64
	Running    bool
	stderrBuf  *bytes.Buffer
	mutex      sync.Mutex
	bufMutex   sync.Mutex
	OnProgress func(float64)
	OnComplete func(error)
}

var (
	processMutex sync.Mutex
	processes    = make(map[string]*FFmpegProcess)
	runningCount int32 // 当前运行中的本地转码任务数
)

func StartLocalTranscode(taskID, sourceFile, outputFile, ffmpegArgs string, onProgress func(float64), onComplete func(error)) error {
	processMutex.Lock()
	if _, exists := processes[taskID]; exists {
		processMutex.Unlock()
		return fmt.Errorf("任务 %s 已在转码中", taskID)
	}
	processMutex.Unlock()

	ffmpegPath := findFFmpeg()
	if ffmpegPath == "" {
		return fmt.Errorf("未找到 ffmpeg，请确保 ffmpeg 已安装并在 PATH 中")
	}

	args := strings.Fields(ffmpegArgs)
	fullArgs := append([]string{"-y", "-i", sourceFile}, args...)
	fullArgs = append(fullArgs, outputFile)

	// 直接执行ffmpeg
	var cmd *exec.Cmd = exec.Command(ffmpegPath, fullArgs...)
	setSysProcAttr(cmd)

	process := &FFmpegProcess{
		TaskID:     taskID,
		Cmd:        cmd,
		Progress:   0,
		Running:    true,
		stderrBuf:  &bytes.Buffer{},
		OnProgress: onProgress,
		OnComplete: onComplete,
	}

	processMutex.Lock()
	processes[taskID] = process
	processMutex.Unlock()

	stdoutBuf := &bytes.Buffer{}
	cmd.Stdout = stdoutBuf

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		processMutex.Lock()
		delete(processes, taskID)
		processMutex.Unlock()
		return err
	}

	logger.Info("localtranscode", "Starting local transcode: task=%s cmd=%s %v", taskID, cmd.Path, cmd.Args)

	if err := cmd.Start(); err != nil {
		processMutex.Lock()
		delete(processes, taskID)
		processMutex.Unlock()
		return err
	}

	go readStderrLocal(taskID, stderrPipe, process)
	go monitorProgressLocal(taskID, process)
	go waitForCompletionLocal(taskID, cmd, process)

	return nil
}

func StopLocalTranscode(taskID string) {
	processMutex.Lock()
	process, ok := processes[taskID]
	processMutex.Unlock()

	if !ok || !process.Running {
		return
	}

	if process.Cmd.Process != nil {
		if err := process.Cmd.Process.Kill(); err != nil {
			logger.Warn("localtranscode", "Failed to kill process for task %s: %v", taskID, err)
		}
	}

	process.mutex.Lock()
	process.Running = false
	process.mutex.Unlock()
}

func GetLocalTranscodeProgress(taskID string) float64 {
	processMutex.Lock()
	process, ok := processes[taskID]
	processMutex.Unlock()

	if !ok {
		return 0
	}

	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.Progress
}

func findFFmpeg() string {
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}

	ffmpegPaths := []string{
		"./ffmpeg.exe",
		"../ffmpeg.exe",
		"ffmpeg.exe",
		"./ffmpeg",
		"../ffmpeg",
		"/usr/bin/ffmpeg",
		"/usr/local/bin/ffmpeg",
		"/opt/mediasrv/bin/ffmpeg",
	}

	for _, p := range ffmpegPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

func readStderrLocal(taskID string, pipe io.ReadCloser, process *FFmpegProcess) {
	defer pipe.Close()

	buffer := make([]byte, 4096)
	for {
		n, err := pipe.Read(buffer)
		if n > 0 {
			process.bufMutex.Lock()
			process.stderrBuf.Write(buffer[:n])
			process.bufMutex.Unlock()
		}
		if err != nil {
			break
		}
	}
}

func monitorProgressLocal(taskID string, process *FFmpegProcess) {
	reProgress := regexp.MustCompile(`time=(\d+:\d+:\d+\.\d+)`)
	reDuration := regexp.MustCompile(`Duration: (\d+:\d+:\d+\.\d+)`)

	var duration float64 = 0
	startTime := time.Now()

	for {
		time.Sleep(200 * time.Millisecond)

		process.mutex.Lock()
		running := process.Running
		process.mutex.Unlock()

		if !running {
			break
		}

		process.bufMutex.Lock()
		output := process.stderrBuf.String()
		process.bufMutex.Unlock()

		if duration == 0 {
			if matches := reDuration.FindStringSubmatch(output); len(matches) > 1 {
				duration = parseTime(matches[1])
			}
		}

		if matches := reProgress.FindAllStringSubmatch(output, -1); len(matches) > 0 {
			lastMatch := matches[len(matches)-1]
			currentTime := parseTime(lastMatch[1])
			if duration > 0 {
				progress := (currentTime / duration) * 100
				if progress > 99.9 {
					progress = 99.9
				}
				process.mutex.Lock()
				process.Progress = progress
				process.mutex.Unlock()
				if process.OnProgress != nil {
					process.OnProgress(progress)
				}
			}
		} else if duration == 0 {
			elapsed := time.Since(startTime).Seconds()
			if elapsed > 3 && elapsed < 30 {
				progress := (elapsed / 30) * 5
				if progress > 0 && progress < 5 {
					process.mutex.Lock()
					process.Progress = progress
					process.mutex.Unlock()
					if process.OnProgress != nil {
						process.OnProgress(progress)
					}
				}
			}
		}
	}
}

func waitForCompletionLocal(taskID string, cmd *exec.Cmd, process *FFmpegProcess) {
	err := cmd.Wait()

	process.mutex.Lock()
	process.Running = false
	process.mutex.Unlock()

	processMutex.Lock()
	delete(processes, taskID)
	processMutex.Unlock()

	// 减少运行计数
	atomic.AddInt32(&runningCount, -1)

	if err != nil {
		process.bufMutex.Lock()
		stderr := process.stderrBuf.String()
		process.bufMutex.Unlock()
		logger.Error("localtranscode", "FFmpeg process exited with error: task=%s err=%v", taskID, err)
		logger.Debug("localtranscode", "FFmpeg stderr for task %s: %s", taskID, stderr)
	} else {
		logger.Info("localtranscode", "FFmpeg process completed: task=%s", taskID)
	}

	if process.OnComplete != nil {
		if err != nil {
			process.bufMutex.Lock()
			stderr := process.stderrBuf.String()
			process.bufMutex.Unlock()
			if strings.Contains(stderr, "Invalid data found when processing input") {
				err = fmt.Errorf("源文件无效或损坏")
			} else if strings.Contains(stderr, "Permission denied") {
				err = fmt.Errorf("权限不足，无法访问文件")
			} else if exitErr, ok := err.(*exec.ExitError); ok {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && getExitCode(status) == 1 {
					err = fmt.Errorf("转码失败")
				}
			}
		}
		process.OnComplete(err)
	}
}

func parseTime(timeStr string) float64 {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return 0
	}

	hours, _ := strconv.ParseFloat(parts[0], 64)
	minutes, _ := strconv.ParseFloat(parts[1], 64)
	seconds, _ := strconv.ParseFloat(parts[2], 64)

	return hours*3600 + minutes*60 + seconds
}
