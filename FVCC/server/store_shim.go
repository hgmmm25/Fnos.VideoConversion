package main

// store_shim.go — P2-1 阶段 A 兼容层：转发 internal/store 导出符号。
//
// main 包仍持有 Handlers 等组装逻辑，暂以 type alias / 变量转发保持
// 现有引用零改动；阶段 B/C 将 Handlers 迁入 internal/api 后删除本文件。

import (
	"fvcc/internal/store"
)

type Store = store.Store

var (
	NewStore              = store.NewStore
	ErrProjectNameUsed    = store.ErrProjectNameUsed
	ErrRevConflict        = store.ErrRevConflict
	ErrProjectNotFound    = store.ErrProjectNotFound
	TrashDirName          = store.TrashDirName
	MoveToTrash           = store.MoveToTrash
)
