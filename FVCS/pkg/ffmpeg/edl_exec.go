package ffmpeg

// RenderEDL 计划执行器（设计文档 05 §4.2~§4.5、§6）
//
// 职责：
//  1. 按 RenderPlan.Stages 顺序执行（prepare → segment × N → concat → mux → finalize）；
//  2. 逐阶段解析 ffmpeg `-progress pipe:1` 输出，按 05 §6.1 权重换算整体进度；
//  3. 复用既有 processes 注册表，使 StopTranscode(taskID) 可中断渲染；
//  4. 完成 finalize 落盘（中间产物 → 成品路径）。

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/winapi"
)

// StageRunInfo 阶段进度上报
type StageRunInfo struct {
	Stage      string
	Index      int // segment 阶段的段序号（1-based）
	Total      int // segment 阶段的段总数
	SegDone    int // 已完成片段数（1-based 计数；含本次续跑跳过的片段），供断点续跑落库
	StagePct   float64
	OverallPct float64
}

// ErrRenderStopped 渲染被主动停止（StopTranscode / 任务取消）
var ErrRenderStopped = errors.New("render stopped by user")

// RunRenderPlan 执行渲染计划（无续跑）；onProgress 可为 nil
func RunRenderPlan(taskID string, plan *RenderPlan, onProgress func(StageRunInfo)) error {
	return RunRenderPlanResume(taskID, plan, 0, onProgress)
}

// RunRenderPlanResume 执行渲染计划，支持从断点片段续跑（06 §3.3 / §3.4）。
//
// resumeSegDone > 0 时，先校验 workDir 内前 resumeSegDone 个片段产物是否完整
// （存在且非空），校验通过的前缀直接跳过、不重复渲染；首个不完整的片段及其
// 之后的残留产物会被清理，从该段起重跑。传 0 时与 RunRenderPlan 等价。
func RunRenderPlanResume(taskID string, plan *RenderPlan, resumeSegDone int, onProgress func(StageRunInfo)) error {
	if plan == nil || len(plan.Stages) == 0 {
		return fmt.Errorf("渲染计划为空")
	}
	cfg := config.Get()
	if !isFFmpegAvailable(cfg.FFmpegPath) {
		return fmt.Errorf("%s: ffmpeg not found at %s", "E_FFMPEG_MISSING", cfg.FFmpegPath)
	}

	totalWeight := 0.0
	for _, s := range plan.Stages {
		totalWeight += s.WeightPct
	}
	if totalWeight <= 0 {
		totalWeight = 100
	}

	segTotal := 0
	for _, s := range plan.Stages {
		if s.Name == StageSegment {
			segTotal++
		}
	}

	done := 0.0
	segIdx := 0
	segDone := 0

	report := func(stage string, idx int, frac float64, weight float64) {
		if onProgress == nil {
			return
		}
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		onProgress(StageRunInfo{
			Stage:      stage,
			Index:      idx,
			Total:      segTotal,
			SegDone:    segDone,
			StagePct:   frac * 100,
			OverallPct: (done + frac*weight) / totalWeight * 100,
		})
	}

	// 断点续跑：校验 workDir 内已完成片段前缀，确定可跳过的段数（06 §3.3 优化项 1）
	skipSeg := ResumableSegmentPrefix(plan, resumeSegDone)
	if skipSeg > 0 {
		logger.Info("render", "task=%s resume: 复用 workDir=%s 已完成片段 %d/%d，从第 %d 段续跑",
			taskID, plan.WorkDir, skipSeg, segTotal, skipSeg+1)
	}

	for _, st := range plan.Stages {
		switch st.Name {
		case StagePrepare:
			if err := prepareWorkspace(plan); err != nil {
				return err
			}
			logger.Info("render", "task=%s mode=%s prepare done, workDir=%s, estBytes=%d",
				taskID, plan.Mode, plan.WorkDir, plan.EstOutputBytes)
			report(st.Name, 0, 1, st.WeightPct)

		case StageSegment:
			segIdx++
			if segIdx <= skipSeg {
				// 该段产物已在 workDir 内确认完整，直接跳过
				segDone = segIdx
				logger.Info("render", "task=%s resume: 跳过片段 %d/%d (%s)", taskID, segIdx, segTotal, st.Output)
				report(st.Name, segIdx, 1, st.WeightPct)
				done += st.WeightPct
				continue
			}
			err := runStage(taskID, cfg.FFmpegPath, st.Args, st.ExpectedMs, func(frac float64) {
				report(st.Name, segIdx, frac, st.WeightPct)
			})
			if err != nil {
				return fmt.Errorf("片段 %d 渲染失败: %w", segIdx, err)
			}
			if _, serr := os.Stat(st.Output); serr != nil {
				return fmt.Errorf("片段 %d 输出缺失: %s", segIdx, st.Output)
			}
			segDone = segIdx
			report(st.Name, segIdx, 1, st.WeightPct)

		case StageConcat, StageMux:
			err := runStage(taskID, cfg.FFmpegPath, st.Args, st.ExpectedMs, func(frac float64) {
				report(st.Name, 0, frac, st.WeightPct)
			})
			if err != nil {
				return fmt.Errorf("%s 阶段失败: %w", st.Name, err)
			}
			if _, serr := os.Stat(st.Output); serr != nil {
				return fmt.Errorf("%s 阶段输出缺失: %s", st.Name, st.Output)
			}

		case StageFinalize:
			if err := finalizeOutput(plan, func(frac float64) {
				report(st.Name, 0, frac, st.WeightPct)
			}); err != nil {
				return fmt.Errorf("成品落盘失败: %w", err)
			}
			logger.Info("render", "task=%s finalize done, output=%s", taskID, plan.OutputPath)

		default:
			// 未知阶段：直接跳过（向前兼容）
			logger.Warn("render", "task=%s 跳过未知阶段: %s", taskID, st.Name)
		}
		done += st.WeightPct
	}
	return nil
}

// ResumableSegmentPrefix 计算 workDir 内可安全复用的片段前缀长度（06 §3.3 优化项 1）。
//
// want 为任务表记录的已完成段数（SegDone）。逐个校验 plan 中前 want 个 segment
// 阶段的产物：存在且非空视为完整；首个不完整/缺失的片段即断点，其后（含该段）
// 的残留产物会被清理，避免拼接进半成品。返回值 ∈ [0, want]，且不超过实际段数。
// 传入 want <= 0 时不做任何校验、不清理，直接返回 0（等价于整任务重跑）。
func ResumableSegmentPrefix(plan *RenderPlan, want int) int {
	if plan == nil || want <= 0 {
		return 0
	}
	segStages := make([]RenderStage, 0, len(plan.Stages))
	for _, st := range plan.Stages {
		if st.Name == StageSegment {
			segStages = append(segStages, st)
		}
	}
	if len(segStages) == 0 {
		return 0
	}
	if want > len(segStages) {
		want = len(segStages)
	}

	ok := 0
	for i := 0; i < want; i++ {
		if !segmentArtifactComplete(segStages[i].Output) {
			break
		}
		ok++
	}
	if ok < want {
		logger.Warn("render", "resume: workDir 片段不完整（记录 %d 段，实际可用 %d 段），清理第 %d 段起的残留产物",
			want, ok, ok+1)
	}
	// 清理断点之后（含断点）的残留产物：半成品拼接会导致成品损坏
	for i := ok; i < len(segStages); i++ {
		if strings.TrimSpace(segStages[i].Output) == "" {
			continue
		}
		if _, err := os.Stat(segStages[i].Output); err == nil {
			if rerr := os.Remove(segStages[i].Output); rerr != nil {
				logger.Warn("render", "resume: 清理残留片段失败 %v: %v", segStages[i].Output, rerr)
			} else {
				logger.Info("render", "resume: 已清理残留片段 %v", segStages[i].Output)
			}
		}
	}
	return ok
}

// segmentArtifactComplete 片段产物完整性判定：存在、是普通文件且非空
func segmentArtifactComplete(path string) bool {
	p := strings.TrimSpace(path)
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return false
	}
	return true
}

// prepareWorkspace 建中间目录、写 concat 清单、磁盘空间预检（05 §4.5 / §6.1）
func prepareWorkspace(plan *RenderPlan) error {
	if err := os.MkdirAll(plan.WorkDir, 0755); err != nil {
		return fmt.Errorf("创建中间产物目录失败: %w", err)
	}
	if plan.ConcatList != "" {
		listPath := filepath.Join(plan.WorkDir, concatListName)
		if err := os.WriteFile(listPath, []byte(plan.ConcatList), 0644); err != nil {
			return fmt.Errorf("写入 concat 清单失败: %w", err)
		}
	}
	// 空间预检：需要 中间产物 + 成品（成品落在目标卷，单独检查）
	if plan.EstOutputBytes > 0 {
		free, err := winapi.GetDiskFreeSpace(plan.WorkDir)
		if err == nil && free > 0 && free < plan.EstOutputBytes {
			return fmt.Errorf("%s: 中间卷可用空间不足（需 %d MB，剩 %d MB）",
				"E_DISK_FULL", plan.EstOutputBytes/(1024*1024), free/(1024*1024))
		}
	}
	return nil
}

// runStage 执行单个 ffmpeg 阶段并解析进度
func runStage(taskID, ffmpegPath string, args []string, expectedMs int64, onFrac func(float64)) error {
	cfg := config.Get()
	cmd := exec.Command(ffmpegPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000 | getPriorityFlags(cfg.ProcessPriority),
	}

	process := &FFmpegProcess{
		TaskID:    taskID,
		Cmd:       cmd,
		Running:   true,
		stderrBuf: &bytes.Buffer{},
	}
	processMutex.Lock()
	processes[taskID] = process
	processMutex.Unlock()

	unregister := func() {
		processMutex.Lock()
		if cur, ok := processes[taskID]; ok && cur == process {
			delete(processes, taskID)
		}
		processMutex.Unlock()
	}
	defer unregister()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	logger.Info("render", "task=%s start stage: %s", taskID, strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return err
	}
	if jobHandle != 0 {
		if err := winapi.AssignProcessToJob(jobHandle, cmd.Process.Pid); err != nil {
			logger.Warn("ffmpeg", "Failed to assign process %d to job object: %v", cmd.Process.Pid, err)
		}
	}

	go func() {
		defer stderr.Close()
		buf := make([]byte, 4096)
		for {
			n, rerr := stderr.Read(buf)
			if n > 0 {
				process.bufMutex.Lock()
				process.stderrBuf.Write(buf[:n])
				process.bufMutex.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}()

	// 解析 `-progress pipe:1`（key=value 行）
	lastFrac := 0.0
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "progress=end" {
			lastFrac = 1
			if onFrac != nil {
				onFrac(lastFrac)
			}
			continue
		}
		us, ok := parseProgressLine(line)
		if !ok || expectedMs <= 0 {
			continue
		}
		frac := float64(us) / float64(expectedMs*1000)
		if frac > 0.995 {
			frac = 0.995 // 100% 由阶段正常结束统一上报
		}
		if frac > lastFrac {
			lastFrac = frac
			if onFrac != nil {
				onFrac(frac)
			}
		}
	}
	// 清空 stdout 剩余内容，避免管道阻塞
	_, _ = io.Copy(io.Discard, stdout)

	waitErr := cmd.Wait()

	process.mutex.Lock()
	wasRunning := process.Running
	process.mutex.Unlock()
	if !wasRunning {
		logger.Info("render", "task=%s stage stopped by user", taskID)
		return ErrRenderStopped
	}
	if waitErr != nil {
		process.bufMutex.Lock()
		tail := tailString(process.stderrBuf.String(), 2000)
		process.bufMutex.Unlock()
		logger.Error("render", "task=%s ffmpeg stage failed: %v; stderr tail: %s", taskID, waitErr, tail)
		return fmt.Errorf("ffmpeg 退出异常（%v）: %s", waitErr, tail)
	}
	if onFrac != nil {
		onFrac(1)
	}
	return nil
}

// parseProgressLine 解析 `-progress pipe:1` 的单行输出，返回 out_time（微秒）
func parseProgressLine(line string) (int64, bool) {
	switch {
	case strings.HasPrefix(line, "out_time_us="):
		v, err := strconv.ParseInt(strings.TrimSpace(line[len("out_time_us="):]), 10, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		return v, true
	case strings.HasPrefix(line, "out_time_ms="):
		// ffmpeg 该字段历史实现存在单位歧义（实为微秒），按微秒处理
		v, err := strconv.ParseInt(strings.TrimSpace(line[len("out_time_ms="):]), 10, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		return v, true
	case strings.HasPrefix(line, "out_time="):
		s := strings.TrimSpace(line[len("out_time="):])
		sec := parseTime(s)
		if sec <= 0 {
			return 0, false
		}
		return int64(sec * 1e6), true
	}
	return 0, false
}

func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// finalizeOutput 成品落盘：优先原子改名（同卷），失败回退带进度复制
func finalizeOutput(plan *RenderPlan, onFrac func(float64)) error {
	if plan.FinalFrom == "" || plan.OutputPath == "" {
		return fmt.Errorf("finalize 参数缺失（from=%q to=%q）", plan.FinalFrom, plan.OutputPath)
	}
	if err := os.MkdirAll(filepath.Dir(plan.OutputPath), 0755); err != nil {
		return err
	}
	if plan.FinalFrom == plan.OutputPath {
		if onFrac != nil {
			onFrac(1)
		}
		return nil
	}
	if err := os.Rename(plan.FinalFrom, plan.OutputPath); err == nil {
		if onFrac != nil {
			onFrac(1)
		}
		return nil
	}
	return copyFileWithProgress(plan.FinalFrom, plan.OutputPath, onFrac)
}

func copyFileWithProgress(src, dst string, onFrac func(float64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	st, err := in.Stat()
	if err != nil {
		return err
	}
	total := st.Size()

	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	buf := make([]byte, 4*1024*1024)
	var written int64
	lastTick := time.Now()
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(tmp)
				return werr
			}
			written += int64(n)
			if onFrac != nil && time.Since(lastTick) > 300*time.Millisecond {
				lastTick = time.Now()
				if total > 0 {
					onFrac(float64(written) / float64(total))
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	if onFrac != nil {
		onFrac(1)
	}
	return nil
}
