package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathValidator 路径安全校验器，按 FS.md 8.2 四层校验逻辑实现。
type PathValidator struct {
	accessPaths []string // 授权目录列表 (TRIM_DATA_ACCESSIBLE_PATHS)
	extraPaths  []string // 用户手动配置的额外授权目录（通过设置页配置）
	envFile     string   // config_callback 写入的授权目录文件路径
	devMode     bool     // 开发模式标志
}

// NewPathValidator 创建路径校验器。
// devMode 为 true 时（本地开发），若环境变量未设置则放行当前工作目录。
// 生产模式下若环境变量未设置，返回空列表，前端将提示未授权。
// envFile 为 config_callback 脚本写入的授权目录文件路径，用于运行时
// 动态读取 fnOS 最新授权目录（进程启动后环境变量不会变化，需通过文件传递）。
func NewPathValidator(devMode bool, envFile string) *PathValidator {
	pv := &PathValidator{devMode: devMode, envFile: envFile}
	pv.reload()
	return pv
}

// SetExtraPaths 设置用户手动配置的额外授权目录。
func (pv *PathValidator) SetExtraPaths(paths []string) {
	pv.extraPaths = pv.extraPaths[:0]
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				pv.extraPaths = append(pv.extraPaths, abs)
			} else {
				pv.extraPaths = append(pv.extraPaths, p)
			}
		}
	}
}

// allPaths 返回环境变量授权目录与手动配置目录的合并列表。
func (pv *PathValidator) allPaths() []string {
	result := make([]string, 0, len(pv.accessPaths)+len(pv.extraPaths))
	seen := map[string]bool{}
	for _, p := range pv.accessPaths {
		if !seen[p] {
			result = append(result, p)
			seen[p] = true
		}
	}
	for _, p := range pv.extraPaths {
		if !seen[p] {
			result = append(result, p)
			seen[p] = true
		}
	}
	return result
}

// reload 重新加载授权目录。
// 优先从 envFile（由 config_callback 写入）读取，以获取 fnOS 运行时
// 动态授权的最新目录；若文件不存在或为空，则回退到进程环境变量。
func (pv *PathValidator) reload() {
	pv.accessPaths = pv.accessPaths[:0]

	raw := ""
	// 优先从 config_callback 写入的文件读取（支持运行时动态授权）
	if pv.envFile != "" {
		if data, err := os.ReadFile(pv.envFile); err == nil {
			raw = strings.TrimSpace(string(data))
		}
	}
	// 回退到进程环境变量（启动时快照，不会动态变化）
	if raw == "" {
		raw = os.Getenv("TRIM_DATA_ACCESSIBLE_PATHS")
	}

	if raw == "" {
		if pv.devMode {
			// 开发模式下未设置，放行当前目录
			if cwd, err := os.Getwd(); err == nil {
				pv.accessPaths = []string{cwd}
			}
		}
		return
	}
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ':' || r == '\n' || r == '\r'
	}) {
		p = strings.TrimSpace(p)
		if p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				pv.accessPaths = append(pv.accessPaths, abs)
			} else {
				pv.accessPaths = append(pv.accessPaths, p)
			}
		}
	}
}

// Validate 完整四层路径安全校验：
//  1. Clean 标准化路径（URL 解码由 gin 框架完成，避免双重解码）
//  2. 拦截显性 .. 路径
//  3. EvalSymlinks 解析软链接获取真实路径
//  4. 强制校验真实路径前缀属于授权目录
func (pv *PathValidator) Validate(path string) error {
	// 每次校验前重新读取环境变量，支持 fnOS 运行时动态授权
	pv.reload()

	// 1. 标准化路径（gin c.Query 已完成 URL 解码，此处不再重复解码）
	clean := filepath.Clean(path)

	// 2. 拦截显性 ..
	if strings.Contains(clean, "..") {
		return fmt.Errorf("非法路径: 包含 .. 穿越字符")
	}

	// 3. 解析软链接获取真实物理路径
	realPath, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// 文件可能尚不存在（如输出路径），尝试对父目录解析
		parent := filepath.Dir(clean)
		realParent, perr := filepath.EvalSymlinks(parent)
		if perr != nil {
			return fmt.Errorf("路径解析失败: %w", err)
		}
		realPath = filepath.Join(realParent, filepath.Base(clean))
	}

	// 4. 校验真实路径前缀属于授权目录
	allPaths := pv.allPaths()
	if len(allPaths) == 0 {
		// 生产模式下未配置授权目录，拒绝访问（防止越权）
		// 开发模式下 NewPathValidator 已回退到当前工作目录，不会走到这里
		return fmt.Errorf("未配置授权目录，请在 fnOS 应用设置中为此应用授权共享文件夹")
	}
	for _, allow := range allPaths {
		if strings.HasPrefix(realPath, allow) {
			return nil
		}
	}
	return fmt.Errorf("路径超出授权访问范围: %s", realPath)
}

// AccessPaths 返回授权目录列表（供前端展示）。
// 每次调用时重新读取环境变量，支持 fnOS 运行时动态授权。
func (pv *PathValidator) AccessPaths() []string {
	pv.reload()
	return pv.allPaths()
}
