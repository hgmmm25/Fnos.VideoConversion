# FVCC API 契约与命名规范（P2-4）

> 生效范围：FVCC（NAS 调度端 Web UI）REST API + SSE/WS 推送。本文件是 `ui-src/src/api.ts` 统一错误契约与 `server/` 响应写法的**唯一权威依据**；新增接口、修改错误响应必须遵循本规范。
> 版本：1.4.4 ｜ 落实日期：2026-09-18 ｜ 最后复核：2026-09-19（版本号随工作区三处同步）｜ 关联混乱报告 P2-4「统一 API 契约与命名」

---

## 1. 总则

1. **协议字段一律 camelCase**：HTTP REST 请求/响应、SSE/WS 消息的**线上字段名**使用 camelCase（`authKey`、`newPath`、`trashPath`、`taskId`、`outTimeMs`）。
2. **错误响应使用统一契约**：所有 HTTP 失败响应（含业务失败）输出 `{ok:false, code, msg, detail?}`；**禁止**再输出裸 `{"error": ...}`。
3. **内部 / 兼容段例外**：与 FVCS 渲染端对等通信的 WS 报文（`server/remote.go`）属于**内部协议段**，允许 snake_case（见 §4），但必须保持接收端双风格兼容解析。
4. **HTTP 状态码语义不变**：迁移只改变响应体形状，状态码 400/403/404/409/500 等语义保持不变。
5. **旧格式兼容由前端适配层兜底**：`ui-src/src/api.ts` `request()` 保留 `body.msg || body.error` 兜底，SSE 错误分支保留 `data.msg || data.error`。**新代码不得再依赖旧格式**。

---

## 2. 统一错误契约

### 2.1 契约形状

```jsonc
// HTTP 错误态（4xx/5xx）
{
  "ok": false,
  "code": "E_BAD_REQUEST",   // 稳定机器码，前端 ApiError.code 使用
  "msg": "路径不能为空",       // 面向用户的可读信息
  "detail": { "path": "/data/a.mp4" }  // 可选，额外上下文（校验详情等）
}

// HTTP 200 业务失败态（如 testServer 连通性测试失败）
{
  "ok": false,
  "code": "E_SERVER_OFFLINE",
  "msg": "dial tcp ...: connect: connection refused"
}
```

### 2.2 新旧契约对照

| 形态 | 旧（已废弃） | 新（唯一合法） | 说明 |
|---|---|---|---|
| HTTP 错误态 | `c.JSON(400, gin.H{"error": "..."})` | `fail(c, 400, "...")` | `code` 按状态码映射，`msg` 保留原文案 |
| HTTP 200 业务失败 | `c.JSON(200, gin.H{"ok": false, "error": "..."})` | `failWithCode(c, 200, "E_SERVER_OFFLINE", "...")` | 显式业务码 |
| SSE/WS 错误消息 | `{"type":"error","error":"..."}` | `{"type":"error","code":"E_SCAN_FAILED","msg":"..."}` | `type` 保留，`error` 拆为 `code+msg` |
| 前端兼容兜底 | `body.error` / `data.error` | `body.msg \|\| body.error` / `data.msg \|\| data.error` | 旧格式过渡期兜底，新代码只读 `msg` |

### 2.3 code 映射表

后端 `server/apierr.go` 的 `fail()` 按 HTTP 状态码自动映射 code；`failWithCode()` 用于显式业务码。

| HTTP 状态码 | code | 典型场景 |
|---|---|---|
| 400 | `E_BAD_REQUEST` | 参数错误、路径为空、路径解析失败 |
| 401 | `E_UNAUTHORIZED` | 未认证（网关/限流域） |
| 403 | `E_FORBIDDEN` | 未授权目录、非 SMB 共享访问、回收站越权 |
| 404 | `E_NOT_FOUND` | 文件/服务器/回收站条目不存在 |
| 409 | `E_CONFLICT` | 恢复目标已存在同名文件 |
| 429 | `E_RATE_LIMITED` | 限流（ratelimit 域） |
| 500 | `E_INTERNAL` | 内部错误（重命名/移动/删除/恢复失败等） |
| 200（业务失败） | 显式业务码 | `E_SERVER_OFFLINE`（testServer）等 |

### 2.4 域内既有 helper（同一契约，勿重复造轮子）

以下 helper 已输出同一契约形状，**保留使用**，新代码优先复用：

- `server/apierr.go`：`fail(c, status, msg, detail...)` / `failWithCode(c, status, code, msg, detail...)` —— **默认选择**。
- `server/handlers_edl.go`：`edlErr(c, status, code, msg, detail)` —— EDL 域，附带审计记账副作用。
- `server/stream.go`：`streamErr(c, status, code, msg)` —— 预览网关域。
- `server/gateway.go`：`{ok:false, code, msg}` 内联 —— 网关鉴权域（必要时可迁至 `failWithCode`）。

---

## 3. 命名规范

### 3.1 协议层（REST + SSE/WS 线上字段）—— camelCase

| 类别 | 规范 | 示例 |
|---|---|---|
| 请求/响应字段 | camelCase | `authKey`、`newPath`、`trashPath`、`taskId`、`serverId`、`profileId` |
| 复合词 | 每个单词首字母大写（除首词） | `outTimeMs`、`chunkSize`、`sourceRoot`、`maxConcurrent` |
| 前端 TS 接口字段 | 与线上 json tag 完全同名 | `TrashItem.path/origPath/size/modTime` |
| 错误契约字段 | 固定 `ok/code/msg/detail`（小写） | — |

Go 结构体定义示例：

```go
type VideoInfoCache struct {
	FileID     string `json:"fileId"`    // camelCase 线上名
	OutTimeMs  int64  `json:"outTimeMs"` // 禁止 out_time_ms
}
```

### 3.2 内部 / 兼容段 —— snake_case 边界（重要）

以下场景**允许** snake_case，且只允许出现在这些场景：

1. **FVCS 对等 WS 报文**（`server/remote.go`：`progressPush`、`helloPush` 等）：与 FVCS 渲染端既有协议对齐，线上字段为 `task_id` / `out_time_ms` / `seg_total` / `server_id` / `cpu_cores` 等。**接收端必须双风格兼容**：`parseHelloCaps` 等解析函数逐字段合并 camelCase / snake_case，camelCase 优先、缺失回落 snake_case——新增字段时同步补两侧解析，禁止只读一侧。
2. **持久化存储字段**：`store.go` JSON 落盘文件属内部数据格式，可沿用既有命名；但**暴露给前端的字段**必须转换/声明为 camelCase。
3. **第三方/上游协议透传**（如 ffprobe JSON、FVCS 状态回传的 `ErrorCode/ErrorMessage`）：保持上游原始命名，不做改写。

**判断口诀**：凡字段会被 `fetch` / `WebSocket` 送到浏览器，一律 camelCase；凡字段只在 Go 进程间或落盘 JSON 内流转，可按所属协议段选择，但必须在注释中标注"内部协议"。

---

## 4. 实现约定（Go）

```go
// ✅ 新写法
if err := validate(); err != nil {
    fail(c, http.StatusBadRequest, "路径不能为空")          // 自动映射 E_BAD_REQUEST
    return
}
if err := h.moveToTrash(path); err != nil {
    fail(c, http.StatusInternalServerError, "删除失败: "+err.Error())
    return
}
failWithCode(c, http.StatusOK, "E_SERVER_OFFLINE", err.Error()) // HTTP 200 业务失败

// ❌ 已废弃写法（禁止新增）
c.JSON(400, gin.H{"error": "..."})
c.JSON(200, gin.H{"ok": false, "error": "..."})
json.Marshal(gin.H{"type": "error", "error": "..."})
```

**SSE/WS 错误消息**统一为 `{"type":"error","code":"<E_XXX>","msg":"..."}`，`type` 字段保留（前端按 `type` 分发），`error` 键不再输出。

---

## 5. 前端适配层约定（ui-src/src/api.ts）

1. `request<T>()`：仅 `!resp.ok` 时抛 `ApiError`；`msg = body.msg || body.error || resp.statusText` 为**兼容兜底**，新接口依赖 `body.msg`。
2. HTTP 200 业务失败（`ok:false`）不抛错，由页面读 `r.ok` / `r.msg` 判断。
3. SSE 错误分支读取 `data.msg || data.error`；页面层禁止直接读 `data.error`。
4. 新增接口返回类型必须与后端 json tag 同名同义（camelCase）。

---

## 6. 验收标准

- [ ] 全仓 grep `gin.H{"error"` 结果为 0（`server/` 下）；
- [ ] 全仓 grep `{"type": "error", "error"` 结果为 0（SSE/WS 错误消息）；
- [ ] `go build ./...`、`go vet ./...`、`go test -short ./...` 通过；
- [ ] `npx tsc --noEmit` 通过；
- [ ] 新接口错误响应输出 `{ok:false, code, msg}`，禁止裸 `{error}`。
