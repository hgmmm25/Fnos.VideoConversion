// backup_test.go — P3-5 阶段 A：store 备份单测。
// 覆盖方案稿 §5 阶段 A 验收项：备份可解压、manifest 校验和一致、保留策略轮转、
// 凭据密文入包且 secret.key 不入包、关键写前备份去抖。

package store

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fvcc/internal/store/model"
)

// openBackupZip 打开备份 zip，返回 (reader, 已读文件内容 map)。
func openBackupZip(t *testing.T, path string) (*zip.Reader, map[string][]byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open backup zip: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	st, err := f.Stat()
	if err != nil {
		t.Fatalf("stat backup zip: %v", err)
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	contents := map[string][]byte{}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", zf.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		contents[zf.Name] = b
	}
	return zr, contents
}

// TestBackupZipManifestChecksum 验证备份可解压、manifest 记录的每个文件 SHA-256 与内容一致，
// 且 secret.key 不在包内、凭据以密文形态存在。
func TestBackupZipManifestChecksum(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// 造数据：含明文凭据的 server + 含明文密码的 settings（内存态为明文）。
	s.UpsertServer(model.Server{ID: "srv1", Name: "n", AuthKey: "plain-secret", Status: "online"})
	st := s.GetSettings()
	st.SMBPassword = "plain-pass"
	s.SaveSettings(st)
	s.UpsertTask(model.Task{ID: "t1", OrderID: 1, Status: model.StatusQueue, FileName: "movie.mkv"})

	res, err := s.Backup(BackupKindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	zipPath := filepath.Join(dir, backupDirName, res.File)
	_, contents := openBackupZip(t, zipPath)

	// 1) secret.key 不得入包。
	if _, ok := contents["secret.key"]; ok {
		t.Fatal("备份包内不应包含 secret.key")
	}
	if _, ok := contents[backupManifest]; !ok {
		t.Fatal("备份包内缺少 manifest.json")
	}

	// 2) manifest 校验和与逐文件内容一致。
	var man BackupManifest
	if err := json.Unmarshal(contents[backupManifest], &man); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if man.SchemaVersion != schemaVersion {
		t.Fatalf("manifest schemaVersion=%d, 期望 %d", man.SchemaVersion, schemaVersion)
	}
	if man.Counts["tasks"] != 1 || man.Counts["servers"] != 1 {
		t.Fatalf("manifest counts 异常: %+v", man.Counts)
	}
	for _, f := range man.Files {
		b, ok := contents[f.Name]
		if !ok {
			t.Fatalf("manifest 记录的文件 %s 不在包内", f.Name)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Fatalf("文件 %s 校验和不一致", f.Name)
		}
	}

	// 3) 凭据密文入包（enc:v1: 前缀，非明文）。
	var sf model.ServersFile
	if err := json.Unmarshal(contents["server.json"], &sf); err != nil {
		t.Fatalf("unmarshal server.json: %v", err)
	}
	if len(sf.Servers) != 1 || sf.Servers[0].AuthKey == "plain-secret" || len(sf.Servers[0].AuthKey) < 7 || sf.Servers[0].AuthKey[:7] != "enc:v1:" {
		t.Fatalf("备份内 AuthKey 未加密: %+v", sf.Servers)
	}
	var setf model.SettingsFile
	if err := json.Unmarshal(contents["settings.json"], &setf); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}
	if setf.Settings.SMBPassword == "plain-pass" {
		t.Fatal("备份内 SMBPassword 未加密")
	}
}

// TestBackupRetentionRotation 验证系统生成池（auto）按时间保留最近 backupKeepTimed 份，
// manual 不参与轮转。
func TestBackupRetentionRotation(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// 注入时钟：每次备份前进 1 秒，保证文件名时间戳唯一可断言。
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	n := 0
	old := backupClock
	backupClock = func() time.Time { n++; return base.Add(time.Duration(n) * time.Second) }
	defer func() { backupClock = old }()

	total := backupKeepTimed + 5
	for i := 0; i < total; i++ {
		if _, err := s.Backup(BackupKindAuto); err != nil {
			t.Fatalf("auto backup %d: %v", i, err)
		}
	}
	// 手动备份 2 份，不应被轮转删除。
	for i := 0; i < 2; i++ {
		if _, err := s.Backup(BackupKindManual); err != nil {
			t.Fatalf("manual backup %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(dir, backupDirName))
	if err != nil {
		t.Fatalf("readdir backup: %v", err)
	}
	var autoCount, manualCount int
	for _, e := range entries {
		_, kind, ok := parseBackupName(e.Name())
		if !ok {
			continue
		}
		switch kind {
		case BackupKindAuto:
			autoCount++
		case BackupKindManual:
			manualCount++
		}
	}
	if autoCount != backupKeepTimed {
		t.Fatalf("auto 备份应保留 %d 份，实际 %d", backupKeepTimed, autoCount)
	}
	if manualCount != 2 {
		t.Fatalf("manual 备份应全部保留（2 份），实际 %d", manualCount)
	}
}

// TestPreCriticalBackupDebounce 验证关键写前备份去抖：窗口内多次关键写只落一份 prewrite 备份。
func TestPreCriticalBackupDebounce(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	oldInt := prewriteMinInterval
	prewriteMinInterval = time.Minute
	defer func() { prewriteMinInterval = oldInt }()

	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	n := 0
	oldClock := backupClock
	backupClock = func() time.Time { n++; return base.Add(time.Duration(n) * time.Millisecond) }
	defer func() { backupClock = oldClock }()

	// 连续 3 次 UpsertServer（关键写），去抖窗口内应只产生 1 份 prewrite 备份。
	for i := 0; i < 3; i++ {
		s.UpsertServer(model.Server{ID: "s", Name: "n", Status: "online"})
	}

	prewrite := 0
	entries, _ := os.ReadDir(filepath.Join(dir, backupDirName))
	for _, e := range entries {
		if _, kind, ok := parseBackupName(e.Name()); ok && kind == BackupKindPrewrite {
			prewrite++
		}
	}
	if prewrite != 1 {
		t.Fatalf("去抖窗口内 3 次关键写应只落 1 份 prewrite 备份，实际 %d", prewrite)
	}
}
