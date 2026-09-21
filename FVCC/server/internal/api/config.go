package api

// P2-1 C轮：根包 Config / gwPrefix 随 router.go 迁入本包；
// NewHandlers / NewRouter 为根包（main.go）装配入口。

import (
	"github.com/gin-gonic/gin"

	"fvcc/internal/remote"
	"fvcc/internal/scheduler"
	"fvcc/internal/security"
	"fvcc/internal/store"
	"fvcc/internal/ws"
)

// gwPrefix 统一网关前缀（原 main.go gwPrefix；router.go 包内引用）。
const gwPrefix = "/app/fvcc"

// GWPrefix 导出网关前缀（main.go 不再使用，保留给外部引用）。
const GWPrefix = gwPrefix

// Config 保存服务运行配置（原 main.go 定义）。
type Config struct {
	SockPath string // Unix Socket 路径 (生产)
	UIDir    string // 前端静态资源目录
	DataDir  string // 数据目录
	DevMode  bool   // 是否本地开发模式
	DevAddr  string // 开发模式 TCP 监听地址
}

// NewHandlers 构造 API 处理器集合（根包装配入口，替代原 main 包直接字面量构造）。
func NewHandlers(store *store.Store, probe MediaProber, pv *security.PathValidator, remote *remote.RemoteClient, hub *ws.Hub, sch *scheduler.Scheduler) *Handlers {
	return &Handlers{store: store, probe: probe, pv: pv, remote: remote, hub: hub, scheduler: sch}
}

// NewRouter 构建路由（原根包 newRouter；导出供 main.go 调用）。
func NewRouter(cfg Config, h *Handlers) *gin.Engine {
	return newRouter(cfg, h)
}
