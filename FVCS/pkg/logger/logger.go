package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

var levelNames = []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
var levelColors = []string{"\033[90m", "\033[37m", "\033[33m", "\033[31m", "\033[35m"}

type LogEntry struct {
	Time    time.Time
	Level   LogLevel
	Module  string
	Message string
}

var (
	logMutex       sync.RWMutex
	logBuffer      []LogEntry
	maxBufferSize  = 1000
	logHooks       []func(entry LogEntry)
	fileWriter     *os.File
	logDir         string
	currentLogPath string
	currentLogDate string
	maxLogFiles             = 10
	logMaxSizeMB   int64    = 10
	logKeepDays    int      = 7
	logLevel       LogLevel = INFO
)

func init() {
	logDir = filepath.Join(getAppDir(), "logs")
	os.MkdirAll(logDir, 0755)
	rotateLogFile()
	go startCleanupTimer()
}

func getAppDir() string {
	exePath, err := os.Executable()
	if err != nil {
		dir, err := os.Getwd()
		if err != nil {
			return "."
		}
		return dir
	}
	return filepath.Dir(exePath)
}

func SetLogLevel(level LogLevel) {
	logMutex.Lock()
	defer logMutex.Unlock()
	logLevel = level
}

func GetLogLevel() LogLevel {
	logMutex.RLock()
	defer logMutex.RUnlock()
	return logLevel
}

func SetLogConfig(maxSizeMB int64, keepDays int) {
	logMutex.Lock()
	defer logMutex.Unlock()
	logMaxSizeMB = maxSizeMB
	logKeepDays = keepDays
}

func rotateLogFile() {
	logMutex.Lock()
	defer logMutex.Unlock()
	rotateLogFileInternal()
}

func rotateLogFileInternal() {
	if fileWriter != nil {
		fileWriter.Close()
		fileWriter = nil
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(logDir, fmt.Sprintf("app_%s.log", today))

	// 如果今天的文件已存在且过大，先备份为带时间戳的文件
	if info, err := os.Stat(path); err == nil {
		maxSizeBytes := logMaxSizeMB * 1024 * 1024
		if maxSizeBytes > 0 && info.Size() >= maxSizeBytes {
			backupPath := filepath.Join(logDir, fmt.Sprintf("app_%s_%s.log", today, time.Now().Format("150405")))
			os.Rename(path, backupPath)
		}
	}

	var err error
	fileWriter, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	currentLogPath = path
	currentLogDate = today

	// 创建新 log 文件时，清理超出数量的旧文件
	cleanupExcessLogs()
}

func startCleanupTimer() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		checkDateChange()
		checkLogSize()
	}
}

// checkDateChange 检查是否跨天，如果跨天则切换到新日期的 log 文件
func checkDateChange() {
	today := time.Now().Format("2006-01-02")
	logMutex.RLock()
	dateChanged := currentLogDate != today
	logMutex.RUnlock()

	if dateChanged {
		rotateLogFile()
	}
}

// cleanupExcessLogs 清理 logs 目录下超过 maxLogFiles 数量的旧 log 文件
// 按文件名降序排序（最新日期在前），保留最新的 maxLogFiles 个，删除其余
func cleanupExcessLogs() {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}

	type logFile struct {
		name string
		path string
	}
	var logs []logFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// 仅处理 app_*.log 文件
		if !strings.HasPrefix(name, "app_") || !strings.HasSuffix(name, ".log") {
			continue
		}
		logs = append(logs, logFile{name: name, path: filepath.Join(logDir, name)})
	}

	// 按文件名降序排序（文件名含日期+可选时间戳，最新的在前）
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].name > logs[j].name
	})

	// 保留最新的 maxLogFiles 个，删除其余
	if len(logs) > maxLogFiles {
		for i := maxLogFiles; i < len(logs); i++ {
			os.Remove(logs[i].path)
		}
	}
}

func checkLogSize() {
	logMutex.RLock()
	if fileWriter == nil {
		logMutex.RUnlock()
		return
	}

	fileInfo, err := fileWriter.Stat()
	logMutex.RUnlock()

	if err != nil {
		return
	}

	maxSizeBytes := logMaxSizeMB * 1024 * 1024
	if maxSizeBytes <= 0 {
		maxSizeBytes = 10 * 1024 * 1024
	}

	if fileInfo.Size() >= maxSizeBytes {
		rotateLogFile()
	}
}

func AddUILogHook(hook func(entry LogEntry)) {
	logMutex.Lock()
	defer logMutex.Unlock()
	logHooks = append(logHooks, hook)
}

func Debug(module, format string, args ...interface{}) {
	log(DEBUG, module, fmt.Sprintf(format, args...))
}

func Info(module, format string, args ...interface{}) {
	log(INFO, module, fmt.Sprintf(format, args...))
}

func Warn(module, format string, args ...interface{}) {
	log(WARN, module, fmt.Sprintf(format, args...))
}

func Error(module, format string, args ...interface{}) {
	log(ERROR, module, fmt.Sprintf(format, args...))
}

func Fatal(module, format string, args ...interface{}) {
	log(FATAL, module, fmt.Sprintf(format, args...))
	os.Exit(1)
}

func log(level LogLevel, module, message string) {
	logMutex.RLock()
	currentLevel := logLevel
	logMutex.RUnlock()

	if level < currentLevel {
		return
	}

	message = sanitize(message)

	entry := LogEntry{
		Time:    time.Now(),
		Level:   level,
		Module:  module,
		Message: message,
	}

	logMutex.Lock()
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxBufferSize {
		logBuffer = logBuffer[len(logBuffer)-maxBufferSize:]
	}

	if fileWriter == nil {
		logMutex.Unlock()
		rotateLogFileInternal()
		logMutex.Lock()
	}

	if fileWriter != nil {
		logLine := fmt.Sprintf("[%s] [%s] [%s] %s\n",
			entry.Time.Format("2006-01-02 15:04:05"),
			levelNames[level],
			module,
			message)
		if _, err := fileWriter.WriteString(logLine); err != nil {
			fileWriter.Close()
			fileWriter = nil
		} else {
			fileWriter.Sync()
		}
	}

	for _, hook := range logHooks {
		go hook(entry)
	}

	logMutex.Unlock()

	consoleLine := fmt.Sprintf("%s[%s] [%s] [%s] %s\033[0m\n",
		levelColors[level],
		entry.Time.Format("2006-01-02 15:04:05"),
		levelNames[level],
		module,
		message)
	fmt.Print(consoleLine)

	if level == FATAL {
		os.Exit(1)
	}
}

func sanitize(message string) string {
	return message
}

func GetRecentLogs(count int) []LogEntry {
	logMutex.RLock()
	defer logMutex.RUnlock()

	start := 0
	if len(logBuffer) > count {
		start = len(logBuffer) - count
	}

	return logBuffer[start:]
}

func ExportLogs(w io.Writer) error {
	logMutex.RLock()
	defer logMutex.RUnlock()

	for _, entry := range logBuffer {
		line := fmt.Sprintf("[%s] [%s] [%s] %s\n",
			entry.Time.Format("2006-01-02 15:04:05"),
			levelNames[entry.Level],
			entry.Module,
			entry.Message)
		if _, err := w.Write([]byte(line)); err != nil {
			return err
		}
	}

	return nil
}

func ClearLogs() {
	logMutex.Lock()
	defer logMutex.Unlock()
	logBuffer = []LogEntry{}
}
