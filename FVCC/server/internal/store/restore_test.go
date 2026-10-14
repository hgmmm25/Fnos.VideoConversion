// restore_test.go — P3-5 阶段 C：快照回滚测试。
// 验收（STORE_PERSISTENCE_ROADMAP §5-C）：损坏/篡改 zip 拒绝、恢复后 Load 正常、
// 运行中任务重置 QUEUE、审计落库、失败不改动内存、zip-slip 与未来版本拒绝。

package store

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"fvcc/internal/store/model"
)

// readZipEntries 读取 zip 全部条目（条目名 → 内容）。
func readZipEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read entry %s: %v", f.Name, err)
		}
		out[f.Name] = b
	}
	return out
}

// makeZipFile 手工写 zip（不经 buildBackupZip，便于构造篡改/恶意条目）。
func makeZipFile(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	zw := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatalf("zip create %s: %v", n, err)
		}
		if _, err := w.Write(files[n]); err != nil {
			t.Fatalf("zip write %s: %v", n, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
}

// fakeBackupID 生成一个可过 parseBackupName 的测试备份 ID（未来时间戳避免碰撞）。
func fakeBackupID(kind string) string {
	return "2099" + time.Now().UTC().Format("0102150405") + "-" + kind
}

func seedStoreForRestore(t *testing.T, dir string) (*Store, string) {
	t.Helper()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.UpsertTask(model.Task{ID: "t1", OrderID: 1, Status: model.StatusTranscoding, FileName: "a.mp4", SourceFile: "/a.mp4"})
	s.UpsertServer(model.Server{ID: "s1", Name: "node1"})
	if _, err := s.CreateProject(model.Project{Name: "回滚前项目"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return s, filepath.Join(dir, backupDirName)
}

func TestRestoreRoundTripAndQueueReset(t *testing.T) {
	dir := t.TempDir()
	s, _ := seedStoreForRestore(t, dir)

	man, err := s.Backup(BackupKindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// 备份后继续演化：新增任务、删除服务器。
	s.UpsertTask(model.Task{ID: "t2", OrderID: 2, Status: model.StatusQueue, FileName: "b.mp4", SourceFile: "/b.mp4"})
	if !s.DeleteServer("s1") {
		t.Fatalf("DeleteServer 未命中")
	}

	res, err := s.Restore(man.ID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.PreRestoreID == "" {
		t.Fatalf("回滚前未生成 prerestore 备份")
	}

	// 整包恢复语义：t2 消失、s1 回来、项目仍在。
	if _, ok := s.GetTask("t2"); ok {
		t.Fatalf("部分回滚泄漏：备份后新增任务 t2 未被移除")
	}
	if _, ok := s.GetServer("s1"); !ok {
		t.Fatalf("备份后删除的服务器 s1 未恢复")
	}
	ps := s.GetProjects()
	if len(ps) != 1 || ps[0].Name != "回滚前项目" {
		t.Fatalf("项目未恢复: %+v", ps)
	}
	// 评审点 3：回滚后运行中任务重置 QUEUE（同崩溃恢复语义）。
	task, ok := s.GetTask("t1")
	if !ok {
		t.Fatalf("任务 t1 丢失")
	}
	if task.Status != model.StatusQueue {
		t.Fatalf("运行中任务未重置 QUEUE，实际 %q", task.Status)
	}

	// 审计：snapshot.restore 落库；prerestore 备份存在。
	if n := countAuditAction(t, s, "snapshot.restore"); n != 1 {
		t.Fatalf("snapshot.restore 审计数=%d，期望 1", n)
	}
	if n := countBackupsKind(t, s, BackupKindPreRestore); n != 1 {
		t.Fatalf("prerestore 备份数=%d，期望 1", n)
	}

	// 恢复结果已持久化：重启后读回一致。
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("重启 Load: %v", err)
	}
	if _, ok := s2.GetTask("t2"); ok {
		t.Fatalf("重启后 t2 复现，恢复未落库")
	}
	if _, ok := s2.GetServer("s1"); !ok {
		t.Fatalf("重启后 s1 缺失，恢复未落库")
	}
}

func TestRestoreRejectsTamperedZip(t *testing.T) {
	dir := t.TempDir()
	s, bdir := seedStoreForRestore(t, dir)

	man, err := s.Backup(BackupKindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	orig := readZipEntries(t, filepath.Join(bdir, man.File))
	// 原位翻一个字节：尺寸不变（先过尺寸检查），但 SHA-256 必不匹配 manifest。
	tampered := append([]byte(nil), orig["tasks.json"]...)
	tampered[len(tampered)/2] ^= 0xFF
	orig["tasks.json"] = tampered
	id := fakeBackupID("manual")
	makeZipFile(t, filepath.Join(bdir, "backup-"+id+".zip"), orig)

	// 篡改后现状数据：t2 存在。恢复应被拒绝且不改动内存。
	s.UpsertTask(model.Task{ID: "t2", OrderID: 2, Status: model.StatusQueue, FileName: "b.mp4", SourceFile: "/b.mp4"})
	_, err = s.Restore(id)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("篡改包应验签失败，实际: %v", err)
	}
	if _, ok := s.GetTask("t2"); !ok {
		t.Fatalf("验签失败不应改动内存态")
	}
	if n := countBackupsKind(t, s, BackupKindPreRestore); n != 0 {
		t.Fatalf("验签失败发生在备份前，不应产生 prerestore 备份，实际 %d", n)
	}
	if n := countAuditAction(t, s, "security.alert"); n != 1 {
		t.Fatalf("失败应记 security.alert，实际 %d", n)
	}
}

func TestRestoreRejectsZipSlipEntry(t *testing.T) {
	dir := t.TempDir()
	s, bdir := seedStoreForRestore(t, dir)
	if _, err := s.Backup(BackupKindManual); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	evil := map[string][]byte{
		"../evil.json": []byte("pwned"),
	}
	id := fakeBackupID("manual")
	makeZipFile(t, filepath.Join(bdir, "backup-"+id+".zip"), evil)

	_, err := s.Restore(id)
	if err == nil || !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("白名单外条目应拒绝，实际: %v", err)
	}
	// 条目只在内存解析，绝不落盘：dataDir 外层不得出现 evil.json
	if _, err := os.Stat(filepath.Join(dir, "evil.json")); !os.IsNotExist(err) {
		t.Fatalf("zip-slip 条目被写盘")
	}
}

func TestRestoreRejectsFutureSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	s, bdir := seedStoreForRestore(t, dir)
	man, err := s.Backup(BackupKindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	orig := readZipEntries(t, filepath.Join(bdir, man.File))

	var mf BackupManifest
	if err := json.Unmarshal(orig[backupManifest], &mf); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	mf.SchemaVersion = schemaVersion + 5
	nb, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	orig[backupManifest] = nb

	id := fakeBackupID("manual")
	makeZipFile(t, filepath.Join(bdir, "backup-"+id+".zip"), orig)
	_, err = s.Restore(id)
	if err == nil || !strings.Contains(err.Error(), "高于本程序支持") {
		t.Fatalf("高版本备份应拒绝恢复，实际: %v", err)
	}
}

func TestRestoreRejectsBadBackupID(t *testing.T) {
	dir := t.TempDir()
	s, _ := seedStoreForRestore(t, dir)

	if _, err := s.Restore(""); err == nil {
		t.Fatalf("空 ID 应拒绝")
	}
	if _, err := s.Restore("../../etc/passwd"); err == nil ||
		!strings.Contains(err.Error(), "非法备份 ID") {
		t.Fatalf("路径穿越 ID 应被判定非法，实际: %v", err)
	}
	if _, err := s.Restore("20990101000000-manual"); err == nil ||
		!strings.Contains(err.Error(), "不存在") {
		t.Fatalf("未知 ID 应报备份不存在，实际: %v", err)
	}
}
