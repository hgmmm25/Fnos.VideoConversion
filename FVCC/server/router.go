package main

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// newRouter 构建所有路由，统一挂在网关前缀 /app/fvcc 下。
func newRouter(cfg Config, h *Handlers) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	// r.Use(requestLogger()) // 禁用访问日志
	// 安全响应头（07 §4.4）优先于业务处理，保证 4xx/5xx 与静态资源同样带全
	r.Use(securityHeaders())
	// 鉴权失败限流（07 §4.5）：应用侧 401/403 达阈值后 429
	r.Use(authFailThrottle(authFailLimiter, h.auditReject("ratelimit.auth")))
	r.Use(gatewayUser())

	g := r.Group(gwPrefix)

	// ===== REST API =====
	api := g.Group("/api")
	{
		api.GET("/info", h.info)
		api.GET("/metrics", h.metrics)
		// P2-1 可观测性：Prometheus 文本格式指标端点（不引入 client_golang，无 CGO 依赖）
		api.GET("/metrics/prometheus", h.metricsPrometheus)

		// 设置
		api.GET("/settings", h.getSettings)
		api.PUT("/settings", h.saveSettings)
		api.GET("/log", h.getLog)
		api.DELETE("/log", requireAdmin(), h.clearLog)
		api.GET("/video-cache", h.getVideoCacheInfo)
		api.DELETE("/video-cache", requireAdmin(), h.clearVideoCache)

		// 视频管理
		api.POST("/video/scan", h.scanDirectory)
		api.GET("/video/scan-stream", h.scanDirectoryStream)
		api.POST("/video/probe", h.probeVideo)
		api.GET("/video/preview/:path", h.previewVideo)
		api.POST("/video/rename", h.renameVideo)
		api.POST("/video/move", h.moveVideo)
		api.POST("/video/delete", h.deleteVideo)

		// P2-5 回收站：删除操作回收站化后的列表/恢复/清空入口
		api.GET("/trash", h.listTrash)
		api.POST("/trash/restore", h.restoreTrash)
		api.POST("/trash/empty", requireAdmin(), h.emptyTrash)
		api.GET("/dirs", h.browseDirs)

		// 服务器管理
		api.GET("/servers", h.listServers)
		api.POST("/servers", h.createServer)
		api.PUT("/servers/:id", h.updateServer)
		api.DELETE("/servers/:id", requireAdmin(), h.deleteServer)
		api.POST("/servers/:id/test", h.testServer)

		// 转码方案管理
		api.GET("/profiles", h.listProfiles)
		api.POST("/profiles", h.createProfile)
		api.PUT("/profiles/:id", h.updateProfile)
		api.DELETE("/profiles/:id", requireAdmin(), h.deleteProfile)

		// 任务管理
		api.GET("/tasks", h.listTasks)
		api.POST("/tasks", h.createTask)
		api.POST("/tasks/reorder", h.reorderTasks)
		api.POST("/tasks/:id/pause", h.pauseTask)
		api.POST("/tasks/:id/resume", h.resumeTask)
		api.POST("/tasks/:id/cancel", h.cancelTask)
		api.POST("/tasks/:id/retry", h.retryTask)
		api.DELETE("/tasks/:id", requireAdmin(), h.deleteTask)

		// 历史记录
		api.GET("/history", h.listHistory)
		api.DELETE("/history/:id", requireAdmin(), h.deleteHistory)

		// EDL 剪辑项目（B-03 / 03 §4.2、§4.3）
		// B-09：删除类与提交渲染挂 requireAdmin（07 §4.2、§7）；项目读写对只读用户开放；/proxy 路由落地时同步挂载
		api.GET("/edl/projects", h.listEDLProjects)
		api.POST("/edl/projects", h.createEDLProject)
		api.GET("/edl/projects/:id", h.getEDLProject)
		api.PUT("/edl/projects/:id", h.updateEDLProject)
		api.DELETE("/edl/projects/:id", requireAdmin(), h.deleteEDLProject)
		// 渲染提交（B-04 / 03 §4.4）；D-04：叠加 30 次/分钟/用户限流（07 §4.5）
		api.POST("/edl/projects/:id/render", requireAdmin(),
			renderSubmitLimit(renderSubmitLimiter, h.auditReject("ratelimit.render")), h.renderEDLProject)

		// 预览网关（M4 / 04 §2、03 §4.5）：
		//   POST   /stream/ticket  申请票据（非一次性，绑定 path+root+来源 IP，300s）
		//   DELETE /stream/ticket  页面卸载时显式失效
		//   GET    /stream         Range 视频流（200/206/416）
		//   HEAD   /stream         同 GET，仅返回头部（04 §2.2 要点 1：GET/HEAD 之外一律 405）
		//   GET    /thumb          JPEG 320×180 抽帧缩略图
		api.POST("/stream/ticket", h.handleStreamTicket)
		api.DELETE("/stream/ticket", h.handleStreamTicketDelete)
		api.GET("/stream", h.handleStream)
		api.HEAD("/stream", h.handleStream)
		api.GET("/thumb", h.handleThumb)

		// 代理工作流（M4 / 04 §3.2）：写操作，与 EDL 删除类一致挂 requireAdmin（07 §4.2）
		api.POST("/proxy", requireAdmin(), h.handleProxyRequest)
	}

	// WebSocket：向前端推送任务状态
	g.GET("/ws", h.hub.HandleWS)

	// ===== 前端静态资源 =====
	serveUI(g, cfg.UIDir)

	// SPA 兜底：网关前缀下未匹配的路径返回 index.html，但拒绝 config 等敏感文件
	if cfg.UIDir != "" {
		indexFile := filepath.Join(cfg.UIDir, "index.html")
		r.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			if !strings.HasPrefix(p, gwPrefix+"/") && p != gwPrefix {
				c.String(http.StatusNotFound, "not found")
				return
			}
			// 禁止访问 config 等敏感文件
			if strings.HasPrefix(p, gwPrefix+"/config") {
				c.String(http.StatusForbidden, "forbidden")
				return
			}
			c.File(indexFile)
		})
	}

	return r
}

// serveUI 提供前端静态文件。
// /app/fvcc/            -> index.html
// /app/fvcc/assets/*    -> ui/assets/*
// /app/fvcc/images/*    -> ui/images/*
func serveUI(g *gin.RouterGroup, uiDir string) {
	if uiDir == "" {
		return
	}
	indexFile := filepath.Join(uiDir, "index.html")
	g.GET("/", func(c *gin.Context) { c.File(indexFile) })
	for _, sub := range []string{"assets", "images"} {
		g.StaticFS("/"+sub, http.Dir(filepath.Join(uiDir, sub)))
	}
}
