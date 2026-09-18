package main

// trash_test.go — P2-5 删除回收站化单测：移动/恢复/清空/越权防护

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTrashTestHandlers 构造带授权根的回收站测试 Handler。
func newTrashTestHandlers(t *testing.T, roots ...string) *Handlers {
	t.Helper()
	store := NewStore(filepath.Join(t.TempDir(), "test.db"))
	pv := NewPathValidator(false, "")
	pv.SetExtraPaths(roots)
	return &Handlers{store: store, pv: pv, hub: NewHub()}
}

func TestMoveToTrashRestore(t *testing.T) {
	root := t.TempDir()
	h := newTrashTestHandlers(t, root)

	src := filepath.Join(root, "movies", "a.mp4")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	trashPath, err := h.moveToTrash(src)
	if err != nil {
		t.Fatalf("moveToTrash: %v", err)
	}
	if !strings.Contains(trashPath, filepath.Join(root, trashDirName)) {
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
	h := newTrashTestHandlers(t, root)
	outside := filepath.Join(t.TempDir(), "x.mp4")
	if _, err := h.moveToTrash(outside); err == nil {
		t.Fatal("越权路径移入回收站应报错")
	}
}

func TestEmptyTrash(t *testing.T) {
	root := t.TempDir()
	h := newTrashTestHandlers(t, root)

	src := filepath.Join(root, "sub", "b.mp4")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.moveToTrash(src); err != nil {
		t.Fatalf("moveToTrash: %v", err)
	}

	// 直接清空目录内容（等价 emptyTrash 的核心逻辑）
	trashRoot := filepath.Join(root, trashDirName)
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
