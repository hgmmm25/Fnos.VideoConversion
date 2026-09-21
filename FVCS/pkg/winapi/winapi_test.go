package winapi

import (
	"os/exec"
	"testing"
	"time"
)

// TestCreateJobObject_Success 校验 Job 对象可正常创建并释放。
func TestCreateJobObject_Success(t *testing.T) {
	job, err := CreateJobObject()
	if err != nil {
		t.Fatalf("CreateJobObject 失败: %v", err)
	}
	if job == 0 {
		t.Fatalf("CreateJobObject 返回了空句柄")
	}
	if err := CloseJobObject(job); err != nil {
		t.Fatalf("CloseJobObject 失败: %v", err)
	}
}

// TestAssignProcessToJob_InvalidArgs 校验非法入参被显式拒绝（而非静默通过）。
func TestAssignProcessToJob_InvalidArgs(t *testing.T) {
	if err := AssignProcessToJob(0, 1234); err == nil {
		t.Fatalf("jobHandle=0 应当返回错误")
	}

	job, err := CreateJobObject()
	if err != nil {
		t.Fatalf("CreateJobObject 失败: %v", err)
	}
	defer CloseJobObject(job)

	if err := AssignProcessToJob(job, 0); err == nil {
		t.Fatalf("processID=0 应当返回错误")
	}
	if err := AssignProcessToJob(job, 999999); err == nil {
		t.Fatalf("不存在的 pid 应当返回错误")
	}
}

// TestAssignProcessToJob_RealChildProcess 是本次修复的核心回归用例：
// 旧实现把 PID 当进程句柄传给 AssignProcessToJobObject，返回
// ERROR_INVALID_HANDLE（"The handle is invalid."），ffmpeg 子进程从未进入 Job。
// 现要求：分配成功、IsProcessInJob 复核为真、TerminateJob 能连带结束子进程。
func TestAssignProcessToJob_RealChildProcess(t *testing.T) {
	job, err := CreateJobObject()
	if err != nil {
		t.Fatalf("CreateJobObject 失败: %v", err)
	}
	defer CloseJobObject(job)

	// 启动一个存活约 30s 的子进程作为「ffmpeg 替身」
	cmd := exec.Command("cmd", "/c", "ping -n 30 127.0.0.1 > NUL")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动测试子进程失败: %v", err)
	}
	pid := cmd.Process.Pid

	if err := AssignProcessToJob(job, pid); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("AssignProcessToJob(pid=%d) 失败（回归：PID 被当作句柄）: %v", pid, err)
	}

	inJob, err := IsProcessInJob(pid, job)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("IsProcessInJob 失败: %v", err)
	}
	if !inJob {
		_ = cmd.Process.Kill()
		t.Fatalf("子进程未进入目标 Job 对象")
	}

	// Job 内进程应被 TerminateJob 连带结束，可用于证明 Job 管控真实生效
	if err := TerminateJob(job); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("TerminateJob 失败: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("TerminateJob 后子进程仍未退出，Job 管控未生效")
	}
}
