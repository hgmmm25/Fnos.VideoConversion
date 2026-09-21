package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/winapi"
)

const (
	configFileName   = "config.enc.json"
	configTempName   = "config.tmp"
	defaultWsPort    = 8080
	defaultMaxTasks  = 2
	defaultChunkSize = 10 * 1024 * 1024
)

var (
	ErrConfigDecrypt = errors.New("config decrypt failed")
	ErrConfigInvalid = errors.New("config file invalid")
)

type Config struct {
	AuthKeyEncrypt     string `json:"auth_key_encrypt"`
	WsPort             int    `json:"ws_port"`
	MaxConcurrentTasks int    `json:"max_concurrent_tasks"`
	ChunkSize          int64  `json:"chunk_size"`
	ProcessPriority    int    `json:"process_priority"`
	TempDir            string `json:"temp_dir"`
	FFmpegPath         string `json:"ffmpeg_path"`
	AutoStart          bool   `json:"auto_start"`
	AutoRunOnBoot      bool   `json:"auto_run_on_boot"`

	ListenLocalOnly        bool   `json:"listen_local_only"`
	MaxConnPerClient       int    `json:"max_conn_per_client"`
	QpsLimit               int    `json:"qps_limit"`
	MaxWSConns             int    `json:"max_ws_conns"`             // 07 §4.5：WS 单节点并发上限（≤3）
	DisableSecurityHeaders bool   `json:"disable_security_headers"` // 07 §4.4：显式关闭安全响应头（默认关闭即为开启）
	ListenAddr             string `json:"listen_addr"`              // 07 §4.4：显式监听地址；空则由 ListenLocalOnly 推导
	LogMaxSizeMB           int64  `json:"log_max_size_mb"`
	LogKeepDays            int    `json:"log_keep_days"`
	TempFileTTLHour        int    `json:"temp_file_ttl_hour"`
	EnableRangeDownload    bool   `json:"enable_range_download"`
	LogLevel               string `json:"log_level"`

	// WSS 数据面加密（改进方向 P1-3 / SECURITY.md §4）：两项均非空时 WS 端口启用 TLS，
	// 缺一不可；留空则维持明文（内网隔离兜底）。证书应为自签或内网 CA 签发。
	WSTLSCert string `json:"ws_tls_cert"`
	WSTLSKey  string `json:"ws_tls_key"`
}

const (
	aesKeySize        = 32
	masterKeyFileName = "master.key"

	// 默认值（07 §4.4 / §4.5）
	defaultMaxWSConns = 3
)

var (
	globalConfig *Config
	configMutex  sync.RWMutex

	// legacyEncryptKey 为 M3 之前的硬编码密钥，仅保留用于解密既有 config.enc.json
	// （07 §5.3：禁止静态硬编码密钥）。新写入一律使用 DPAPI 主密钥。
	legacyEncryptKey = []byte{70, 86, 67, 83, 95, 50, 48, 50, 52, 95, 69, 110, 99, 114, 121, 112, 116, 95, 75, 101, 121, 95, 86, 50, 53, 54, 95, 83, 101, 99, 117, 114}

	masterKeyOnce sync.Once
	masterKeyVal  []byte
	masterKeyErr  error
)

// masterKey 返回本机配置主密钥（07 §5.3 / M3 迁移）：
//   - 优先读取 exe 同级 master.key（DPAPI LocalMachine 保护，本机任意账户可解）；
//   - 缺失或不可解时重新生成 32 字节随机密钥并 DPAPI 保护后落盘（0600）；
//   - 生成/落盘失败返回 error，调用方回退旧硬编码密钥以保持可用性（不静默丢配置）。
func masterKey() ([]byte, error) {
	masterKeyOnce.Do(func() {
		path := filepath.Join(getAppDir(), masterKeyFileName)
		if data, err := os.ReadFile(path); err == nil {
			plain, decErr := winapi.DPAPIUnprotect(data)
			if decErr == nil && len(plain) == aesKeySize {
				masterKeyVal = plain
				return
			}
			logger.Warn("config", "Master key unreadable (%v), regenerating", decErr)
		}
		key := make([]byte, aesKeySize)
		if _, err := rand.Read(key); err != nil {
			masterKeyErr = fmt.Errorf("generate master key: %w", err)
			return
		}
		sealed, err := winapi.DPAPIProtect(key, "FVCS config master key")
		if err != nil {
			masterKeyErr = fmt.Errorf("protect master key: %w", err)
			return
		}
		if err := os.WriteFile(path, sealed, 0600); err != nil {
			masterKeyErr = fmt.Errorf("write master key: %w", err)
			return
		}
		logger.Info("config", "Generated DPAPI-protected master key: %s", path)
		masterKeyVal = key
	})
	return masterKeyVal, masterKeyErr
}

func init() {
	globalConfig = &Config{
		WsPort:              defaultWsPort,
		MaxConcurrentTasks:  defaultMaxTasks,
		ChunkSize:           defaultChunkSize,
		ProcessPriority:     3,
		TempDir:             ".\\temp",
		FFmpegPath:          ".\\ffmpeg.exe",
		AutoStart:           true,
		AutoRunOnBoot:       false,
		ListenLocalOnly:     false,
		MaxConnPerClient:    5,
		QpsLimit:            100,
		MaxWSConns:          defaultMaxWSConns,
		LogMaxSizeMB:        10,
		LogKeepDays:         7,
		TempFileTTLHour:     24,
		EnableRangeDownload: true,
		LogLevel:            "INFO",
	}
}

func Get() *Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return globalConfig
}

func Set(cfg *Config) {
	configMutex.Lock()
	defer configMutex.Unlock()
	globalConfig = cfg
}

func Load() error {
	configMutex.Lock()

	appDir := getAppDir()
	path := filepath.Join(appDir, configFileName)

	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		globalConfig.AuthKeyEncrypt = encryptString(generateRandomKey())

		configMutex.Unlock()
		return Save()
	}

	configMutex.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return recoverDefault(err)
	}

	decrypted, usedLegacy, err := decryptForLoad(data)
	if err != nil {
		return recoverDefault(ErrConfigDecrypt)
	}

	var cfg Config
	if err := json.Unmarshal(decrypted, &cfg); err != nil {
		return recoverDefault(ErrConfigInvalid)
	}
	normalizeDefaults(&cfg)

	globalConfig = &cfg

	if usedLegacy {
		// M3 迁移：用旧硬编码密钥解出的配置立即以 DPAPI 主密钥重写（07 §5.3）
		logger.Info("config", "Config decrypted with legacy key, re-encrypting with DPAPI master key")
		if err := Save(); err != nil {
			logger.Warn("config", "Re-encrypt config failed: %v", err)
		}
	}
	return nil
}

// normalizeDefaults 兜底旧版配置缺失的新增字段（不覆盖用户显式配置）。
func normalizeDefaults(cfg *Config) {
	if cfg.MaxWSConns <= 0 {
		cfg.MaxWSConns = defaultMaxWSConns
	}
	if cfg.QpsLimit <= 0 {
		cfg.QpsLimit = 100
	}
	cfg.ListenAddr = strings.TrimSpace(cfg.ListenAddr)
}

func Save() error {
	configMutex.Lock()
	defer configMutex.Unlock()

	path := filepath.Join(getAppDir(), configFileName)
	tmpPath := filepath.Join(getAppDir(), configTempName)

	data, err := json.MarshalIndent(globalConfig, "", "  ")
	if err != nil {
		return err
	}

	encrypted, err := encrypt(data)
	if err != nil {
		return err
	}

	if err := os.WriteFile(tmpPath, encrypted, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

func recoverDefault(err error) error {
	logger.Warn("config", "Config file corrupted, recovering default: %s", err.Error())

	path := filepath.Join(getAppDir(), configFileName)
	if _, err := os.Stat(path); err == nil {
		backupPath := path + ".bak"
		os.Rename(path, backupPath)
	}

	globalConfig = &Config{
		AuthKeyEncrypt:      encryptString(generateRandomKey()),
		WsPort:              defaultWsPort,
		MaxConcurrentTasks:  defaultMaxTasks,
		ChunkSize:           defaultChunkSize,
		ProcessPriority:     3,
		TempDir:             ".\\temp",
		FFmpegPath:          ".\\ffmpeg.exe",
		AutoStart:           true,
		AutoRunOnBoot:       false,
		ListenLocalOnly:     false,
		MaxConnPerClient:    5,
		QpsLimit:            100,
		MaxWSConns:          defaultMaxWSConns,
		LogMaxSizeMB:        10,
		LogKeepDays:         7,
		TempFileTTLHour:     24,
		EnableRangeDownload: true,
		LogLevel:            "INFO",
	}

	return Save()
}

func getAppDir() string {
	exePath, err := os.Executable()
	if err != nil {
		dir, err := os.Getwd()
		if err != nil {
			return "."
		}
		return dir
	}
	return filepath.Dir(exePath)
}

func generateRandomKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func encryptString(s string) string {
	data, err := encrypt([]byte(s))
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

// encrypt 使用 DPAPI 主密钥加密（主密钥不可用时回退旧密钥，保证服务可用）。
func encrypt(data []byte) ([]byte, error) {
	key, err := masterKey()
	if err != nil {
		logger.Warn("config", "Master key unavailable (%v), fallback to legacy key", err)
		key = legacyEncryptKey
	}
	return seal(key, data)
}

// seal 使用指定 32 字节密钥做 AES-256-GCM 加密（nonce 前置）。
func seal(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, data, nil), nil
}

// openWith 使用指定密钥解密；密钥不匹配/GCM 校验失败返回 ErrConfigDecrypt。
func openWith(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, ErrConfigDecrypt
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// decrypt 解密配置：优先 DPAPI 主密钥，失败回落旧硬编码密钥（M3 迁移兼容）。
func decrypt(data []byte) ([]byte, error) {
	plain, _, err := decryptForLoad(data)
	return plain, err
}

// decryptForLoad 解密配置并回报是否使用了旧密钥（true 表示需要重写为 DPAPI 主密钥）。
func decryptForLoad(data []byte) ([]byte, bool, error) {
	if key, err := masterKey(); err == nil {
		if plain, err := openWith(key, data); err == nil {
			return plain, false, nil
		}
	}
	plain, err := openWith(legacyEncryptKey, data)
	if err != nil {
		return nil, false, ErrConfigDecrypt
	}
	return plain, true, nil
}

// Reencrypt 用 DPAPI 主密钥重写配置文件（M3 迁移 / 维护入口，见 reencrypt.go）。
// 与旧实现（无条件 Save）不同：仅在文件确为旧硬编码密钥密文时重写，且不依赖
// 内存中的 globalConfig，可安全地在服务进程之外（fvcs-cli）单独调用。
func Reencrypt() error {
	path := ConfigPath()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil // 无配置文件即无迁移对象
		}
		return err
	}
	_, err := ReencryptFile(path, false)
	return err
}

func DecryptAuthKey() string {
	configMutex.RLock()
	defer configMutex.RUnlock()
	if globalConfig.AuthKeyEncrypt == "" {
		return ""
	}
	encryptedData, err := base64.StdEncoding.DecodeString(globalConfig.AuthKeyEncrypt)
	if err != nil {
		return ""
	}
	data, err := decrypt(encryptedData)
	if err != nil {
		return ""
	}
	return string(data)
}

func SetAuthKey(key string) {
	configMutex.Lock()
	defer configMutex.Unlock()
	globalConfig.AuthKeyEncrypt = encryptString(key)
}

func GetAuthKey() string {
	return DecryptAuthKey()
}
