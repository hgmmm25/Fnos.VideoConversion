package main

import (
	"os"
	"os/signal"
	"syscall"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/ffmpeg"
	"Fnos.VC_Service/pkg/ipc"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/server"
	"Fnos.VC_Service/pkg/task"
	"Fnos.VC_Service/pkg/version"
	"Fnos.VC_Service/pkg/winapi"
)

const mutexName = "FVCS_Service_Mutex"

func main() {
	if winapi.IsAnotherInstanceRunning(mutexName) {
		return
	}

	if err := config.Load(); err != nil {
		logger.Error("service", "Failed to load config: %v", err)
		return
	}

	cfg := config.Get()
	logLevel := logger.INFO
	switch cfg.LogLevel {
	case "DEBUG":
		logLevel = logger.DEBUG
	case "WARN":
		logLevel = logger.WARN
	case "ERROR":
		logLevel = logger.ERROR
	case "FATAL":
		logLevel = logger.FATAL
	}
	logger.SetLogLevel(logLevel)

	logger.Info("service", "FVCS Service starting... version=%s", version.Version)

	ffmpeg.DetectHardwareAccel()

	if err := ipc.StartIPCServer(); err != nil {
		logger.Error("service", "Failed to start IPC server: %v", err)
		return
	}

	cfg = config.Get()
	if cfg.AutoStart {
		logger.Info("service", "AutoStart enabled, starting task manager and server...")

		if server.IsPortInUse(cfg.WsPort) {
			logger.Error("service", "WebSocket port %d already in use, auto start skipped", cfg.WsPort)
		} else {
			httpPort, err := winapi.FindFreePort(10000, 65535)
			if err != nil {
				logger.Error("service", "Failed to find free HTTP port: %v, auto start skipped", err)
			} else {
				// 凭据档案库（07 §5.3）先于任务加载就绪：任务恢复执行时才可解密挂载
				if err := server.InitCredentialAdmin(); err != nil {
					logger.Error("service", "Failed to init credential store: %v", err)
				}
				if err := task.Init(); err != nil {
					logger.Error("service", "Failed to init task manager: %v", err)
				} else if err := server.Init(cfg.WsPort, httpPort); err != nil {
					logger.Error("service", "Failed to start server: %v", err)
					task.Stop()
				} else {
					logger.Info("service", "Auto start completed, WS:%d, HTTP:%d", cfg.WsPort, httpPort)
				}
			}
		}
	} else {
		logger.Info("service", "AutoStart disabled, waiting for IPC StartService command...")
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	<-sigs

	logger.Info("service", "FVCS Service shutting down...")

	ipc.StopIPCServer()
	server.Stop()
	ffmpeg.StopAll()
	task.Stop()

	logger.Info("service", "FVCS Service stopped")
}
