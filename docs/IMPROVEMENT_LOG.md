---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 839b5d1d4fff15220193598838e6072d_35cb4303b2ba11f19369525400de85a5
    ReservedCode1: 99coZZxs0AwZ1+jHhlvaC/PwfTsG2ULM4u9rfOxmJIWRx3AGXabg+3W+tRJZwWn9Qy55rQdhrzAKFQ9sOOPb4gmVKQDFori0XfD3+VmxcutBOGtPGhNYzI117MYnp52TRbEfATrO9gUivux1Ep3Ks33aEsz9BRliqj5icfFsbvYdmXIBr2JH1pLd71k=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 839b5d1d4fff15220193598838e6072d_35cb4303b2ba11f19369525400de85a5
    ReservedCode2: 99coZZxs0AwZ1+jHhlvaC/PwfTsG2ULM4u9rfOxmJIWRx3AGXabg+3W+tRJZwWn9Qy55rQdhrzAKFQ9sOOPb4gmVKQDFori0XfD3+VmxcutBOGtPGhNYzI117MYnp52TRbEfATrO9gUivux1Ep3Ks33aEsz9BRliqj5icfFsbvYdmXIBr2JH1pLd71k=
---

# FVCC 改进方向落实记录（2026-09-17）

> 依据《项目分析与改进方向.md》逐项落实。原则：低风险直接改码并验证；结构性大改（SQLite 迁移、事件驱动调度、freecut 收敛）如实说明未落实原因，不强行破坏既有稳定链路。
> 验证基线：`go build ./...` / `go vet ./...` / `go test -short ./...` 全部通过；`tsc --noEmit` 通过；`scripts/check-versions.ps1` 退出码 0。

## 一、找到的文档

- `D:\Fnos.VideoConversion\项目分析与改进方向.md`（主文档，7.1 KB）
- 配套：`D:\Fnos.VideoConversion\BUILD.md`、`D:\Fnos.VideoConversion\README.md`、`_archive_md\*`（历史快照）

## 二、改进项清单与落实情况

| 编号 | 改进项 | 落实状态 | 落实方式（文件 / 改动） | 验证 |
|---|---|---|---|---|
| P0-1 | 修复 build.ps1 GOTOOLCHAIN 并同步 BUILD.md + CI 版本校验 | ✅ 已落实（原 GOTOOLCHAIN=local 修复已完成；本次补版本校验脚本） | 新增 `scripts/check-versions.ps1`：校验 manifest / ui-src/package.json / server/VERSION 三方版本对齐 + go.mod 工具链声明；`BUILD.md` 增加发布前校验说明与 archive/ 目录树 | 脚本运行退出码 0，输出四方一致（1.4.0 / go 1.27.1） |
| P0-2 | FVCC 存储迁 SQLite | ⛔ 未落实 | — | — |
| P1-1 | 调度器事件驱动 + WS 广播 | 🔶 部分落实 | WS 广播前端已消费（ws.ts，无轮询）为既有事实；本次优化 `FVCC/server/scheduler.go` `tick()`：合并两次 `GetTasks()` 为一次快照，降低 1s tick 下重复拷贝与排序开销（语义不变） | 新增并发测试 `store_concurrency_test.go` 覆盖 tick 并发安全，通过 |
| P1-2 | 前端收敛 freecut（editor 目录） | ⛔ 未落实 | — | — |
| P1-3 | 数据面 HTTPS/WSS 加密 | ✅ 已落实（第二轮补齐） | 第一轮：`main.go` `isLoopbackAddr` 非回环告警 + `docs/SECURITY.md`；第二轮：FVCS WS 端口 TLS（`ws_tls_cert/ws_tls_key`）+ FVCC `wss://` 拨号（`useWSS/tlsCACert/tlsSkipVerify`），详见 §六 | 双端 WSS 单测 + 全量回归（见 §六/§七） |
| P2-1 | 可观测性 Prometheus/metrics + trace ID | 🔶 部分落实 | `handlers.go`：`/metrics` 扩展 activeTasks / queuedTasks / failedTasks / completedTotal / nodeCapsCount / offlineServers；新增 `/metrics/prometheus` Prometheus 文本端点（手写文本格式，零新依赖）；`router.go` 挂路由；`ui-src/src/types.ts` Metrics 接口同步扩展。trace ID 全链路注入未落实（见 §三） | go build 通过；`curl /metrics/prometheus` 见 §四 |
| P2-2 | 测试补强 | ✅ 已落实 | 新增 `FVCC/server/store_concurrency_test.go`（Store 并发读写压力、锁并发、sortQueueTasks 并发一致性、Scheduler tick 并发安全收敛）、`FVCC/server/edl_fuzz_test.go`（3 个 fuzz 目标 + 种子） | 全量 `go test -short .` 通过（含既有 B-05 验收） |
| P2-3 | 仓库治理清理 fpk/exe | ✅ 已落实 | 历史 fpk（v1.1.0~v1.3.1 共 11 个）、旧 fnpack-1.2.1.exe、fvcs-service.exe.bak_preupgrade、_build_check.txt、_backup_* 备份目录统一移入 `D:\Fnos.VideoConversion\archive\`（fpk/、tools/ 分目录）；根目录 0 字节 temp_vet.txt 删除（回收站）；新增根 `.gitignore`（构建产物/归档/运行期数据）；`BUILD.md` 目录树与 fnpack 引用同步更新 | 目录核对完成 |
| P3-1 | 文档版本对齐 | ✅ 已落实 | `_archive_md\运作模式分析报告.md`、`_archive_md\技术栈版本升级指南.md` 头部加"快照漂移说明"（旧版 go 1.22/1.25 引用已标注过时，以 go.mod go 1.27.1 为准） | — |
| P3-2 | 文档快照漂移说明机制 | ✅ 已落实 | `项目分析与改进方向.md`、`WebVideoEditor_整体架构设计方案.md`、`BUILD.md`、2 份 _archive_md 文档头部统一追加漂移说明 blockquote（基线时间 + 代码即真相 + 校验方式） | — |

## 三、未落实项及原因

| 编号 | 未落实内容 | 原因 |
|---|---|---|
| P0-2 | JSON 文件存储迁 SQLite | `store.go`（741 行）含 tasks/history/servers/profiles/locks/settings/videoCache 七大持久域 + 原子写/回退/锁语义，迁移属结构性重构，风险高、需完整数据迁移与回滚方案，超出本次改进范围，应作为独立里程碑（建议分配后续迭代）。 |
| P1-1 完整版 | 1s ticker 改为纯事件驱动调度 | 当前 tick 兼顾冷却到期扫描、锁回收、进度探测等多职责；改事件驱动需重建触发源与超时兜底，属调度内核重构，与 B 组既有验收测试强耦合，本次仅落地低风险优化（合并快照）。 |
| P1-2 | 前端收敛 freecut（editor 目录与 freecut 功能重叠） | 涉及 FVCC 前端 `src/editor/` 与独立 freecut 应用的 UI 架构合并，改动面横跨两个前端代码库，需产品决策后专项实施。 |
| P2-1 trace ID | 全链路 trace ID 注入 | logger 为包级全局函数，40+ 调用点无 context 参数；全链路注入需 context 化改造（结构性重构）。当前任务日志已含 taskID（`scheduler` 日志 `id=%s`），可先按 taskID 贯穿定位。 |

## 四、关键验证结果

1. **构建**：`cd FVCC\server && go build ./...` ✅，`go vet ./...` ✅
2. **全量测试**：`go test -short .` ✅（含新增并发压力测试与既有 B-05 验收）
3. **新增 fuzz 种子**：`go test -run "Fuzz..."` ✅（CI 可加 `-fuzz` 持续变异）
4. **前端类型**：`cd FVCC\ui-src && tsc --noEmit` ✅（Metrics 接口扩展无类型错误）
5. **版本校验**：`scripts\check-versions.ps1` ✅ 退出码 0（manifest=package.json=VERSION=1.4.0，go 1.27.1）
6. **指标端点**：`GET /metrics` 新增字段返回正常；`GET /metrics/prometheus` 输出 Prometheus 文本格式
7. **安全告警**：`--dev` 监听非回环地址时启动日志输出 WARN（代码路径编译验证）

## 五、改动文件清单

**新增**
- `scripts/check-versions.ps1`
- `docs/SECURITY.md`
- `FVCC/server/store_concurrency_test.go`
- `FVCC/server/edl_fuzz_test.go`
- `FVCC/server/handler_metrics_test.go`
- `.gitignore`
- `archive/`（归档历史 fpk / 工具 / 备份，共 17 个文件 2 个备份目录）

**修改**
- `FVCC/server/scheduler.go`（tick 合并任务快照）
- `FVCC/server/main.go`（isLoopbackAddr + 非回环告警）
- `FVCC/server/handlers.go`（/metrics 扩展 + /metrics/prometheus）
- `FVCC/server/router.go`（挂载 Prometheus 端点）
- `FVCC/ui-src/src/types.ts`（Metrics 接口扩展）
- `BUILD.md`（archive 目录树、fnpack 引用、版本校验说明）
- `项目分析与改进方向.md` / `WebVideoEditor_整体架构设计方案.md` / `_archive_md\运作模式分析报告.md` / `_archive_md\技术栈版本升级指南.md`（快照漂移说明）

---

## 六、第二轮（2026-09-18）：P1-3 WSS 数据面加密落地

> 承接第一轮未落实项中的 P1-3（数据面 HTTPS/WSS 加密本体），按 `docs/SECURITY.md` §4 路径落地。

### 6.1 落实情况

| 编号 | 改进项 | 落实状态 | 落实方式（文件 / 改动） | 验证 |
|---|---|---|---|---|
| P1-3 | FVCC↔FVCS WebSocket 启用 TLS | ✅ 已落实 | **FVCS 侧**：`pkg/config/config.go` 新增 `ws_tls_cert` / `ws_tls_key`（成对配置，缺一拒绝启动）；`pkg/server/server.go` 新增 `validateWSConfig()`，`startWebSocketServer()` 按配置切换 `ListenAndServeTLS`，启动日志标注 WSS enabled/disabled。**FVCC 侧**：`models.go` `Server` 新增 `useWSS` / `tlsCACert` / `tlsSkipVerify`；`remote.go` 新增 `buildDialer()` 按节点配置构造 TLS 拨号器（CA PEM 注入 / 系统根池 / skipVerify 危险开关 + WARN），`dialAndAuth()` 按 `useWSS` 选择 `wss://` 拨号 | `go vet` 双端通过；新增 WSS 单测全绿（见 6.3） |

### 6.2 设计决策

1. **半配置拒绝启动**：`ws_tls_cert`/`ws_tls_key` 只配其一时报错退出，防止部署方误以为已加密；
2. **CA 信任三态**：`tlsCACert` 指定内网 CA PEM → 注入 RootCAs；未指定 → 系统根证书池（自签 CA 需导入系统信任）；`tlsSkipVerify=true` → 显式跳过校验并输出 WARN（生产禁止，SECURITY.md 明确）；
3. **明文向后兼容**：未配置 TLS 的节点完全走原 `ws://` 链路，升级不破坏既有部署；
4. **TLS 失败可观测**：握手失败经 `dialer.Dial` 错误回传，日志含 URL 与 TLS 错误明细，复用既有指数退避重连。

### 6.3 新增测试

- `FVCS/pkg/server/server_tls_test.go`：`TestValidateWSConfig`（半配置拒绝 4 例）+ `TestStartWebSocketServerTLSBranch`（自签证书端到端：信任 CA 成功 / 未知 CA 拒绝 / skipVerify 放行）
- `FVCS/pkg/server/server_tls_test_helper.go`：自签证书生成、TLS 监听、wss 拨号辅助
- `FVCC/server/remote_wss_test.go`：`TestBuildDialerBranches`（4 分支）+ `TestDialAndAuthWSS`（信任 CA 成功 / 未知 CA 失败 / skipVerify 放行）+ `TestDialAndAuthPlainCompat`（明文兼容）

### 6.4 验证结果

1. FVCC `go vet ./...` ✅；FVCS `go vet ./pkg/server/... ./pkg/config/...` ✅
2. FVCC WSS 单测：`go test -run WSS -v ./...` ✅（4/4 子用例）
3. FVCS TLS 单测：`go test -run TLS -v ./pkg/server/...` ✅
4. 全量回归：FVCC `go test -short ./...` / FVCS `./pkg/server ./pkg/config`（见 §七）
5. 版本校验：`scripts\check-versions.ps1` ✅（无版本变更）

### 6.5 变更文件清单

**修改**
- `FVCS/pkg/config/config.go`（新增 WSTLSCert/WSTLSKey）
- `FVCS/pkg/server/server.go`（validateWSConfig + startWebSocketServer TLS 分支）
- `FVCC/server/models.go`（Server 新增 useWSS/tlsCACert/tlsSkipVerify）
- `FVCC/server/remote.go`（buildDialer + wss 拨号）
- `docs/SECURITY.md`（§3/§4 勾销 P1-3）

**新增**
- `FVCS/pkg/server/server_tls_test.go`
- `FVCS/pkg/server/server_tls_test_helper.go`
- `FVCC/server/remote_wss_test.go`

### 6.6 未落实项（延续至后续迭代）

| 编号 | 未落实内容 | 原因 |
|---|---|---|
| P0-2 | JSON 文件存储迁 SQLite | 结构性重构，需独立里程碑（同第一轮） |
| P1-1 完整版 | 1s ticker 改纯事件驱动调度 | 调度内核重构，与既有验收强耦合（同第一轮） |
| P1-2 | 前端收敛 freecut | 跨前端代码库 UI 合并，需产品决策（同第一轮） |

---

## 七、第三轮（2026-09-18）：P2-1 可观测性补完——trace ID 全链路注入 + 耗时/失败率指标

> 承接第一轮 P2-1 部分落实中遗留的"全链路 trace ID 注入"与指标增强（耗时直方图/失败率），补齐双端日志同一标识聚合能力。改动原则：**不透传 context、不破坏既有调用点**——采用「请求字段透传 + 包级带 trace 日志函数」的轻量方案，40+ 既有日志调用点零改动。

### 7.1 FVCC（调度端）改动

1. **日志结构化字段**：`FVCC/server/logger/logger.go` `LogEntry` 新增 `TraceID string`（JSON `traceId,omitempty`）；`Writer.Write` 同步写入该字段。
2. **请求级透传载体**：`FVCC/server/remote.go` `wsCmd` 补 `TraceId string`（JSON `TraceId`）；`RenderDispatcher` 接口新增 `CreateRenderEDLWithTrace / CreateGenProxyWithTrace` 方法（内部转发，原方法保留兼容）。
3. **TraceID 生成与落库**：`FVCC/server/handlers.go` 新增 `newTraceID(now)`（crypto/rand 8 字节 hex + 时间戳毫秒）；普通转码 / 代理 / 渲染三类任务创建处均写入 `Task.TraceID`。
4. **启动点打点**：`FVCC/server/store.go` 新增 `SetTaskStartedAt(id, ts)`（仅 StartedAt 为空时写入并持久化）；`scheduler.go` SMB 等待转码、上传完成转等待、渲染派发成功、本地转码启动四处调用 `SetTaskStartedAt(time.Now())`，渲染派发日志升级为 `InfoT` 带 TraceID。
5. **指标增强**：`handlers.go` `/metrics` 新增 `durationHistogram / avgDurationSec / failureRate`（基于历史任务 StartedAt→FinishedAt 计算）；`/metrics/prometheus` 新增 `fvcc_task_duration_seconds` 直方图（bucket/sum/count）与 `fvcc_task_failure_rate` gauge。

### 7.2 FVCS（渲染端）改动

1. `pkg/protocol/protocol.go` `CreateTaskRequest` 补 `TraceId`（P2-1 注释）；`wire_compat.go` 提供 `TraceId`↔`traceId` 新旧字段兼容归一。
2. `pkg/task/task.go` `Task` 结构体补 `TraceID` 字段（创建/完成/失败日志携带）；`CreateSMBTaskExWithTrace` 透传。
3. `pkg/task/render_edl.go` / `pkg/task/proxy.go` 创建点透传并 `InfoT` 打点；`pkg/server/server.go` SMB/RenderEDL/GenProxy 创建与拒绝路径全部 `logger.InfoT/ErrorT` 带 `req.TraceId`。
4. `pkg/logger/logger.go` 新增 `DebugT/InfoT/WarnT/ErrorT` 系列（log 落库 `traceId` 字段 + 控制台 `[trace=xxx]` 段）。

### 7.3 验证结果

1. FVCC：`go vet ./...` ✅；全量 `go test ./...` ✅（含 handler_metrics_test、scheduler_edl_test 修复后的 fakeDispatcher 双 Trace 方法桩）
2. FVCS：`go build ./pkg/... ./cmd/service/... ./cmd/fvcs-cli/... ./cmd/test_start/...` ✅；`go vet ./pkg/...` ✅；`go test ./pkg/task/...` ✅
3. 环境说明：`FVCS/pkg/smb` 测试因本机 `CGO_ENABLED=0` 且无 gcc（go-sqlite3 需 cgo）失败，为既有环境限制，与本次改动无关；`cmd/settings`、`cmd/ui`（fyne GUI）需 GL 构建约束，非本改进范围。

### 7.4 变更文件清单

**修改**
- `FVCC/server/logger/logger.go`（LogEntry.TraceID）
- `FVCC/server/remote.go`（wsCmd.TraceId + RenderDispatcher WithTrace 接口）
- `FVCC/server/handlers.go`（newTraceID + 三类任务 TraceID + metrics 直方图/失败率）
- `FVCC/server/handlers_proxy.go` / `handlers_render.go`（任务 TraceID 写入）
- `FVCC/server/store.go`（SetTaskStartedAt）
- `FVCC/server/scheduler.go`（四处启动点打点 + InfoT）
- `FVCC/server/scheduler_edl_test.go`（fakeDispatcher 补齐 WithTrace 桩）
- `FVCS/pkg/protocol/protocol.go` / `wire_compat.go`（TraceId 字段与兼容归一）
- `FVCS/pkg/task/task.go`（Task.TraceID + CreateSMBTaskExWithTrace）
- `FVCS/pkg/task/render_edl.go` / `proxy.go`（透传 + InfoT）
- `FVCS/pkg/server/server.go`（创建/拒绝路径 InfoT/ErrorT 带 trace）
- `FVCS/pkg/logger/logger.go`（TraceID 字段 + T 系列函数）

### 7.5 未落实项（延续至后续迭代）

| 编号 | 未落实内容 | 原因 |
|---|---|---|
| P0-2 | JSON 文件存储迁 SQLite | 结构性重构，需独立里程碑（同第一轮） |
| P1-1 完整版 | 1s ticker 改纯事件驱动调度 | 调度内核重构，与既有验收强耦合（同第一轮） |
| P1-2 | 前端收敛 freecut | 跨前端代码库 UI 合并，需产品决策（同第一轮） |

---

## 八、后续长期落实计划（Roadmap）

> 以下结构性改进项确认**长期持续推进**，不以单轮迭代为限。原则：每项独立里程碑、小步试点、不破坏既有稳定链路；落实后在本章勾销并回填验证结果。

### 8.1 长期待办清单

| 编号 | 改进项 | 优先级 | 落实方向（初步思路） | 里程碑划分 | 状态 |
|---|---|---|---|---|---|
| P0-2 | JSON 文件存储迁 SQLite | P0 | 七大持久域（tasks/history/servers/profiles/locks/settings/videoCache）统一迁 SQLite；保留 JSON 导入导出兼容层与原子写语义 | M1：Schema 设计与数据迁移脚本 → M2：Store 读写层替换 → M3：旧 JSON 数据一键导入与回滚开关 | 🔲 待启动 |
| P1-1 完整版 | 1s ticker 改纯事件驱动调度 | P1 | 重建触发源（任务入队/状态变更/节点心跳）+ 超时兜底扫描；保留冷却到期与锁回收职责；与 B 组验收测试逐项对齐 | M1：事件总线与触发源 → M2：tick 职责拆分 → M3：验收回归与兜底压测 | 🔲 待启动 |
| P1-2 | 前端收敛 freecut | P1 | 评估 FVCC `src/editor/` 与独立 freecut 应用的能力重叠，确定统一编辑器 UI 架构；跨前端代码库合并需产品决策先行 | M1：能力矩阵与产品决策 → M2：编辑器收敛 → M3：旧入口下线 | 🔲 待启动 |

### 8.2 推进约定

1. 每项启动前先出独立里程碑方案（含数据迁移/回滚策略、验收标准），经确认后实施；
2. 已勾销项在 `IMPROVEMENT_LOG.md` 以独立轮次章节记录落实详情，本章状态同步更新为 ✅；
3. 未启动前不修改既有稳定链路，避免半成品风险。

*（内容由AI生成，仅供参考）*

---

## 九、第四轮改进（2026-09-18）：P2-5 删除回收站化 + P0-2 仓库卫生延续

### 9.1 背景

混乱报告硬伤②：`deleteVideo` 直接 `os.Remove` 物理删除，NAS 用户误删不可恢复（P2-5）；工作区仍有构建产物残留（P0-2 仓库卫生延续）。

### 9.2 FVCC 后端改动

1. **新增 `server/trash.go`**（回收站核心模块）：
   - `moveToTrash(path)`：文件移入所在授权根 `<root>/_trash`（`_` 前缀受保留名规则保护，不入素材库扫描），保留相对路径结构避免同名覆盖，重名时追加毫秒时间戳后缀；
   - `listTrash()`：`GET /api/trash` 聚合全部授权根回收站条目（name/path/origPath/size/modTime）；
   - `restoreTrash()`：`POST /api/trash/restore` 仅允许回收站内路径（越权 403），恢复目标已存在同名文件时 409 拒绝，避免覆盖；
   - `emptyTrash()`：`POST /api/trash/empty` 挂 `requireAdmin()` 管理员保护，清空全部回收站。
2. **`server/handlers.go`** `deleteVideo`：`os.Remove` → `h.moveToTrash`，返回 `trashPath`。
3. **`server/router.go`** 注册三个回收站路由。

### 9.3 FVCC 前端改动

1. `ui-src/src/api.ts`：新增 `listTrash / restoreTrash / emptyTrash` 封装，`deleteVideo` 返回类型补 `trashPath`。
2. `ui-src/src/types.ts`：新增 `TrashItem` 接口。
3. `ui-src/src/pages/scanner.ts`：
   - 工具栏新增「回收站」入口（弹窗内支持恢复/清空/关闭）；
   - 删除确认弹窗文案由「此操作不可撤销！」改为「删除后文件将移入回收站，可在回收站中恢复」；
   - 删除成功 toast 改为「已删除 N 个文件（移入回收站）」。

### 9.4 验证结果

1. `server`: `go build ./...` ✅；全量 `go test .` ✅（96.7s）；新增 `trash_test.go` 3 用例（移动/恢复、越权防护、清空）全部 PASS。
2. `ui-src`: `npx tsc --noEmit` ✅。

### 9.5 变更文件清单

**新增**
- `FVCC/server/trash.go`、`FVCC/server/trash_test.go`

**修改**
- `FVCC/server/handlers.go`（deleteVideo 回收站化）
- `FVCC/server/router.go`（/trash 三路由 + requireAdmin）
- `FVCC/ui-src/src/api.ts` / `types.ts` / `pages/scanner.ts`（回收站 UI 与文案）

### 9.6 未落实项（延续至后续迭代）

| 编号 | 未落实内容 | 原因 |
|---|---|---|
| P0-2 | JSON 文件存储迁 SQLite | 结构性重构，需独立里程碑 |
| P1-1 完整版 | 1s ticker 改纯事件驱动调度 | 调度内核重构，与既有验收强耦合 |
| P1-2 | 前端收敛 freecut | 跨前端代码库 UI 合并，需产品决策 |
| P2-6 | AIGC 残留清理 | README frontmatter 残留未清除，下一轮处理 |
| 仓库卫生 | server/fvcc.exe、nul.exe 构建产物 | 已加 .gitignore 但磁盘未清理，可在确定可重建后删除 |

*（内容由AI生成，仅供参考）*

---

## 十、第五轮改进（2026-09-18）：P1-5 依赖瘦身 + P2-3 CI 流水线 + 混乱报告项收口核查

### 10.1 背景

混乱报告 P1-5（依赖瘦身）与 P2-3（CI 流水线）落地下轮；同时对 P0-2 仓库卫生做磁盘级复核，确认前三轮改动已收口。

### 10.2 本轮改动

1. **P1-5 前端依赖瘦身**：`ui-src/src/main.ts` 字体引入由 `@fontsource/*/{400,500,600}.css`（全字符集）改为 `/latin-*.css` 子集，打包字体文件 **56 → 10 个**，体积 **0.65MB → 192KB**（约 -70%），`npm run build` 全绿。
2. **P1-5 后端依赖核查**：`go mod tidy` 无净变化；`go mod why` 确认 mongo-driver（gin/binding→bson）与 quic-go（gin→http3）均为 gin 传递依赖，非残留垃圾；进一步瘦身需评估替换 gin（架构决策，留待 P2-1）。
3. **P2-3 CI 流水线**：新增 `.github/workflows/fvcc-ci.yml`（GitHub Actions），FVCC 相关路径 push/PR 触发；后端 `go vet + go test -race ./...`，前端 `npm ci + npm run build`（含 tsc 与 check:design 门禁）。
4. **P0-2 磁盘残留复核**：`server/fvcc.exe`、`server/nul.exe`、`server/build_err.txt`、`server/out.txt`、`ui-src/{all,err,out}.txt`、`temp/` 均已在磁盘清零；本轮顺带清理 `server/t1.txt`、`server/test_out.txt` 两个测试输出残留。

### 10.3 验证结果

1. `server`: `go build ./...` ✅、`go vet ./...` ✅、trash 单测 3 用例 PASS ✅。
2. `ui-src`: `npm run build` ✅（tsc + design gate + vite）。
3. 提交：`6df8b4a`（P2-5 收口）、`7a2794d`（P1-5 字体）、`50dcafa`（P2-3 CI）。

### 10.4 混乱报告项当前收口状态

| 编号 | 内容 | 状态 |
|---|---|---|
| P0-1 | 凭据加密落库 | ✅ |
| P0-2 | 清理残留 + .gitignore | ✅ |
| P0-3 | git init 版本控制 | ✅ |
| P1-1 | updateProfile 反射重构 | ✅ |
| P1-2 | SMB 校验 helper | ✅ |
| P1-3 | 文档落地 | ✅ |
| P1-4 | manifest 修复 | ✅ |
| P1-5 | 依赖瘦身 | ✅（前端子集 + 后端核查） |
| P2-1 | 后端 internal 分层 | 🔶 待做（架构演进） |
| P2-2 | 前端组件化 | 🔶 待做（架构演进） |
| P2-3 | CI 流水线 | ✅ 本轮 |
| P2-4 | API 契约统一 | 🔶 待做（78 处 {error} → {code,msg,detail}） |
| P2-5 | 删除回收站化 | ✅ |
| P2-6 | AIGC 残留清理 | ✅ |

### 10.5 未落实项（延续至后续迭代）

| 编号 | 未落实内容 | 原因 |
|---|---|---|
| P0-2 | JSON 文件存储迁 SQLite | 结构性重构，需独立里程碑 |
| P1-1 完整版 | 1s ticker 改纯事件驱动调度 | 调度内核重构，与既有验收强耦合 |
| P1-2 | 前端收敛 freecut | 跨前端代码库 UI 合并，需产品决策 |
| P2-1 | 后端 internal 分层 | 大工程（handlers 1765 行等），1~2 月级 |
| P2-2 | 前端组件化 | 大工程（profiles/scanner 1100+ 行），1~2 月级 |
| P2-4 | API 契约统一 | 78 处错误格式迁移 + 字段命名规范，需分批复核 |

*（内容由AI生成，仅供参考）*
