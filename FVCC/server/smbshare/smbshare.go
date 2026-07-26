// Package smbshare 解析 fnOS 服务器上的 Samba 共享配置文件，
// 根据用户名获取对应的 UID，进而读取共享目录配置。
//
// fnOS 的 samba 用户配置文件路径格式为：/etc/samba/users/<uid>.share.conf
// 其中 %U 是 UID（非用户名），例如 zdong 用户的 uid=1000，
// 实际配置文件路径为 /etc/samba/users/1000.share.conf。
package smbshare

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

const sambaConfigPath = "/etc/samba/users/%s.share.conf"

// Share 表示一个 SMB 共享条目
type Share struct {
	Name string // 共享名（如 "media"）
	Path string // 共享对应的本地路径（如 "/vol1/media"）
}

// resolveUID 通过用户名查找 UID。
// fnOS 的 samba 配置文件路径使用 UID 而非用户名。
func resolveUID(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("用户名不能为空")
	}
	u, err := user.Lookup(username)
	if err != nil {
		return "", fmt.Errorf("查找用户 %s 失败: %w", username, err)
	}
	return u.Uid, nil
}

// ListShares 读取指定用户的 samba 共享配置。
// 通过用户名查找 UID，再读取 /etc/samba/users/<uid>.share.conf 文件。
func ListShares(username string) ([]Share, error) {
	if username == "" {
		return nil, fmt.Errorf("用户名不能为空")
	}

	uid, err := resolveUID(username)
	if err != nil {
		return nil, err
	}

	configPath := fmt.Sprintf(sambaConfigPath, uid)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取 samba 配置文件失败 (%s): %w", configPath, err)
	}

	return parseShares(string(data)), nil
}

// parseShares 解析 samba 配置文件内容，提取共享名和路径。
// 配置文件格式示例：
//
//	[media]
//	path = /vol1/media
//	[movies]
//	path = /vol2/movies
func parseShares(content string) []Share {
	var shares []Share
	var currentName string

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// 共享名行：[sharename]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentName = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}

		// path 行：path = /xxx/yyy
		if currentName != "" && strings.HasPrefix(line, "path") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				p := strings.TrimSpace(parts[1])
				if p != "" {
					shares = append(shares, Share{Name: currentName, Path: p})
				}
			}
		}
	}

	return shares
}

// IsPathShared 校验本地路径是否可通过 SMB 协议访问。
// 满足以下任一条件即可：
//   - 路径在共享目录内（共享是路径的父目录或同一目录）
//   - 共享目录在路径内（路径是共享的父目录，浏览时会列出共享根的内容）
func IsPathShared(username, localPath string) (bool, error) {
	shares, err := ListShares(username)
	if err != nil {
		return false, err
	}

	abs, err := filepath.Abs(localPath)
	if err != nil {
		abs = localPath
	}

	for _, s := range shares {
		// 路径在共享目录内
		if abs == s.Path || strings.HasPrefix(abs, s.Path+string(filepath.Separator)) {
			return true, nil
		}
		// 共享目录在路径内（授权目录是共享目录的子路径）
		if s.Path == abs || strings.HasPrefix(s.Path, abs+string(filepath.Separator)) {
			return true, nil
		}
	}
	return false, nil
}

// FindShareForPath 查找路径所属的共享。
// 返回共享名和共享根目录的本地路径；若路径不属于任何共享，返回错误。
func FindShareForPath(username, localPath string) (*Share, error) {
	shares, err := ListShares(username)
	if err != nil {
		return nil, err
	}

	abs, err := filepath.Abs(localPath)
	if err != nil {
		abs = localPath
	}

	for i := range shares {
		s := &shares[i]
		if abs == s.Path || strings.HasPrefix(abs, s.Path+string(filepath.Separator)) {
			return s, nil
		}
	}
	return nil, fmt.Errorf("路径 %s 不在用户 %s 的任何共享目录内", localPath, username)
}

// BuildSMBURL 根据本地路径构建 SMB URL。
// 例如：localPath=/vol1/media/movies/a.mp4, share=media(路径=/vol1/media)
// 返回 \\fnosIP\media\movies\a.mp4
func BuildSMBURL(fnosIP, username, localPath string) (string, error) {
	share, err := FindShareForPath(username, localPath)
	if err != nil {
		return "", err
	}

	abs, err := filepath.Abs(localPath)
	if err != nil {
		abs = localPath
	}

	// 计算相对共享根的相对路径
	rel := strings.TrimPrefix(abs, share.Path)
	rel = strings.TrimPrefix(rel, string(filepath.Separator))

	// 构建 SMB URL: \\ip\sharename\relative\path
	// 将 Unix 路径分隔符转为 Windows 风格
	rel = strings.ReplaceAll(rel, "/", "\\")

	if rel == "" {
		return fmt.Sprintf("\\\\%s\\%s", fnosIP, share.Name), nil
	}
	return fmt.Sprintf("\\\\%s\\%s\\%s", fnosIP, share.Name, rel), nil
}

// GetLocalIP 获取本机非回环 IPv4 地址。
// 用于构建 SMB URL（FVCC 运行在 fnOS 上，需要本机 IP 让 FVCS 连接）。
func GetLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("获取网络接口失败: %w", err)
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("未找到非回环 IPv4 地址")
}
