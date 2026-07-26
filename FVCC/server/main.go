// Package main 是 FVCC (Fnos Video Conversion Client) 视频转码客户端的常驻后端服务。
//
// 通过统一网关 (gatewayPrefix=/app/fvcc) 暴露 HTTP 与 WebSocket，
// 监听 fnOS 转发到的 Unix Socket (默认 ${TRIM_APPDEST}/app.sock)。
//
// 运行时环境变量 (由 cmd/main 注入):
//
//	FVCC_SOCK     - Unix Socket 路径
//	FVCC_UIDIR    - 前端静态资源目录 (app/ui)
//	FVCC_DATADIR  - 数据/日志目录 (${TRIM_PKGVAR})
//	FVCC_DEV=1    - 本地开发模式，监听 TCP 127.0.0.1:8088 而非 Unix Socket
//	FVCC_ADDR     - 开发模式监听地址 (默认 127.0.0.1:8088)
package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/logger"
)

const (
	appName  = "fvcc"
	appVer   = "1.0.0"
	gwPrefix = "/app/fvcc"
	sockName = "app.sock"
)

// Config 保存服务运行配置。
type Config struct {
	SockPath string // Unix Socket 路径 (生产)
	UIDir    string // 前端静态资源目录
	DataDir  string // 数据目录
	DevMode  bool   // 是否本地开发模式
	DevAddr  string // 开发模式 TCP 监听地址
}

func loadConfig() Config {
	c := Config{
		SockPath: os.Getenv("FVCC_SOCK"),
		UIDir:    os.Getenv("FVCC_UIDIR"),
		DataDir:  os.Getenv("FVCC_DATADIR"),
		DevMode:  os.Getenv("FVCC_DEV") == "1",
		DevAddr:  os.Getenv("FVCC_ADDR"),
	}
	if c.DevAddr == "" {
		c.DevAddr = "127.0.0.1:8088"
	}
	return c
}

// applyDevDefaults 填充开发模式下的默认值，便于直接 `go run . -dev`。
func applyDevDefaults(c *Config) {
	if !c.DevMode {
		return
	}
	if c.UIDir == "" {
		c.UIDir = filepath.Join("..", "app", "ui")
	}
	if c.DataDir == "" {
		c.DataDir = "."
	}
	if c.SockPath == "" {
		c.SockPath = filepath.Join(os.TempDir(), "fvcc.sock")
	}
	c.UIDir = absPath(c.UIDir)
	c.DataDir = absPath(c.DataDir)
}

func absPath(p string) string {
	if p == "" {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// 全局组件
var (
	globalHub    *Hub
	globalStore  *Store
	globalRemote *RemoteClient
)

func main() {
	dev := flag.Bool("dev", false, "本地开发模式：监听 TCP 而非 Unix Socket")
	flag.Parse()

	cfg := loadConfig()
	if *dev {
		cfg.DevMode = true
	}
	applyDevDefaults(&cfg)

	logger.Info("main", "fvcc %s starting (dev=%v, dataDir=%s, uiDir=%s)", appVer, cfg.DevMode, cfg.DataDir, cfg.UIDir)

	// 确保数据目录存在
	if cfg.DataDir != "" {
		if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
			logger.Warn("main", "mkdir data dir %s: %v", cfg.DataDir, err)
		} else {
			logger.Debug("main", "data directory ready: %s", cfg.DataDir)
		}
	}

	// 初始化组件
	store := NewStore(cfg.DataDir)
	if err := store.Load(); err != nil {
		logger.Fatal("main", "load store: %v", err)
	}
	globalStore = store

	// 从设置中加载日志级别
	settings := store.GetSettings()
	var logLevel logger.LogLevel
	switch strings.ToUpper(settings.LogLevel) {
	case "DEBUG":
		logLevel = logger.DEBUG
	case "WARN":
		logLevel = logger.WARN
	case "ERROR":
		logLevel = logger.ERROR
	case "FATAL":
		logLevel = logger.FATAL
	default:
		logLevel = logger.INFO
	}
	logger.SetLogLevel(logLevel)
	logger.Info("main", "log level set to %s", settings.LogLevel)

	probe := NewFFprobe()
	envFile := ""
	if cfg.DataDir != "" {
		envFile = filepath.Join(cfg.DataDir, "accessible_paths.env")
	}
	pv := NewPathValidator(cfg.DevMode, envFile)
	// 从已保存的设置中加载手动配置的授权目录
	pv.SetExtraPaths(store.GetSettings().AccessiblePaths)
	hub := NewHub()
	globalHub = hub
	remote := NewRemoteClient()
	globalRemote = remote

	remote.OnPushProgress = func(serverID, remoteTaskID string, progress float64) {
		if progress <= 0 || progress > 100 {
			return
		}
		tasks := store.GetTasks()
		for _, t := range tasks {
			if t.ServerID == serverID && t.RemoteTaskID == remoteTaskID && !t.Status.IsTerminal() {
				if t.Progress >= progress {
					return
				}
				store.UpdateTaskStatus(t.ID, t.Status, progress, "")
				hub.BroadcastTaskUpdate(t.ID, string(t.Status), progress, "")
				return
			}
		}
	}
	remote.OnDisconnect = func(serverID string) {
		logger.Info("main", "server %s disconnected, marking as offline", serverID)
		store.UpdateServerStatus(serverID, "offline")
	}

	// 启动调度器
	scheduler := NewScheduler(store, remote, hub, pv)
	go scheduler.Start()

	// 构建路由
	h := &Handlers{store: store, probe: probe, pv: pv, remote: remote, hub: hub, scheduler: scheduler}
	gin.SetMode(gin.ReleaseMode)
	router := newRouter(cfg, h)

	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 选择监听器
	ln, err := buildListener(cfg)
	if err != nil {
		logger.Fatal("main", "build listener: %v", err)
	}

	// 优雅退出
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("main", "fvcc %s listening on %s (dev=%v, ffprobe=%v)", appVer, ln.Addr(), cfg.DevMode, probe.Available())
		if err := http.Serve(ln, srv.Handler); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("main", "serve: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Info("main", "shutting down ...")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	hub.CloseAll()
	remote.CloseAll()
	store.ReleaseAllLocks()
	_ = ln.Close()
	if !cfg.DevMode && cfg.SockPath != "" {
		_ = os.Remove(cfg.SockPath)
	}
	logger.Info("main", "stopped.")
}

// buildListener 根据模式创建 Unix Socket 或 TCP 监听器。
func buildListener(cfg Config) (net.Listener, error) {
	if cfg.DevMode {
		return net.Listen("tcp", cfg.DevAddr)
	}
	if cfg.SockPath == "" {
		return nil, errors.New("FVCC_SOCK 未设置")
	}
	if err := os.Remove(cfg.SockPath); err != nil && !os.IsNotExist(err) {
		logger.Warn("main", "remove stale socket %s: %v", cfg.SockPath, err)
	}
	ln, err := net.Listen("unix", cfg.SockPath)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(cfg.SockPath, 0o666)
	return ln, nil
}
