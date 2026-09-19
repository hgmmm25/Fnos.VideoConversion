package main

// P2-1 B轮：node 域收敛到 internal/node 后的根包兼容层。
// 本文件把 internal/node 导出符号转发为根包原名，使根包调用点与既有测试引用点零改动；
// 阶段 C 迁入 api 层后随接口收口内联删除。

import (
	"fvcc/internal/node"
)

// 错误转发（调度域 classifyRenderError / QUEUE 保持判断使用）。
var errNoSelectableNode = node.ErrNoSelectableNode
