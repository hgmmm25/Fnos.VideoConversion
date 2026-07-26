package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	logMutex      sync.RWMutex
	logBuffer     []LogEntry
	maxBufferSize = 1000
	logHooks      []func(entry LogEntry)
	fileWriter    *os.File
	logDir        string
	logMaxSizeMB  int64    = 10
	logKeepDays   int      = 7
	logLevel      LogLevel = INFO
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
	dir := filepath.Join(logDir, today)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	path := filepath.Join(dir, "app.log")

	if _, err := os.Stat(path); err == nil {
		backupPath := filepath.Join(dir, fmt.Sprintf("app_%s.log", time.Now().Format("150405")))
		os.Rename(path, backupPath)
	}

	var err error
	fileWriter, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
}

func startCleanupTimer() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cleanupOldLogs()
		checkLogSize()
	}
}

func cleanupOldLogs() {
	keepDays := logKeepDays
	if keepDays <= 0 {
		keepDays = 7
	}

	cutoff := time.Now().AddDate(0, 0, -keepDays)

	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			date, err := time.Parse("2006-01-02", entry.Name())
			if err == nil && date.Before(cutoff) {
				os.RemoveAll(filepath.Join(logDir, entry.Name()))
			}
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
