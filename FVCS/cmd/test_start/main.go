package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"Fnos.VC_Service/pkg/ipc"
)

func main() {
	exePath, _ := os.Executable()
	dir := filepath.Dir(exePath)
	serviceExe := filepath.Join(dir, "VC_Service.exe")

	fmt.Println("Service path:", serviceExe)

	if _, err := os.Stat(serviceExe); os.IsNotExist(err) {
		fmt.Println("VC_Service.exe not found")
		return
	}

	fmt.Println("Starting service...")
	cmd := exec.Command(serviceExe)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		fmt.Println("Failed to start service:", err)
		return
	}

	fmt.Println("Service started, PID:", cmd.Process.Pid)

	time.Sleep(3 * time.Second)

	if ipc.IsServiceRunning() {
		fmt.Println("Service is running")
	} else {
		fmt.Println("Service is NOT running")
	}

	time.Sleep(5 * time.Second)
	cmd.Process.Kill()
	fmt.Println("Service killed")
}
