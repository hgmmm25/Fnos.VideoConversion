package smb

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"Fnos.VC_Service/pkg/logger"
)

func MountSMBShare(smbPath, user, password string) (string, error) {
	logger.Info("smb", "Mounting SMB share: %s", smbPath)

	smbPath = strings.TrimRight(smbPath, `\/`)

	var cmdArgs []string
	if user != "" && password != "" {
		cmdArgs = []string{
			"use", smbPath, "/user:" + user, password, "/persistent:no",
		}
	} else if user != "" {
		cmdArgs = []string{
			"use", smbPath, "/user:" + user, "/persistent:no",
		}
	} else {
		cmdArgs = []string{
			"use", smbPath, "/persistent:no",
		}
	}

	cmd := exec.Command("net", cmdArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(output)
		if strings.Contains(outStr, "already been connected") || strings.Contains(outStr, "多重连接到同一服务器或共享") || strings.Contains(outStr, "Local device name") {
			logger.Info("smb", "SMB share already connected: %s", smbPath)
			return smbPath + `\`, nil
		}
		logger.Error("smb", "Mount SMB failed: %s, output: %s", err, outStr)
		return "", fmt.Errorf("挂载SMB失败: %w, output: %s", err, outStr)
	}

	logger.Info("smb", "SMB share connected successfully: %s (UNC mode, no drive letter assigned)", smbPath)
	return smbPath + `\`, nil
}

func UnmountSMBShare(mountPath string) error {
	logger.Info("smb", "Unmounting SMB share: %s", mountPath)

	smbPath := strings.TrimRight(mountPath, `\/`)

	if len(smbPath) == 2 && smbPath[1] == ':' {
		drive := strings.Split(smbPath, ":")[0]
		cmd := exec.Command("net", "use", drive+":", "/delete", "/yes")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.Warn("smb", "Unmount SMB drive warning: %s, output: %s", err, string(output))
		}
		logger.Info("smb", "SMB drive unmounted: %s:", drive)
		return nil
	}

	cmd := exec.Command("net", "use", smbPath, "/delete", "/yes")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warn("smb", "Unmount SMB UNC warning: %s, output: %s", err, string(output))
	}

	logger.Info("smb", "SMB UNC share unmounted: %s", smbPath)
	return nil
}

func BuildSMBPath(smbBasePath, fileName string) string {
	normalizedName := filepath.FromSlash(fileName)
	if strings.HasPrefix(smbBasePath, `\\`) {
		return strings.TrimRight(smbBasePath, `\`) + `\` + strings.TrimLeft(normalizedName, `\`)
	}
	return filepath.Join(smbBasePath, normalizedName)
}
