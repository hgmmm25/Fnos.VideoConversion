package main

import (
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/winapi"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed FVCS.png
var iconPngData []byte

func main() {
	nullFile, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0644)
	os.Stderr = nullFile

	// 先加载配置并应用日志级别，避免早期 INFO 日志被写入 ERROR 级别的日志文件
	if err := config.Load(); err != nil {
		logger.Error("settings", "Failed to load config: %v", err)
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

	logger.Info("settings", "FVCS Settings starting...")

	myApp := app.NewWithID("Fnos.VC_Service.Settings")

	myApp.SetIcon(fyne.NewStaticResource("icon.png", iconPngData))
	logger.Info("settings", "App icon set from embedded FVCS.png, size=%d", len(iconPngData))

	if winapi.IsDarkMode() {
		myApp.Settings().SetTheme(theme.DarkTheme())
	}

	cfg = config.Get()

	wsPortEntry := widget.NewEntry()
	wsPortEntry.SetText(strconv.Itoa(cfg.WsPort))

	maxTasksEntry := widget.NewEntry()
	maxTasksEntry.SetText(strconv.Itoa(cfg.MaxConcurrentTasks))

	procPriorityOptions := []string{"1 - 低", "2 - 低于正常", "3 - 正常", "4 - 高于正常", "5 - 高"}
	procPrioritySelect := widget.NewSelect(procPriorityOptions, nil)
	// 根据 ProcessPriority 值选择对应项（容错：超出范围时回退到"3 - 正常"）
	priorityIdx := cfg.ProcessPriority - 1
	if priorityIdx < 0 || priorityIdx >= len(procPriorityOptions) {
		priorityIdx = 2
	}
	procPrioritySelect.SetSelected(procPriorityOptions[priorityIdx])

	ffmpegPathEntry := widget.NewEntry()
	ffmpegPathEntry.SetText(cfg.FFmpegPath)

	tempDirEntry := widget.NewEntry()
	tempDirEntry.SetText(cfg.TempDir)

	listenLocalOnlyCheck := widget.NewCheck("仅本地监听", nil)
	listenLocalOnlyCheck.SetChecked(cfg.ListenLocalOnly)

	autoStartCheck := widget.NewCheck("自动启动服务", nil)
	autoStartCheck.SetChecked(cfg.AutoStart)

	autoRunOnBootCheck := widget.NewCheck("开机自启", nil)
	autoRunOnBootCheck.SetChecked(cfg.AutoRunOnBoot)

	logLevelSelect := widget.NewSelect([]string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}, nil)
	logLevelSelect.SetSelected(cfg.LogLevel)

	authKeyLabel := widget.NewLabel("")
	copyBtn := widget.NewButton("复制", func() {})
	copyBtn.Hide()

	window := myApp.NewWindow("FVCS 设置")

	getAuthKeyBtn := widget.NewButton("获取认证密钥", func() {
		authKey := config.GetAuthKey()
		if authKey == "" {
			authKeyLabel.SetText("获取失败: 认证密钥为空")
			copyBtn.Hide()
		} else {
			authKeyLabel.SetText(fmt.Sprintf("认证密钥: %s", authKey))
			copyBtn.Show()
			copyBtn.OnTapped = func() {
				window.Clipboard().SetContent(authKey)
				dialog.ShowInformation("复制成功", "认证密钥已复制到剪贴板", window)
			}
		}
	})

	saveBtn := widget.NewButton("保存", func() {
		wsPort, err := strconv.Atoi(wsPortEntry.Text)
		if err != nil {
			dialog.ShowError(fmt.Errorf("WS端口必须是数字"), window)
			return
		}

		maxTasks, err := strconv.Atoi(maxTasksEntry.Text)
		if err != nil {
			dialog.ShowError(fmt.Errorf("最大并发任务数必须是数字"), window)
			return
		}

		// 从下拉选项中解析优先级数字（格式为 "N - 描述"）
		procPriority := 3
		if selected := procPrioritySelect.Selected; selected != "" {
			if n, err := strconv.Atoi(strings.SplitN(selected, " ", 2)[0]); err == nil {
				procPriority = n
			}
		}

		cfg := config.Get()
		cfg.WsPort = wsPort
		cfg.MaxConcurrentTasks = maxTasks
		cfg.ProcessPriority = procPriority
		cfg.FFmpegPath = ffmpegPathEntry.Text
		cfg.TempDir = tempDirEntry.Text
		cfg.ListenLocalOnly = listenLocalOnlyCheck.Checked
		cfg.AutoStart = autoStartCheck.Checked
		cfg.AutoRunOnBoot = autoRunOnBootCheck.Checked
		cfg.LogLevel = logLevelSelect.Selected

		if err := config.Save(); err != nil {
			dialog.ShowError(fmt.Errorf("保存配置失败: %v", err), window)
			return
		}

		dialog.ShowInformation("保存成功", "配置已保存，需要重启服务生效", window)
	})

	form := widget.NewForm(
		widget.NewFormItem("WebSocket端口", wsPortEntry),
		widget.NewFormItem("最大并发任务数", maxTasksEntry),
		widget.NewFormItem("进程优先级", procPrioritySelect),
		widget.NewFormItem("FFmpeg路径", ffmpegPathEntry),
		widget.NewFormItem("临时目录", tempDirEntry),
		widget.NewFormItem("日志记录级别", logLevelSelect),
	)

	checksBox := container.NewVBox(
		listenLocalOnlyCheck,
		autoStartCheck,
		autoRunOnBootCheck,
	)

	authBox := container.NewVBox(
		getAuthKeyBtn,
		container.NewHBox(authKeyLabel, copyBtn),
	)

	content := container.NewVBox(
		form,
		checksBox,
		widget.NewSeparator(),
		authBox,
		widget.NewSeparator(),
		saveBtn,
	)

	window.SetContent(content)
	window.Resize(fyne.NewSize(520, 420))
	window.ShowAndRun()
}
