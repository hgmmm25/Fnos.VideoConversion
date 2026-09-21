---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 839b5d1d4fff15220193598838e6072d_bace8b0fb59411f183e7525400de85a5
    ReservedCode1: Doos8W1RmCRrBGJz7mRU7ksKJcD7CjfkazbSHt9J2dQgLecaCHWRrMnYD10+YIp1XGPncP8I8pD3bRdVFkNyjq2hkoa9HX249VP6a4j7mQnhNwWgnhlEjV7hgT2VcBzKaLB7UA6EQA0Cjhscj1V4TVtsybNmwYHKDqj60+jSttPRpaUTlzXCm1CrmOc=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 839b5d1d4fff15220193598838e6072d_bace8b0fb59411f183e7525400de85a5
    ReservedCode2: Doos8W1RmCRrBGJz7mRU7ksKJcD7CjfkazbSHt9J2dQgLecaCHWRrMnYD10+YIp1XGPncP8I8pD3bRdVFkNyjq2hkoa9HX249VP6a4j7mQnhNwWgnhlEjV7hgT2VcBzKaLB7UA6EQA0Cjhscj1V4TVtsybNmwYHKDqj60+jSttPRpaUTlzXCm1CrmOc=
---

# FVCC 数据面安全策略

> 依据《WebVideoEditor_Design/07-安全校验与凭据管理细则.md》补写（2026-09-21），消除 `server/main.go` 等注释对 `docs/SECURITY.md` 的悬空引用。
> 目标读者：部署运维 / 二次开发。文中"代码即真相"：以 `FVCC/server` 源码实现为准；上层传输链路（UI→FVCC HTTPS、FVCC↔FVCS WSS）见仓库根 `docs/SECURITY.md`（P1-3 数据面加密）。

## 1. 威胁模型与适用范围

| 项 | 说明 |
|---|---|
| 网络 | 可信局域网；FVCC 生产经 fnOS 网关（Unix Socket）对外，开发模式 TCP 127.0.0.1:8088 |
| 攻击者 | 局域网内非授权设备；已获得普通（非 admin）账号的用户；被篡改的前端请求 |
| 非目标威胁 | 物理接触 NAS、内核级攻击、公网暴露（默认禁止） |

对应措施总览：

| 威胁 | 措施 | 位置 |
|---|---|---|
| 越权访问接口 | 网关透传用户中间件 + `RequireAdmin` 写操作强制 admin | `internal/security/gateway.go` |
| 凭据泄露 | AES-GCM 加密落盘（主密钥 0600 权限）；任务载荷只下发 `credentialId` | `internal/security/crypto.go` |
| 暴力破解/资源耗尽 | 滑动窗口限流（登录失败 / 渲染提交 / 票据 / WS） | `internal/security/ratelimit_core.go`、`internal/api/ratelimit.go` |
| 路径穿越/越权读写 | 路径四层校验 + SMB 白名单 + EDL 结构化白名单 | `internal/edl/edl_validate.go`、`internal/security/smb_validate.go` |
| 破坏性操作无痕 | 审计落库（delete/clear/empty_trash 等） | `internal/security/audit.go` |

## 2. 网关鉴权与会话

### 2.1 网关透传用户（`internal/security/gateway.go`）

- `GatewayUserMiddleware`：解析 fnOS 网关透传的用户信息（`X-Auth-Key` 头或 Cookie 会话），注入 `gin.Context`。
- `GetGatewayUser(c)`：处理器内取当前用户；独立开发模式（无 fnOS 网关）放行，视当前用户为 admin（与既有行为一致）。
- **接口权限位必须就位**：即使独立模式全部放行，`/render`、`/proxy`、`DELETE` 类路由也一律挂 `RequireAdmin()`，避免后续接入真实网关时返工。

### 2.2 权限分级

| 操作 | 只读用户 | admin |
|---|---|---|
| 素材扫描 / 预览 / 缩略图 / `/stream` | ✅ | ✅ |
| 项目读写（`/projects*`） | ✅（P0 简化，团队共用） | ✅ |
| 提交渲染（`/render`）、生成代理（`/genproxy`） | ❌ | ✅ |
| 删除项目 / 删除任务 / 清空日志 / 清空回收站 | ❌ | ✅ |
| 节点管理与凭据管理 | ❌ | ✅ |

## 3. 凭据与敏感数据

### 3.1 加密落盘（`internal/security/crypto.go`）

- 对称加密：AES-256-GCM；主密钥以 **0600 权限**落盘 `<dataDir>/secret.key`（`LoadOrCreateSecretKey` 首次启动生成）。
- 接口：`EncryptSecret(key, plain) (string, error)` / `DecryptSecret(key, stored)`；加密结果 base64 存储，密文含随机 nonce。
- 适用：渲染节点 `Server.AuthKey`、SMB 相关敏感配置等。

### 3.2 凭据流转（对齐 07 §5）

- FVCC 侧**不持有、不传输、不记录** SMB 明文口令：任务载荷只下发 `credentialId` 档案键，密码只在渲染节点本机存在（FVCS 侧 DPAPI 档案）。
- **硬约束**：
  1. 禁止用默认值/猜测值填充凭据；缺失时必须向用户索取；
  2. 禁止凭据进日志、错误消息、进度消息、URL/查询串；
  3. 禁止将明文 secret 写入任务载荷（落库前断言剔除）。

### 3.3 日志

- `server/logs/` 含任务路径信息，按最小权限访问；生产环境建议轮转与归档。

## 4. 限流（对齐 07 §4.5）

| 目标 | 阈值 | 超限响应 | 实现 |
|---|---|---|---|
| 登录/鉴权失败 | 10 次/5min/IP | 429，冷却 15min | `ratelimit_core.go` SlidingWindowLimiter + `api/ratelimit.go` authFailLimiter |
| 渲染提交 | 30/min/用户 | 429 | renderSubmitLimiter |
| 预览票据申请 | 60/min/IP | 429 `E_RATE_LIMITED` | stream 票据中间件 |
| WS 连接 | 浏览器 ≤ 5 条 | 429 | `internal/ws/ws_limit.go` |

## 5. 审计（`internal/security/audit.go`）

| 动作 | 审计类型 |
|---|---|
| EDL 载荷校验拒绝 | `validate.reject` |
| 安全告警（突增/疑似扫描） | `security.alert` |
| 删除（视频→回收站 / 服务器 / 方案 / 任务 / 历史） | `destructive.delete` |
| 清空日志 / 清空视频缓存 | `destructive.clear` |
| 清空回收站 | `destructive.empty_trash` |

- 审计目标经 `SetAuditSink(store.AppendAudit)` 注入落库；未注入时降级日志。
- 新增破坏性操作（如快照回滚）需同步扩展审计类型。

## 6. 路径与载荷约束

### 6.1 路径校验（`internal/edl/edl_validate.go` + `internal/security/smb_validate.go`）

| 层 | 检查 | 失败 |
|---|---|---|
| L1 语法 | 非空、长度 ≤ 255、无 NUL/控制字符、无 `\`、不以 `/` 开头 | `E_EDL_INVALID` |
| L2 结构 | 每段非空且非 `.`/`..`；段数 ≤ 8；单段 ≤ 100 | `E_ASSET_NOT_IN_ROOT` |
| L3 语义 | `path.Clean` 后与原文相等；扩展名 ∈ 白名单（.mp4/.mov/.mkv/.m4v/.avi/.mxf） | `E_EDL_INVALID` |
| L4 落地 | 拼接根后 `filepath.Clean` → `EvalSymlinks` → 校验前缀含分隔符边界 | `E_ASSET_NOT_IN_ROOT` |

- `src`/`proxy`/`dest` 三根只从配置读取，**禁止**从请求参数传入任意根。
- SMB 传输模式：`ValidateSMBPath` 校验 path 位于已共享目录内。

### 6.2 EDL 载荷结构化白名单（对齐 07 §3.3）

- 体积 ≤ 256 KB；`DisallowUnknownFields`（未知字段即拒绝）；`clips ≤ 200`、单段时长下限、总时长上限；`presetKey` 服务端枚举。
- 时间码必须匹配 `^\d{2,}:[0-5]\d:[0-5]\d\.\d{3}$` 且 `out > in`；P0 禁用变速/滤镜/转场。
- 错误信息可定位但不泄露：返回 `index`/`field`，不回显完整载荷。
- 输出名：单段 + 强制 `.mp4` + 拒绝 Windows 保留设备名；重名追加 `_1.._99`，一律不覆盖既有文件。

## 7. 传输链路（引用根文档）

- UI ↔ FVCC：生产 fnOS 网关（Unix Socket + HTTPS 终止）；开发模式监听非回环地址时 `main.go` 输出 WARN 告警。
- FVCC ↔ FVCS：默认明文 WebSocket；节点配置 `useWSS` / `tlsCACert` / `tlsSkipVerify` 后走 WSS（见仓库根 `docs/SECURITY.md` §3，2026-09-18 已落地）。明文链路下不得将 FVCS 端口暴露到不可信网络。

## 8. 自测与验收

安全自测用例（S1~S15）见 07 §8，覆盖路径穿越、注入、载荷上限、权限、票据、重名、符号链接等；实现位置：`server/internal/edl/edl_validate_test.go` 与 FVCS 侧 `pkg/protocol` 测试，两侧共用同一组测试向量保证规则一致。

变更记录：2026-09-21 补写（消除 main.go:205/207 等悬空引用）。
*（内容由AI生成，仅供参考）*
