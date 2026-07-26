package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"Fnos.VC_Service/pkg/logger"
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

	ListenLocalOnly     bool   `json:"listen_local_only"`
	MaxConnPerClient    int    `json:"max_conn_per_client"`
	QpsLimit            int    `json:"qps_limit"`
	LogMaxSizeMB        int64  `json:"log_max_size_mb"`
	LogKeepDays         int    `json:"log_keep_days"`
	TempFileTTLHour     int    `json:"temp_file_ttl_hour"`
	EnableRangeDownload bool   `json:"enable_range_download"`
	LogLevel            string `json:"log_level"`
}

var (
	globalConfig *Config
	configMutex  sync.RWMutex
	encryptKey   = []byte{70, 86, 67, 83, 95, 50, 48, 50, 52, 95, 69, 110, 99, 114, 121, 112, 116, 95, 75, 101, 121, 95, 86, 50, 53, 54, 95, 83, 101, 99, 117, 114}
)

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

	decrypted, err := decrypt(data)
	if err != nil {
		return recoverDefault(ErrConfigDecrypt)
	}

	var cfg Config
	if err := json.Unmarshal(decrypted, &cfg); err != nil {
		return recoverDefault(ErrConfigInvalid)
	}

	globalConfig = &cfg
	return nil
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

func encrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(encryptKey)
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

func decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(encryptKey)
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
