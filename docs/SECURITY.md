# FVCC / FVCS 数据面安全策略

> 依据《项目分析与改进方向.md》P1-3「数据面 HTTPS/WSS 加密」改进方向编制（2026-09-17）。
> 目标读者：部署运维 / 二次开发。文中"代码即真相"：以 `FVCC/server`、`FVCS` 源码实现为准。

## 1. 当前传输链路

| 链路 | 协议 | 现状 |
|---|---|---|
| 浏览器 UI ↔ FVCC | 生产：fnOS 网关（Unix Socket）代理；开发：TCP HTTP | 明文 HTTP/WS（局域网内） |
| FVCC ↔ FVCS（渲染节点） | WebSocket（`remote.WS_URL`） | 默认明文；节点配置 `useWSS` / `tlsCACert` / `tlsSkipVerify` 后走 WSS（2026-09-18 已支持，见 §3） |

## 2. 风险与缓解措施

### 2.1 监听地址约束（已落实：main.go `isLoopbackAddr` 告警）

- FVCC 开发模式（`--dev`）若监听非回环地址（`0.0.0.0`、局域网 IP），启动日志输出 **WARN 告警**。
- **生产环境禁止**将 FVCC 直接暴露在 TCP 上；应经 fnOS 网关（Unix Socket + 反向代理）对外提供服务，由网关负责 HTTPS 终止与鉴权。
- 确需远程调试时：仅绑定内网网卡 IP，禁止 `0.0.0.0`，配合防火墙白名单，使用后立即关闭。

### 2.2 渲染节点链路隔离（建议）

- FVCC ↔ FVCS 走 WebSocket，节点间凭据为 `Server.AuthKey`（已由 `internal/security/crypto.go` AES-GCM 加密落盘；`internal/store/model/models.go` 仍留过时注释 `TODO: P0 AES 加密`，属代码层清理待办）。
- 部署建议：
  1. 渲染节点与 FVCC 处于同一可信内网/VLAN，禁止跨公网直连；
  2. 节点间防火墙仅放行 FVCS 端口 + FVCC 管理端口；
  3. 定期轮换 `AuthKey`（到期机制 `KeyExpireAt` 已支持）；
  4. 后续升级路径：WSS（TLS）终结于反向代理，FVCC/FVCS 维持明文内部链路（见 §4）。

### 2.3 凭据与敏感数据

- 渲染节点 SMB 凭据使用 `CredentialID` 档案键（07 §5）引用，**禁止**在任务载荷/日志中写入明文口令。
- 日志（`server/logs`）含任务路径信息，按最小权限访问；生产环境建议轮转与归档。

## 3. 数据面 HTTPS/WSS 加密（P1-3）

| 项 | 状态 | 说明 |
|---|---|---|
| UI → FVCC HTTPS | 网关终结 | fnOS 网关配置 TLS 证书后自动生效，FVCC 无感知 |
| FVCC → FVCS WSS | **已落地（2026-09-18）** | FVCS 支持 `ws_tls_cert/ws_tls_key` 成对配置后 WS 端口启用 TLS；FVCC 节点支持 `useWSS/tlsCACert/tlsSkipVerify` 并以 `wss://` 拨号，缺省走系统根证书池，`tlsSkipVerify` 为显式危险开关（默认关闭） |

WSS 启用前（明文链路），不得将 FVCS 端口暴露到不可信网络。

## 4. 落地路径（已完成）

1. ✅ FVCS `pkg/config` 新增 `ws_tls_cert` / `ws_tls_key`；`pkg/server` WS 监听按配置切换 `ListenAndServeTLS`，半配置（只配其一）拒绝启动；
2. ✅ FVCC `models.Server` 新增 `useWSS` / `tlsCACert` / `tlsSkipVerify`；`remote` 按节点配置选择 `wss://`，支持 CA PEM 文件注入与系统根证书池；`tlsSkipVerify=true` 输出 WARN；
3. ✅ 握手失败走既有重连逻辑（指数退避），日志含 URL 与 TLS 错误明细，可据此区分 TLS 与业务错误；
4. ✅ `项目分析与改进方向.md` 中 P1-3 数据面条目已勾销（见 IMPROVEMENT_LOG §二）。
