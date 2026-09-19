package api

// P2-1 B轮：MediaProber 测试桩（随 proxy_flow_test.go 迁入 internal/media 后，
// 根包 M4 网关验收测试（stream_test.go）仍需注入探测通道，此处按根包
// MediaProber 接口（Probe + Available）重建同语义桩，避免依赖内部包私有符号）。

import (
	"os"
	"path/filepath"
)

// stubProber 桩探测：按本地绝对路径返回元数据，避免 ffprobe 在环。
type stubProber struct {
	byPath   map[string]VideoInfo
	fallback *VideoInfo
	err      error
}

func (s *stubProber) Probe(path string) (VideoInfo, error) {
	if s.err != nil {
		return VideoInfo{}, s.err
	}
	if s.byPath != nil {
		if v, ok := s.byPath[filepath.Clean(path)]; ok {
			return v, nil
		}
	}
	if s.fallback != nil {
		v := *s.fallback
		// fallback 未显式给出路径时，按探测入参回填（扫描目录用例依赖 path/fileName）。
		if v.Path == "" {
			v.Path = path
		}
		if v.FileName == "" {
			v.FileName = filepath.Base(path)
		}
		return v, nil
	}
	return VideoInfo{}, os.ErrNotExist
}

func (s *stubProber) Available() bool { return true }
