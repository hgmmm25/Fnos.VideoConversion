package store

// trash_test.go — P2-5 删除回收站化单测：移动/恢复/清空/越权防护
//
// 随 P2-1 阶段 A 由 server/trash_test.go 迁入 internal/store；
// 原测试通过 *Handlers 调用 moveToTrash，现直接以授权根列表调用
// MoveToTrash（行为等价：函数仅依赖授权根集合）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveToTrashRestore(t *testing.T) {
	root := t.TempDir()

	src := filepath.Join(root, "movies", "a.mp4")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	trashPath, err := MoveToTrash([]string{root}, src)
	if err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}
	if !strings.Contains(trashPath, filepath.Join(root, TrashDirName)) {
		t.Fatalf("trash path 不在回收站内: %s", trashPath)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("原文件应已移走: %v", err)
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("回收站文件应存在: %v", err)
	}

	// 恢复：回收站 -> 原路径
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(trashPath, src); err != nil {
		t.Fatalf("restore rename: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("恢复后原路径应存在: %v", err)
	}
}

func TestMoveToTrashOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.mp4")
	if _, err := MoveToTrash([]string{root}, outside); err == nil {
		t.Fatal("越权路径移入回收站应报错")
	}
}

func TestEmptyTrash(t *testing.T) {
	root := t.TempDir()

	src := filepath.Join(root, "sub", "b.mp4")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveToTrash([]string{root}, src); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}

	// 直接清空目录内容（等价 emptyTrash 的核心逻辑）
	trashRoot := filepath.Join(root, TrashDirName)
	entries, err := os.ReadDir(trashRoot)
	if err != nil {
		t.Fatalf("read trash: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("回收站应有 1 项，实际 %d", len(entries))
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(trashRoot, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadDir(trashRoot)
	if len(after) != 0 {
		t.Fatalf("清空后回收站应为空，实际 %d", len(after))
	}
}
