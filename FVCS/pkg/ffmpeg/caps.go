package ffmpeg

import (
	"os/exec"
	"runtime"
	"strings"
	"time"

	"Fnos.VC_Service/pkg/protocol"
)

// 节点能力探测（06 §5.1）：供 Hello 载荷上报使用
//
// 复用既有 queryEncoders（ffmpeg -encoders 解析）与 cfg.FFmpegPath，不重复实现硬件探测。

// candidateEncoders 上报白名单与顺序（设计 06 §5.1 示例顺序）
var candidateEncoders = []string{
	"h264_nvenc", "hevc_nvenc",
	"h264_qsv", "hevc_qsv",
	"h264_amf", "hevc_amf",
	"libx264", "libx265",
}

// ListEncoders 返回当前渲染机实际可用的编码器（保持白名单顺序）
func ListEncoders(ffmpegPath string) []string {
	hits := queryEncoders(ffmpegPath)
	out := make([]string, 0, len(candidateEncoders))
	for _, enc := range candidateEncoders {
		if hits[enc] {
			out = append(out, enc)
		}
	}
	return out
}

// DetectGPUs 依据可用硬编编码器 + 显卡名探测 GPU；探测不到时返回空切片（不报错）
func DetectGPUs(ffmpegPath string) []protocol.GPUInfo {
	encs := ListEncoders(ffmpegPath)
	names := listVideoControllerNames()

	byVendor := map[string][]string{}
	order := make([]string, 0, 3)
	add := func(vendor, enc string) {
		if _, ok := byVendor[vendor]; !ok {
			order = append(order, vendor)
		}
		byVendor[vendor] = append(byVendor[vendor], enc)
	}
	for _, enc := range encs {
		switch {
		case strings.HasSuffix(enc, "_nvenc"):
			add("nvidia", enc)
		case strings.HasSuffix(enc, "_qsv"):
			add("intel", enc)
		case strings.HasSuffix(enc, "_amf"):
			add("amd", enc)
		}
	}

	out := make([]protocol.GPUInfo, 0, len(order))
	for _, vendor := range order {
		out = append(out, protocol.GPUInfo{
			Vendor:   vendor,
			Name:     matchGPUName(vendor, names),
			Encoders: byVendor[vendor],
		})
	}
	return out
}

// matchGPUName 从显卡名列表中挑出与厂商匹配的一项，匹配不到时回退首个显卡名
func matchGPUName(vendor string, names []string) string {
	var keywords []string
	switch vendor {
	case "nvidia":
		keywords = []string{"nvidia", "geforce", "rtx", "gtx", "quadro", "tesla"}
	case "intel":
		keywords = []string{"intel", "uhd", "iris", "arc"}
	case "amd":
		keywords = []string{"amd", "radeon", "ati"}
	}
	for _, n := range names {
		low := strings.ToLower(n)
		for _, kw := range keywords {
			if strings.Contains(low, kw) {
				return n
			}
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// listVideoControllerNames 读取本机显卡名（Windows：CIM/WMIC；其他系统返回空）
func listVideoControllerNames() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	cmds := [][]string{
		{"powershell", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_VideoController).Name"},
		{"wmic", "path", "win32_VideoController", "get", "name"},
	}
	for _, c := range cmds {
		raw, err := runWithTimeout(c[0], c[1:], 3*time.Second)
		if err != nil {
			continue
		}
		names := make([]string, 0, 2)
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.EqualFold(line, "Name") {
				continue
			}
			names = append(names, line)
		}
		if len(names) > 0 {
			return names
		}
	}
	return nil
}

// runWithTimeout 带超时执行外部命令并返回 stdout
func runWithTimeout(name string, args []string, timeout time.Duration) (string, error) {
	cmd := exec.Command(name, args...)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.Output()
		close(done)
	}()
	select {
	case <-done:
		return string(out), err
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", exec.ErrNotFound
	}
}
