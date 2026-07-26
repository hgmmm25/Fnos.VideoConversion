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
	r.Use(gatewayUser())

	g := r.Group(gwPrefix)

	// ===== REST API =====
	api := g.Group("/api")
	{
		api.GET("/info", h.info)
		api.GET("/metrics", h.metrics)

		// 设置
		api.GET("/settings", h.getSettings)
		api.PUT("/settings", h.saveSettings)
		api.GET("/log", h.getLog)
		api.DELETE("/log", h.clearLog)
		api.GET("/video-cache", h.getVideoCacheInfo)
		api.DELETE("/video-cache", h.clearVideoCache)

		// 视频管理
		api.POST("/video/scan", h.scanDirectory)
		api.GET("/video/scan-stream", h.scanDirectoryStream)
		api.POST("/video/probe", h.probeVideo)
		api.GET("/video/preview/:path", h.previewVideo)
		api.POST("/video/rename", h.renameVideo)
		api.POST("/video/move", h.moveVideo)
		api.POST("/video/delete", h.deleteVideo)
		api.GET("/dirs", h.browseDirs)

		// 服务器管理
		api.GET("/servers", h.listServers)
		api.POST("/servers", h.createServer)
		api.PUT("/servers/:id", h.updateServer)
		api.DELETE("/servers/:id", h.deleteServer)
		api.POST("/servers/:id/test", h.testServer)

		// 转码方案管理
		api.GET("/profiles", h.listProfiles)
		api.POST("/profiles", h.createProfile)
		api.PUT("/profiles/:id", h.updateProfile)
		api.DELETE("/profiles/:id", h.deleteProfile)

		// 任务管理
		api.GET("/tasks", h.listTasks)
		api.POST("/tasks", h.createTask)
		api.POST("/tasks/reorder", h.reorderTasks)
		api.POST("/tasks/:id/pause", h.pauseTask)
		api.POST("/tasks/:id/resume", h.resumeTask)
		api.POST("/tasks/:id/cancel", h.cancelTask)
		api.POST("/tasks/:id/retry", h.retryTask)
		api.DELETE("/tasks/:id", h.deleteTask)

		// 历史记录
		api.GET("/history", h.listHistory)
		api.DELETE("/history/:id", h.deleteHistory)
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
