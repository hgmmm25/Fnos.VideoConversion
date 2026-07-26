package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/ffmpeg"
	"Fnos.VC_Service/pkg/ipc"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/server"
	"Fnos.VC_Service/pkg/task"
	"Fnos.VC_Service/pkg/winapi"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"fyne.io/systray"
)

const mutexName = "FVCS_UI_Mutex"

//go:embed FVCS.png
var iconPngData []byte

//go:embed FVCS.ico
var iconIcoData []byte

type uiAction int

const (
	uiShowTasks uiAction = iota
	uiShowSettings
	uiExit
)

type uiEvent struct {
	action uiAction
	data   interface{}
}

type App struct {
	fyneApp          fyne.App
	mainWindow       fyne.Window
	tasksWindow      fyne.Window
	taskList         *widget.List
	statusLabels     map[string]*widget.Label
	serviceRunning   bool
	tasksVisible     bool
	startItem        *systray.MenuItem
	stopItem         *systray.MenuItem
	tasksItem        *systray.MenuItem
	settingsItem     *systray.MenuItem
	exitItem         *systray.MenuItem
	eventLoopRunning bool
	taskRefreshTimer *time.Ticker
	taskRefreshStop  chan struct{}
	cachedTasks      []*task.Task
	tasksMutex       sync.Mutex
	iconData         []byte
	uiEventChan      chan uiEvent
}

func main() {
	nullFile, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0644)
	os.Stderr = nullFile

	os.Setenv("LIBPNG_SKIP_CRC_CHECK", "1")
	os.Setenv("PNG_NO_SIMD", "1")

	// 先加载配置并应用日志级别，避免早期 INFO 日志被写入 ERROR 级别的日志文件
	if err := config.Load(); err != nil {
		logger.Error("main", "Failed to load config: %v", err)
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

	logger.Info("main", "FVCS UI starting...")

	if winapi.IsAnotherInstanceRunning(mutexName) {
		logger.Info("main", "Another instance already running, exiting")
		winapi.ShowInfoMessage("FVCS已在运行中")
		return
	}

	logger.Info("main", "Mutex acquired, no other instance running")

	logger.Info("main", "Creating Fyne app...")
	myApp := app.NewWithID("Fnos.VC_Service.UI")
	logger.Info("main", "Fyne app created: %v", myApp)

	a := &App{
		fyneApp:        myApp,
		serviceRunning: true,
		statusLabels:   make(map[string]*widget.Label),
		uiEventChan:    make(chan uiEvent, 16),
	}

	myApp.SetIcon(fyne.NewStaticResource("icon.png", iconPngData))
	a.iconData = iconIcoData
	logger.Info("main", "App icon set from embedded FVCS.png (%d bytes), systray icon from FVCS.ico (%d bytes)", len(iconPngData), len(iconIcoData))

	if winapi.IsDarkMode() {
		myApp.Settings().SetTheme(theme.DarkTheme())
		logger.Info("main", "Dark theme applied")
	}

	logger.Info("main", "Initializing service components...")

	logger.Info("main", "Detecting hardware acceleration...")
	ffmpeg.DetectHardwareAccel()

	logger.Info("main", "Initializing task manager...")
	if err := task.Init(); err != nil {
		logger.Error("main", "Failed to init task manager: %v", err)
	}

	httpPort, err := winapi.FindFreePort(10000, 65535)
	if err != nil {
		logger.Error("main", "Failed to find free HTTP port: %v", err)
		httpPort = 10000
	}

	logger.Info("main", "Starting server on WS:%d, HTTP:%d...", cfg.WsPort, httpPort)
	if err := server.Init(cfg.WsPort, httpPort); err != nil {
		logger.Error("main", "Failed to start server: %v", err)
	}

	logger.Info("main", "Service components initialized")

	logger.Info("main", "Creating main window...")
	a.mainWindow = myApp.NewWindow("FVCS")
	logger.Info("main", "Main window created: %v", a.mainWindow)
	a.mainWindow.SetOnClosed(func() {
		logger.Error("main", "!!! MAIN WINDOW CLOSED !!! - This should not happen during normal operation")
		logger.Info("main", "Main window closed, recreating hidden window")
		a.mainWindow = myApp.NewWindow("FVCS")
		a.mainWindow.Hide()
		logger.Info("main", "Hidden window recreated")
	})
	a.mainWindow.Hide()
	logger.Info("main", "Main window hidden")

	logger.Info("main", "Starting goroutines...")
	go a.startStatusRefresh()
	logger.Info("main", "startStatusRefresh goroutine started")

	go a.uiEventHandler()
	logger.Info("main", "uiEventHandler goroutine started")

	logger.Info("main", "Starting systray in goroutine...")
	go func() {
		// 锁定此 goroutine 到专用 OS 线程
		// systray 的 Windows 消息循环（GetMessage/DispatchMessage）必须在创建隐藏窗口的同一个线程上运行
		// 否则 Go 运行时可能将 goroutine 迁移到其他线程，导致消息循环收不到窗口消息，托盘菜单无响应
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		systray.Run(func() {
			logger.Info("systray", "systray.Run ready callback called")
			a.initSystemTrayMenu()
		}, func() {
			logger.Info("systray", "systray.Run exit callback called")
			a.fyneApp.Quit()
		})
	}()

	logger.Info("main", "Starting Fyne app.Run() on main thread...")
	myApp.Run()
	logger.Info("main", "Fyne app.Run() exited")
}

func (a *App) initSystemTrayMenu() {
	logger.Info("systray", "initSystemTrayMenu called")

	systray.SetTitle("FVCS")
	systray.SetTooltip("FVCS视频转换服务")

	if len(a.iconData) > 0 {
		systray.SetIcon(a.iconData)
		logger.Info("systray", "Using preloaded icon, size=%d", len(a.iconData))
	} else {
		logger.Warn("systray", "No icon data available")
	}

	logger.Info("systray", "Adding menu items...")
	a.startItem = systray.AddMenuItem("启动服务", "启动后台转码服务")
	logger.Info("systray", "startItem created: %v", a.startItem)
	a.stopItem = systray.AddMenuItem("停止服务", "停止后台转码服务")
	logger.Info("systray", "stopItem created: %v", a.stopItem)

	systray.AddSeparator()
	a.tasksItem = systray.AddMenuItem("当前任务", "显示当前转码任务")
	logger.Info("systray", "tasksItem created: %v", a.tasksItem)
	a.settingsItem = systray.AddMenuItem("设置", "打开设置页面")
	logger.Info("systray", "settingsItem created: %v", a.settingsItem)
	systray.AddSeparator()
	a.exitItem = systray.AddMenuItem("退出", "退出应用程序")
	logger.Info("systray", "exitItem created: %v", a.exitItem)

	a.refreshSystemTrayMenu()

	logger.Info("systray", "Starting menu event goroutine...")
	go a.handleMenuEvents()

	logger.Info("systray", "initSystemTrayMenu completed")
}

func (a *App) uiEventHandler() {
	logger.Info("uiEventHandler", "UI event handler started")
	for event := range a.uiEventChan {
		logger.Info("uiEventHandler", "Received UI event: %d", event.action)
		switch event.action {
		case uiShowTasks:
			fyne.Do(func() {
				a.showTasksWindow()
			})
		case uiShowSettings:
			// launchSettings不需要在UI线程中执行
			go a.launchSettings()
		case uiExit:
			fyne.Do(func() {
				a.exitApp()
			})
		}
		logger.Info("uiEventHandler", "UI event %d processed", event.action)
	}
	logger.Info("uiEventHandler", "UI event handler exiting")
}

func (a *App) handleMenuEvents() {
	a.eventLoopRunning = true
	defer func() {
		a.eventLoopRunning = false
		if r := recover(); r != nil {
			logger.Error("systray", "Menu event goroutine panic: %v", r)
		}
	}()
	logger.Info("systray", "Menu event goroutine started")
	for {
		select {
		case <-a.startItem.ClickedCh:
			logger.Info("systray", "startItem clicked, serviceRunning=%v", a.serviceRunning)
			go a.startService()
		case <-a.stopItem.ClickedCh:
			logger.Info("systray", "stopItem clicked, serviceRunning=%v", a.serviceRunning)
			go a.stopService()
		case <-a.tasksItem.ClickedCh:
			logger.Info("systray", "tasksItem clicked, tasksWindow=%v", a.tasksWindow != nil)
			// 非阻塞发送，防止 uiEventChan 满时阻塞事件循环导致 systray 菜单无响应
			a.sendUIEvent(uiEvent{action: uiShowTasks})
		case <-a.settingsItem.ClickedCh:
			logger.Info("systray", "settingsItem clicked")
			a.sendUIEvent(uiEvent{action: uiShowSettings})
		case <-a.exitItem.ClickedCh:
			logger.Info("systray", "exitItem clicked")
			a.sendUIEvent(uiEvent{action: uiExit})
		}
	}
}

func (a *App) sendUIEvent(event uiEvent) {
	select {
	case a.uiEventChan <- event:
	default:
		logger.Warn("systray", "uiEventChan full, dropping event %d", event.action)
	}
}

func (a *App) updateSystemTrayMenu() {
	logger.Info("systray", "updateSystemTrayMenu called, serviceRunning=%v", a.serviceRunning)
	a.refreshSystemTrayMenu()
	logger.Info("systray", "updateSystemTrayMenu completed")
}

func (a *App) refreshSystemTrayMenu() {
	if a.startItem != nil && a.stopItem != nil {
		if a.serviceRunning {
			a.startItem.Hide()
			a.stopItem.Show()
		} else {
			a.startItem.Show()
			a.stopItem.Hide()
		}
	}
}

func (a *App) startStatusRefresh() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if a.serviceRunning {
			fyne.Do(func() { a.refreshSystemStatus() })
			if a.tasksVisible && a.tasksWindow != nil {
				fyne.Do(func() { a.refreshTasks() })
			}
		}
	}
}

func (a *App) refreshSystemStatus() {
	cfg := config.Get()

	diskFreeBytes, _ := winapi.GetDiskFreeSpace(".")
	totalMem, availMem, _ := winapi.GetMemoryInfo()

	status := ipc.SystemStatusData{
		WsPort:        cfg.WsPort,
		HttpPort:      task.GetHTTPPort(),
		RunningTasks:  task.GetRunningCount(),
		WaitingTasks:  task.GetWaitingCount(),
		OnlineClients: server.GetOnlineClientCount(),
		DiskFreeMB:    diskFreeBytes / (1024 * 1024),
		MemoryUsageMB: (totalMem - availMem) / (1024 * 1024),
	}

	if label, ok := a.statusLabels["ws_port"]; ok {
		label.SetText(fmt.Sprintf("WS端口: %d", status.WsPort))
	}
	if label, ok := a.statusLabels["running_tasks"]; ok {
		label.SetText(fmt.Sprintf("运行中: %d", status.RunningTasks))
	}
	if label, ok := a.statusLabels["waiting_tasks"]; ok {
		label.SetText(fmt.Sprintf("等待中: %d", status.WaitingTasks))
	}
	if label, ok := a.statusLabels["online_clients"]; ok {
		label.SetText(fmt.Sprintf("在线客户端: %d", status.OnlineClients))
	}
	if label, ok := a.statusLabels["disk_free"]; ok {
		label.SetText(fmt.Sprintf("磁盘可用: %.2f GB", float64(status.DiskFreeMB)/1024))
	}
}

func (a *App) refreshTasks() {
	a.tasksMutex.Lock()
	a.cachedTasks = task.GetActiveTasks()
	a.tasksMutex.Unlock()
	if a.taskList != nil {
		a.taskList.Refresh()
	}
}

func (a *App) launchSettings() {
	exePath, err := os.Executable()
	if err != nil {
		logger.Error("main", "Failed to get executable path: %v", err)
		return
	}
	exeDir := filepath.Dir(exePath)
	settingsExe := filepath.Join(exeDir, "FVCS_Settings.exe")

	logger.Info("main", "Launching settings: %s", settingsExe)
	err = winapi.StartProcess(settingsExe)
	if err != nil {
		logger.Error("main", "Failed to launch settings: %v", err)
		winapi.ShowErrorMessage("启动设置程序失败: " + err.Error())
	}
}

func (a *App) startService() {
	if a.serviceRunning {
		return
	}

	logger.Info("main", "Restarting service components...")

	cfg := config.Get()
	httpPort, err := winapi.FindFreePort(10000, 65535)
	if err != nil {
		logger.Error("main", "Failed to find free HTTP port: %v", err)
		httpPort = 10000
	}

	if err := task.Init(); err != nil {
		winapi.ShowErrorMessage("初始化任务管理器失败: " + err.Error())
		return
	}

	if err := server.Init(cfg.WsPort, httpPort); err != nil {
		winapi.ShowErrorMessage("启动服务器失败: " + err.Error())
		return
	}

	a.serviceRunning = true
	a.refreshSystemTrayMenu()
	logger.Info("main", "Service restarted")
	winapi.ShowInfoMessage("服务启动成功")
}

func (a *App) stopService() {
	logger.Info("main", "Stopping service components...")

	server.Stop()
	task.Stop()

	a.serviceRunning = false
	a.refreshSystemTrayMenu()
	logger.Info("main", "Service stopped")
	winapi.ShowInfoMessage("服务停止成功")
}

func (a *App) showTasksWindow() {
	logger.Info("showTasksWindow", "Entering showTasksWindow, tasksWindow=%v", a.tasksWindow != nil)
	if a.tasksWindow != nil {
		logger.Info("showTasksWindow", "tasksWindow already exists, showing...")
		a.tasksWindow.Show()
		a.tasksWindow.RequestFocus()
		logger.Info("showTasksWindow", "tasksWindow shown and focused")
		return
	}

	logger.Info("showTasksWindow", "Creating new tasks window...")
	a.tasksWindow = a.fyneApp.NewWindow("当前任务")
	logger.Info("showTasksWindow", "New tasks window created: %v", a.tasksWindow)

	logger.Info("showTasksWindow", "Creating taskRefreshStop channel...")
	a.taskRefreshStop = make(chan struct{})
	a.tasksVisible = true

	logger.Info("showTasksWindow", "Creating task refresh timer (1s)...")
	a.taskRefreshTimer = time.NewTicker(1 * time.Second)
	go func() {
		logger.Info("showTasksWindow", "Task refresh goroutine started")
		defer logger.Info("showTasksWindow", "Task refresh goroutine exiting")
		for {
			select {
			case <-a.taskRefreshTimer.C:
				// logger.Debug("showTasksWindow", "Task refresh timer tick")
				a.tasksMutex.Lock()
				a.cachedTasks = task.GetActiveTasks()
				a.tasksMutex.Unlock()
				fyne.Do(func() {
					a.taskList.Refresh()
					a.refreshSystemStatus()
				})
			case <-a.taskRefreshStop:
				logger.Info("showTasksWindow", "Task refresh stop signal received")
				return
			}
		}
	}()

	logger.Info("showTasksWindow", "Setting onClosed callback...")
	a.tasksWindow.SetOnClosed(func() {
		logger.Info("showTasksWindow", "tasksWindow closed callback called")
		close(a.taskRefreshStop)
		if a.taskRefreshTimer != nil {
			a.taskRefreshTimer.Stop()
			a.taskRefreshTimer = nil
		}
		a.tasksWindow = nil
		a.tasksVisible = false
		logger.Info("showTasksWindow", "tasksWindow closed cleanup completed")
	})

	a.tasksWindow.Resize(fyne.NewSize(600, 450))

	logger.Info("showTasksWindow", "Creating taskList widget...")
	a.taskList = widget.NewList(
		func() int {
			a.tasksMutex.Lock()
			defer a.tasksMutex.Unlock()
			return len(a.cachedTasks)
		},
		func() fyne.CanvasObject {
			nameLabel := widget.NewLabel("")
			nameLabel.TextStyle = fyne.TextStyle{Bold: true}

			statusLabel := widget.NewLabel("")
			stopBtn := widget.NewButton("停止", func() {})
			stopBtn.Importance = widget.DangerImportance

			topRow := container.NewHBox(
				nameLabel,
				layout.NewSpacer(),
				statusLabel,
				stopBtn,
			)

			progressBar := widget.NewProgressBar()

			meta1 := widget.NewLabel("")
			meta1.TextStyle = fyne.TextStyle{Italic: true}
			meta2 := widget.NewLabel("")
			meta2.TextStyle = fyne.TextStyle{Italic: true}
			meta2.Alignment = fyne.TextAlignTrailing

			bottomRow := container.NewHBox(
				meta1,
				layout.NewSpacer(),
				meta2,
			)

			card := container.NewVBox(
				topRow,
				progressBar,
				bottomRow,
			)
			return card
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			a.tasksMutex.Lock()
			defer a.tasksMutex.Unlock()
			if id >= len(a.cachedTasks) {
				return
			}
			t := a.cachedTasks[id]
			card := item.(*fyne.Container).Objects
			if len(card) < 3 {
				return
			}

			topRow, _ := card[0].(*fyne.Container)
			progressBar, _ := card[1].(*widget.ProgressBar)
			bottomRow, _ := card[2].(*fyne.Container)
			if topRow == nil || progressBar == nil || bottomRow == nil {
				return
			}

			topItems := topRow.Objects
			bottomItems := bottomRow.Objects

			if nameLabel, ok := topItems[0].(*widget.Label); ok {
				displayName := t.SourceFileName
				if displayName != "" {
					displayName = filepath.Base(displayName)
				} else {
					displayName = t.TaskID
				}
				nameLabel.SetText(displayName)
			}

			if statusLabel, ok := topItems[2].(*widget.Label); ok {
				statusLabel.SetText(formatStatusWithProgress(t))
			}

			if btn, ok := topItems[3].(*widget.Button); ok {
				taskID := t.TaskID
				status := t.Status
				canCancel := status == task.StatusWaiting ||
					status == task.StatusTranscoding ||
					status == task.StatusUploading ||
					status == task.StatusCreated
				btn.Disable()
				if canCancel {
					btn.Enable()
				}
				btn.OnTapped = func() {
					if canCancel {
						task.CancelTask(taskID)
						fyne.Do(func() { a.taskList.Refresh() })
					}
				}
			}

			progressBar.SetValue(t.Progress / 100)

			if meta1, ok := bottomItems[0].(*widget.Label); ok {
				createdStr := t.CreatedAt.Format("01-02 15:04")
				smbMark := ""
				if t.IsSMBMode {
					smbMark = " [SMB]"
				}
				meta1.SetText(fmt.Sprintf("创建: %s%s", createdStr, smbMark))
			}
			if meta2, ok := bottomItems[2].(*widget.Label); ok {
				extra := ""
				if t.Resolution != "" {
					extra = t.Resolution
				}
				if t.Bitrate != "" {
					if extra != "" {
						extra += " · "
					}
					extra += t.Bitrate
				}
				if t.TotalChunks > 0 && (t.Status == task.StatusCreated || t.Status == task.StatusUploading) {
					received := len(t.ReceivedChunks)
					if extra != "" {
						extra += " · "
					}
					extra += fmt.Sprintf("分片 %d/%d", received, t.TotalChunks)
				}
				meta2.SetText(extra)
			}
		},
	)
	logger.Info("showTasksWindow", "taskList widget created")

	logger.Info("showTasksWindow", "Creating status labels...")
	runningLabel := widget.NewLabel("运行中: 0")
	waitingLabel := widget.NewLabel("等待中: 0")
	wsPortLabel := widget.NewLabel("WS端口: -")
	diskFreeLabel := widget.NewLabel("磁盘可用: -")

	a.statusLabels["running_tasks"] = runningLabel
	a.statusLabels["waiting_tasks"] = waitingLabel
	a.statusLabels["ws_port"] = wsPortLabel
	a.statusLabels["disk_free"] = diskFreeLabel

	logger.Info("showTasksWindow", "Creating status bar...")
	statusBar := container.NewHBox(
		runningLabel,
		layout.NewSpacer(),
		waitingLabel,
		layout.NewSpacer(),
		wsPortLabel,
		layout.NewSpacer(),
		diskFreeLabel,
	)

	logger.Info("showTasksWindow", "Creating content container...")
	content := container.NewBorder(
		widget.NewToolbar(
			widget.NewToolbarAction(theme.ViewRefreshIcon(), func() { a.refreshTasks() }),
			widget.NewToolbarSeparator(),
			widget.NewToolbarAction(theme.CancelIcon(), func() { a.clearAllTasks() }),
		),
		statusBar,
		nil,
		nil,
		a.taskList,
	)

	logger.Info("showTasksWindow", "Setting window content...")
	a.tasksWindow.SetContent(content)
	logger.Info("showTasksWindow", "Showing window...")
	a.tasksWindow.Show()
	logger.Info("showTasksWindow", "Window shown")

	logger.Info("showTasksWindow", "Initial refresh tasks...")
	fyne.Do(func() { a.refreshTasks() })
	logger.Info("showTasksWindow", "Initial refresh system status...")
	fyne.Do(func() { a.refreshSystemStatus() })
	logger.Info("showTasksWindow", "showTasksWindow completed")
}

func formatStatusWithProgress(t *task.Task) string {
	var zhStatus string
	switch t.Status {
	case task.StatusCreated:
		zhStatus = "已创建"
	case task.StatusUploading:
		zhStatus = "上传中"
	case task.StatusWaiting:
		zhStatus = "等待中"
	case task.StatusTranscoding:
		zhStatus = "转码中"
	case task.StatusSuccess:
		zhStatus = "已完成"
	case task.StatusFailed:
		zhStatus = "失败"
	case task.StatusCancelled:
		zhStatus = "已取消"
	case task.StatusClientDisconnect:
		zhStatus = "客户端离线"
	default:
		zhStatus = string(t.Status)
	}

	switch t.Status {
	case task.StatusTranscoding, task.StatusUploading:
		return fmt.Sprintf("%s · %.1f%%", zhStatus, t.Progress)
	case task.StatusSuccess:
		return "已完成 ✓"
	case task.StatusFailed:
		return "失败 ✗"
	case task.StatusCancelled:
		return "已取消 ✗"
	case task.StatusClientDisconnect:
		return "客户端离线 ✗"
	default:
		return zhStatus
	}
}

func (a *App) clearAllTasks() {
	task.ClearAllTasks()
	fyne.Do(func() { a.refreshTasks() })
}

func (a *App) exitApp() {
	a.stopService()

	if a.tasksWindow != nil {
		a.tasksWindow.Close()
	}
	if a.mainWindow != nil {
		a.mainWindow.Close()
	}

	a.fyneApp.Quit()
}
