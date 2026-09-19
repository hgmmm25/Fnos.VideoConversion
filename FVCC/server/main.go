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

	"fvcc/internal/api"
	"fvcc/internal/media"
	remotepkg "fvcc/internal/remote"
	"fvcc/internal/scheduler"
	"fvcc/internal/security"
	"fvcc/internal/store"
	"fvcc/internal/store/model"
	"fvcc/internal/version"
	"fvcc/internal/ws"
	"fvcc/logger"
)

const (
	appName  = "fvcc"
	sockName = "app.sock"
)

// AppVer 由 internal/version 从 VERSION 文件注入（go:embed），勿在此另写版本字面量。

func loadConfig() api.Config {
	c := api.Config{
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
func applyDevDefaults(c *api.Config) {
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
	globalHub    *ws.Hub
	globalStore  *store.Store
	globalRemote *remotepkg.RemoteClient
)

func main() {
	dev := flag.Bool("dev", false, "本地开发模式：监听 TCP 而非 Unix Socket")
	flag.Parse()

	cfg := loadConfig()
	if *dev {
		cfg.DevMode = true
	}
	applyDevDefaults(&cfg)

	logger.Info("main", "fvcc %s starting (dev=%v, dataDir=%s, uiDir=%s)", version.AppVer, cfg.DevMode, cfg.DataDir, cfg.UIDir)

	// 确保数据目录存在
	if cfg.DataDir != "" {
		if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
			logger.Warn("main", "mkdir data dir %s: %v", cfg.DataDir, err)
		} else {
			logger.Debug("main", "data directory ready: %s", cfg.DataDir)
		}
	}

	// 初始化组件
	store := store.NewStore(cfg.DataDir)
	if err := store.Load(); err != nil {
		logger.Fatal("main", "load store: %v", err)
	}
	globalStore = store

	// P2-1 阶段 A：审计落库回调注入（internal/security 经 AuditSink 记账，nil 时仅降级日志）
	security.SetAuditSink(store.AppendAudit)

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

	probe := media.NewFFprobe()
	envFile := ""
	if cfg.DataDir != "" {
		envFile = filepath.Join(cfg.DataDir, "accessible_paths.env")
	}
	pv := security.NewPathValidator(cfg.DevMode, envFile)
	// 从已保存的设置中加载手动配置的授权目录
	pv.SetExtraPaths(store.GetSettings().AccessiblePaths)
	hub := ws.NewHub()
	globalHub = hub
	// P2-1 阶段 B：WS 快照注入（internal/ws 的 sendTaskSnapshot 经此取任务列表）。
	ws.TaskSnapshotProvider = globalStore.GetTasks
	remote := remotepkg.NewRemoteClient()
	globalRemote = remote

	// 启动调度器
	scheduler := scheduler.NewScheduler(store, remote, hub, pv)
	// B-06 装配：渲染下发通道（remote.go 实现 RenderDispatcher）+ 共享根解析所需的设置读取器
	remote.SetSettingsProvider(store.GetSettings)
	scheduler.SetRenderDispatcher(remote)

	// B-07 装配：FVCS 经 WS 回传的进度统一交给调度侧（反查任务 → 落库 stage/seg → 500ms 聚合广播）
	remote.OnPushProgress = func(serverID string, p remotepkg.RemoteProgress) {
		scheduler.HandleRemoteProgress(serverID, p)
	}
	remote.OnDisconnect = func(serverID string) {
		logger.Info("main", "server %s disconnected, marking as offline", serverID)
		store.UpdateServerStatus(serverID, "offline")
		// B-08：按真实健康分广播（离线罚分 −20 已计入，06 §5.2）
		scheduler.BroadcastNodeStatus(serverID, "offline", scheduler.HealthScoreOf(serverID), "WS 连接断开")
	}
	// B-08 装配：节点 Hello 能力上报 → node_caps 落库 + node_status 广播（06 §5.1）
	remote.OnPushHello = func(serverID string, caps model.NodeCaps) {
		scheduler.ApplyNodeHello(caps, time.Now())
	}
	go scheduler.Start()

	// 构建路由
	h := api.NewHandlers(store, probe, pv, remote, hub, scheduler)
	gin.SetMode(gin.ReleaseMode)
	router := api.NewRouter(cfg, h)

	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 选择监听器
	ln, err := buildListener(cfg)
	if err != nil {
		logger.Fatal("main", "build listener: %v", err)
	}
	// P1-3 数据面安全加固：开发模式若监听非回环地址（0.0.0.0 或局域网网卡），
	// 输出风险告警。生产环境（fnOS Unix Socket 网关）不应直接暴露 TCP；若确需
	// 远程调试，仅绑定内网网卡并配合防火墙，详见 docs/SECURITY.md。
	if cfg.DevMode && !isLoopbackAddr(cfg.DevAddr) {
		logger.Warn("main", "DEV 模式监听非回环地址 %s：公网/不可信网络下禁止此配置（见 docs/SECURITY.md）", cfg.DevAddr)
	}

	// 优雅退出
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("main", "fvcc %s listening on %s (dev=%v, ffprobe=%v)", version.AppVer, ln.Addr(), cfg.DevMode, probe.Available())
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

// isLoopbackAddr 判断监听地址是否仅绑定回环（127.0.0.1 / ::1 / localhost）。
// P1-3 安全加固：仅回环地址视为安全默认，其余（0.0.0.0、内网 IP 等）触发告警。
func isLoopbackAddr(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	// 非 IP 字面量（如主机名）无法静态判定，保守视为非回环
	return false
}

// buildListener 根据模式创建 Unix Socket 或 TCP 监听器。
func buildListener(cfg api.Config) (net.Listener, error) {
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
