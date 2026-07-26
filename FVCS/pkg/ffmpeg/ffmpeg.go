package ffmpeg

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/winapi"
)

type HardwareAccelType string

const (
	AccelNVENC HardwareAccelType = "nvenc"
	AccelQSV   HardwareAccelType = "qsv"
	AccelAMF   HardwareAccelType = "amf"
	AccelVAAPI HardwareAccelType = "vaapi"
	AccelNone  HardwareAccelType = "none"
)

// getPriorityFlags 根据配置的 ProcessPriority 返回 Windows 进程优先级 CreationFlags
// 1=低, 2=低于正常, 3=正常, 4=高于正常, 5=高
func getPriorityFlags(priority int) uint32 {
	switch priority {
	case 1:
		return 0x00000040 // IDLE_PRIORITY_CLASS
	case 2:
		return 0x00004000 // BELOW_NORMAL_PRIORITY_CLASS
	case 4:
		return 0x00008000 // ABOVE_NORMAL_PRIORITY_CLASS
	case 5:
		return 0x00000080 // HIGH_PRIORITY_CLASS
	default: // 3 或其他值
		return 0x00000020 // NORMAL_PRIORITY_CLASS
	}
}

type FFmpegProcess struct {
	TaskID    string
	Cmd       *exec.Cmd
	Progress  float64
	Running   bool
	mutex     sync.Mutex
	stderrBuf *bytes.Buffer
	bufMutex  sync.Mutex
}

type TaskInfo struct {
	TaskID         string
	SourceFileName string
	OutputName     string
	FFmpegArgs     string
}

var (
	processes     map[string]*FFmpegProcess
	processMutex  sync.Mutex
	detectedAccel HardwareAccelType
	onSuccess     func(taskID, outputPath string)
	onFailed      func(taskID string)
	jobHandle     winapi.JobHandle
)

func init() {
	processes = make(map[string]*FFmpegProcess)

	var err error
	jobHandle, err = winapi.CreateJobObject()
	if err != nil {
		logger.Warn("ffmpeg", "Failed to create job object, child processes may not be cleaned up: %v", err)
	} else {
		logger.Info("ffmpeg", "Job object created for process management")
	}
}

func SetCallbacks(success func(taskID, outputPath string), failed func(taskID string)) {
	onSuccess = success
	onFailed = failed
}

func DetectHardwareAccel() HardwareAccelType {
	if detectedAccel != "" {
		return detectedAccel
	}

	cfg := config.Get()
	if !isFFmpegAvailable(cfg.FFmpegPath) {
		detectedAccel = AccelNone
		return detectedAccel
	}

	cmd := exec.Command(cfg.FFmpegPath, "-encoders")
	output, err := cmd.CombinedOutput()
	if err != nil {
		detectedAccel = AccelNone
		return detectedAccel
	}

	outputStr := string(output)

	if strings.Contains(outputStr, "h264_nvenc") || strings.Contains(outputStr, "hevc_nvenc") {
		detectedAccel = AccelNVENC
		logger.Info("ffmpeg", "Detected NVENC hardware acceleration")
		return detectedAccel
	}

	if strings.Contains(outputStr, "h264_qsv") || strings.Contains(outputStr, "hevc_qsv") {
		detectedAccel = AccelQSV
		logger.Info("ffmpeg", "Detected QSV hardware acceleration")
		return detectedAccel
	}

	if strings.Contains(outputStr, "h264_amf") || strings.Contains(outputStr, "hevc_amf") {
		detectedAccel = AccelAMF
		logger.Info("ffmpeg", "Detected AMF hardware acceleration")
		return detectedAccel
	}

	if strings.Contains(outputStr, "h264_vaapi") || strings.Contains(outputStr, "hevc_vaapi") {
		detectedAccel = AccelVAAPI
		logger.Info("ffmpeg", "Detected VAAPI hardware acceleration")
		return detectedAccel
	}

	detectedAccel = AccelNone
	logger.Warn("ffmpeg", "No hardware acceleration detected, falling back to software encoding")
	return detectedAccel
}

func isFFmpegAvailable(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var inputOptions = map[string]bool{
	"-hwaccel":               true,
	"-hwaccel_device":        true,
	"-hwaccel_output_format": true,
	"-avioflags":             true,
	"-f":                     true,
	"-c:v":                   false,
	"-c:a":                   false,
}

var streamMapOptions = map[string]bool{
	"-map": true,
}

func BuildFFmpegCommand(taskInfo TaskInfo, inputPath, outputPath string) []string {
	args := []string{"-y"}

	if taskInfo.FFmpegArgs != "" {
		extraArgs := strings.Fields(taskInfo.FFmpegArgs)

		var inputOpts []string
		var streamOpts []string
		var outputOpts []string

		for i := 0; i < len(extraArgs); i++ {
			arg := extraArgs[i]
			if strings.HasPrefix(arg, "-") {
				if inputOptions[arg] {
					inputOpts = append(inputOpts, arg)
					if i+1 < len(extraArgs) && !strings.HasPrefix(extraArgs[i+1], "-") {
						inputOpts = append(inputOpts, extraArgs[i+1])
						i++
					}
				} else if streamMapOptions[arg] {
					streamOpts = append(streamOpts, arg)
					if i+1 < len(extraArgs) && !strings.HasPrefix(extraArgs[i+1], "-") {
						streamOpts = append(streamOpts, extraArgs[i+1])
						i++
					}
				} else {
					outputOpts = append(outputOpts, arg)
					if i+1 < len(extraArgs) && !strings.HasPrefix(extraArgs[i+1], "-") {
						outputOpts = append(outputOpts, extraArgs[i+1])
						i++
					}
				}
			}
		}

		args = append(args, inputOpts...)
	}

	args = append(args, "-i", inputPath)

	if taskInfo.FFmpegArgs != "" {
		extraArgs := strings.Fields(taskInfo.FFmpegArgs)

		var streamOpts []string
		var outputOpts []string

		for i := 0; i < len(extraArgs); i++ {
			arg := extraArgs[i]
			if strings.HasPrefix(arg, "-") {
				if inputOptions[arg] {
					continue
				} else if streamMapOptions[arg] {
					streamOpts = append(streamOpts, arg)
					if i+1 < len(extraArgs) && !strings.HasPrefix(extraArgs[i+1], "-") {
						streamOpts = append(streamOpts, extraArgs[i+1])
						i++
					}
				} else {
					outputOpts = append(outputOpts, arg)
					if i+1 < len(extraArgs) && !strings.HasPrefix(extraArgs[i+1], "-") {
						outputOpts = append(outputOpts, extraArgs[i+1])
						i++
					}
				}
			}
		}

		args = append(args, streamOpts...)
		args = append(args, outputOpts...)
	}

	args = append(args, outputPath)
	return args
}

func StartTranscode(taskInfo TaskInfo, inputPath string) error {
	cfg := config.Get()

	var taskDir string
	if filepath.IsAbs(cfg.TempDir) {
		taskDir = filepath.Join(cfg.TempDir, taskInfo.TaskID)
	} else {
		taskDir = filepath.Join(getAppDir(), cfg.TempDir, taskInfo.TaskID)
	}
	outputFileName := taskInfo.OutputName
	if outputFileName == "" {
		outputFileName = "output.mp4"
	}
	outputPath := filepath.Join(taskDir, outputFileName)

	return startTranscodeInternal(taskInfo, inputPath, outputPath)
}

func StartSMBTranscode(taskInfo TaskInfo, inputPath, outputPath string) error {
	return startTranscodeInternal(taskInfo, inputPath, outputPath)
}

func startTranscodeInternal(taskInfo TaskInfo, inputPath, outputPath string) error {
	cfg := config.Get()

	if !isFFmpegAvailable(cfg.FFmpegPath) {
		return fmt.Errorf("ffmpeg not found at: %s", cfg.FFmpegPath)
	}

	args := BuildFFmpegCommand(taskInfo, inputPath, outputPath)

	cmd := exec.Command(cfg.FFmpegPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000 | getPriorityFlags(cfg.ProcessPriority),
	}

	process := &FFmpegProcess{
		TaskID:    taskInfo.TaskID,
		Cmd:       cmd,
		Progress:  0,
		Running:   true,
		stderrBuf: &bytes.Buffer{},
	}

	processMutex.Lock()
	processes[taskInfo.TaskID] = process
	processMutex.Unlock()

	stdoutBuf := &bytes.Buffer{}
	cmd.Stdout = stdoutBuf

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		processMutex.Lock()
		delete(processes, taskInfo.TaskID)
		processMutex.Unlock()
		return err
	}

	logger.Info("ffmpeg", "Starting transcode: cmd=%s %v", cfg.FFmpegPath, args)

	if err := cmd.Start(); err != nil {
		processMutex.Lock()
		delete(processes, taskInfo.TaskID)
		processMutex.Unlock()
		return err
	}

	if jobHandle != 0 {
		if err := winapi.AssignProcessToJob(jobHandle, cmd.Process.Pid); err != nil {
			logger.Warn("ffmpeg", "Failed to assign process %d to job object: %v", cmd.Process.Pid, err)
		} else {
			logger.Info("ffmpeg", "Process %d assigned to job object", cmd.Process.Pid)
		}
	}

	go readStderr(taskInfo.TaskID, stderrPipe, process)
	go monitorProgress(taskInfo.TaskID, process)
	go waitForCompletion(taskInfo.TaskID, cmd, outputPath, process)

	return nil
}

func getAppDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func readStderr(taskID string, pipe io.ReadCloser, process *FFmpegProcess) {
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

func monitorProgress(taskID string, process *FFmpegProcess) {
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
			match := reDuration.FindStringSubmatch(output)
			if len(match) == 2 {
				duration = parseTime(match[1])
				logger.Info("ffmpeg", "monitorProgress: found duration=%.2fs, taskID=%s", duration, taskID)
			}
		}

		matches := reProgress.FindAllStringSubmatch(output, -1)
		if len(matches) > 0 {
			currentTime := parseTime(matches[len(matches)-1][1])
			if duration > 0 {
				progress := (currentTime / duration) * 100
				if progress > 100 {
					progress = 100
				}
				if progress > process.Progress {
					process.mutex.Lock()
					process.Progress = progress
					process.mutex.Unlock()
				}
			}
		}

		if time.Since(startTime) > 5*time.Minute {
			process.mutex.Lock()
			currentProgress := process.Progress
			process.mutex.Unlock()

			if currentProgress < 1 {
				logger.Warn("ffmpeg", "Progress stuck, marking task as failed: ", taskID)
				StopTranscode(taskID)
				return
			}
		}
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

func waitForCompletion(taskID string, cmd *exec.Cmd, outputPath string, process *FFmpegProcess) {
	err := cmd.Wait()

	process.mutex.Lock()
	process.Running = false
	process.mutex.Unlock()

	if err != nil {
		logger.Error("ffmpeg", "Transcode failed for task ", taskID, ": ", err)
		process.bufMutex.Lock()
		stderrOutput := process.stderrBuf.String()
		process.bufMutex.Unlock()
		if stderrOutput != "" {
			logger.Error("ffmpeg", "FFmpeg stderr output:\n", stderrOutput)
		}
		process.mutex.Lock()
		if process.Progress > 99 {
			process.Progress = 99
		}
		process.mutex.Unlock()
		processMutex.Lock()
		delete(processes, taskID)
		processMutex.Unlock()
		if onFailed != nil {
			onFailed(taskID)
		}
		return
	}

	if _, err := os.Stat(outputPath); err != nil {
		logger.Error("ffmpeg", "Output file not found: ", outputPath)
		processMutex.Lock()
		delete(processes, taskID)
		processMutex.Unlock()
		if onFailed != nil {
			onFailed(taskID)
		}
		return
	}

	process.mutex.Lock()
	process.Progress = 100
	process.mutex.Unlock()
	processMutex.Lock()
	delete(processes, taskID)
	processMutex.Unlock()

	if onSuccess != nil {
		onSuccess(taskID, outputPath)
	}
	logger.Info("ffmpeg", "Transcode completed successfully: ", taskID)
}

func StopTranscode(taskID string) {
	processMutex.Lock()
	process, ok := processes[taskID]
	processMutex.Unlock()

	if !ok || !process.Running {
		return
	}

	if process.Cmd.Process != nil {
		process.Cmd.Process.Kill()
	}

	process.mutex.Lock()
	process.Running = false
	process.mutex.Unlock()

	processMutex.Lock()
	delete(processes, taskID)
	processMutex.Unlock()
}

func GetProgress(taskID string) float64 {
	processMutex.Lock()
	defer processMutex.Unlock()

	process, ok := processes[taskID]
	if !ok {
		return 0
	}

	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.Progress
}

func GetActiveProcessCount() int {
	processMutex.Lock()
	defer processMutex.Unlock()
	return len(processes)
}

func StopAll() {
	processMutex.Lock()
	taskIDs := make([]string, 0, len(processes))
	for taskID := range processes {
		taskIDs = append(taskIDs, taskID)
	}
	processMutex.Unlock()

	for _, taskID := range taskIDs {
		StopTranscode(taskID)
	}
}
