# FVCC 任务编号与章节引用对照表（TASK_REFERENCE）

> 用途：混乱报告 P2-6「清理任务编号注释」的落地物。代码注释中散落的 `B-xx / C-xx / D-xx / M4 / Px-x / 修复①-⑤` 任务编号与 `03 §4.2` 式章节引用，在本表中统一登记为可读语义，供维护者追溯设计依据。
> 约定：新增注释请直接书写可读语义，编号仅作括号溯源；本表与代码同仓维护，`docs/` 为章节引用文档的权威位置说明。
> 维护日期：2026-09-20（第四版；同步混乱报告第四版 1.4.7：P2-1/P2-2 持续收尾、P0-1 过时注释已清、前端图示化落地）

---

## 1. 编号体系总览

| 体系 | 来源 | 使用范围 | 语义 |
|---|---|---|---|
| `Px-x` | 改进方向清单 / 混乱度评价报告（P0/P1/P2 优先级） | server 与 ui-src **两套不同清单**（见 §2 分离说明） | 改进任务编号 |
| `B-xx` | 后端调度/EDL 领域任务批次（B 组） | server/ | 后端功能批次 |
| `C-xx` | 前端剪辑页任务批次（C 组） | ui-src/src/ | 前端页面批次 |
| `D-xx` | 安全类任务（D 组） | server/ | 安全加固批次 |
| `M4` | 预览网关与代理工作流模块 | server/ | 模块/里程碑编号（其余 M 编号均为前端 SVG path 误报） |
| `修复①-⑤` | 2026-09-16 剪辑页缺陷修复批次 | server 与 ui-src | 缺陷修复标记 |
| `nn §x.y` | WebVideoEditor_Design 规格文档章节 | server 与 ui-src | 规格文档引用（文档位置见 §4） |

---

## 2. P 组编号（注意：前后端为两套独立清单）

### 2.1 后端 P 组（源自《项目分析与改进方向.md》/ 混乱报告 P0~P2）

| 编号 | 可读语义 | 代表位置 |
|---|---|---|
| P0-1 | 凭据安全：AuthKey/SMBPassword 加密落库 + 响应不回显明文（internal/store/model/models.go 过时 TODO 已随 1.4.7 清理） | server/internal/security/crypto.go、internal/store/store.go、internal/api/handlers.go |
| P1-1 | updateProfile 59 块手写字段映射收敛（反射白名单 helper applyFields） | server/internal/protocol/applyjson.go、internal/scheduler/scheduler.go（tick 合并快照） |
| P1-2 | SMB 共享路径校验收敛为 helper（doScanDirectory/probeVideo/browseDirs/createTask 四处复用） | server/internal/security/smb_validate.go |
| P1-3 | 文档落地（README/规格入库）与数据面 WSS 加密（SECURITY.md §4） | server/internal/remote/remote.go、docs/SECURITY.md |
| P2-1 | 可观测性：trace ID 全链路 + /metrics 指标扩展 + 前端骨架屏/空态 | server/internal/api/（handlers 拆分）、internal/store/store.go、ui-src/* |
| P2-2 | 测试补强 / 前端无障碍对话框语义（role=dialog/焦点陷阱） | server/internal/*/*_test.go、ui-src/src/* |
| P2-3 | CI 流水线 + 覆盖率基线（≥60%） | .github/workflows/fvcc-ci.yml、scripts/check-coverage.ps1 |
| P2-4 | 统一 API 错误契约 {ok:false,code,msg,detail?} | server/internal/protocol/apierr.go、ui-src/src/api.ts、docs/API_CONTRACT.md |
| P2-5 | 删除操作回收站化（_trash 移入/恢复/清空） | server/internal/store/trash.go、ui-src/src/api.ts |
| P2-6 | 清理 AIGC 残留与任务编号注释（本表即落地物） | docs/TASK_REFERENCE.md |

> **编号漂移登记（2026-09-19，路径更新 2026-09-20）**：混乱报告 §4 中 `P2-1`（后端分层）、`P2-2`（前端组件化）为**报告体系编号**（均已落地）；代码注释中的 `P2-1` 多指"可观测性：Prometheus 指标端点"（internal/api/handler_metrics_test.go），`P2-2` 在代码注释中未见（前端 P 组为另一套清单，见 §2.2）。两套语义并存属历史事实，维护者按文件归属对照。本表"代表位置"已更新为 P2-1 分层后 internal 路径；B/C/D/M4/修复 组的旧路径映射见 §3 顶部路径说明。

### 2.2 前端 P 组（源自 FVCC 前端 UI 优化清单，**与后端同名编号含义不同**）

| 编号 | 可读语义 | 代表位置 |
|---|---|---|
| P0-1 | 日志面板/预览区底色收敛为主题令牌（--c-log-bg/--ansi-*，单一真源） | ui-src/src/pages/settings.ts、pages/editor/layout.ts |
| P0-2 | 预览区恒定深底 + 高对比辅助文字令牌 | ui-src/src/pages/editor/preview.ts |
| P0-4 | 移动端导航可点区域 ≥44px（min-h-11） | ui-src/src/main.ts |
| P1-1 | 进行中任务统一信号色（上传/转码/下载不再彩色） | ui-src/src/types.ts |
| P1-2 | 危险态样式收敛（2px 彩色描边 → 标题红字+危险按钮） | ui-src/src/ui.ts |
| P1-3 | 西文字体本地打包 + 统一 HH:MM:SS 时间码格式 | ui-src/src/main.ts、ui.ts |
| P1-4 | 全局命令面板（Ctrl/Cmd+K）+ 全局状态条 + 导航分组 | ui-src/src/command.ts、main.ts |
| P1-5 | 依赖瘦身（字体 latin 子集）+ 工作区布局预设 | ui-src/src/main.ts、pages/editor/layout.ts |
| P2-1 | 骨架屏/空状态/批量逐项结果（后端 P2-1 可观测性的前端配套体验） | ui-src/src/ui.ts、pages/* |
| P2-2 | 对话框/抽屉无障碍语义（role=dialog/aria-modal/Esc/焦点陷阱） | ui-src/src/ui.ts、pages/* |

> ⚠️ 同名不同义：后端 `P0-1`（凭据加密）≠ 前端 `P0-1`（主题令牌）；后端 `P1-1`（字段映射）≠ 前端 `P1-1`（信号色）。维护者请按文件归属对照本节。

---

## 3. B / C / D / M4 / 修复编号映射

> **路径说明（2026-09-20 第四版补）**：§3 各表"代表位置"已更新为 P2-1 分层后 internal 路径；历史旧路径 → 现状子包映射速查：`security.go`→`internal/security`（pathvalidator.go / gateway.go / audit.go / crypto.go / smb_validate.go / rate.go）、`proxy_flow.go`→`internal/media`（proxy.go，代理工作流）、`stream.go`→`internal/api`（stream.go，预览网关）、`store_proxy.go`/`store.go`/`store_edl.go`/`trash.go`→`internal/store`、`handlers*.go`/`router.go`/`ratelimit.go`/`security_roots.go`/`hashutil.go`→`internal/api`、`ws.go`→`internal/ws`（ws_limit.go 同）、`edl_validate.go`→`internal/edl`、`remote.go`→`internal/remote`、`scheduler.go`→`internal/scheduler`、`models.go`→`internal/store/model`、`apierr.go`/`applyjson.go`→`internal/protocol`、`version.go`→`internal/version`、`node_select.go`→`internal/node`。

### 3.1 B 组（后端调度/EDL 业务批次）

| 编号 | 可读语义 | 代表位置 |
|---|---|---|
| B-01 | 调度与持久化扩展：tasks 表列迁移、新增集合、幂等列迁移（设计 06 §2） | server/internal/store/store.go、internal/store/edl.go、ui-src/src/types.ts |
| B-03 | EDL 剪辑项目 CRUD（设计 03 §4.2/§4.3） | server/internal/api/router.go、internal/api/handlers_edl.go |
| B-04 | 渲染提交链路（设计 03 §4.4；含设置页渲染根字段保留等顺带改动） | server/internal/api/router.go、internal/api/handlers.go:125 |
| B-05 | 渲染类任务分流与冷却调度（QUEUE/COOLDOWN 状态机，设计 06 §4.1/§4.3/§4.5） | server/internal/scheduler/scheduler.go、internal/scheduler/scheduler_edl_test.go |
| B-06 | 渲染类任务下发通道（CreateRenderEDL/CreateGenProxy，仅传 credentialId，设计 06 §4.2） | server/internal/remote/remote.go、internal/scheduler/scheduler.go |
| B-07 | 进度与事件聚合（WS BroadcastTaskUpdateFull / HandleRemoteProgress，设计 06 §6） | server/internal/ws/ws.go、internal/scheduler/scheduler.go、internal/store/edl.go |
| B-08 | 节点能力上报（HelloPush）→ NodeCaps 落库 + 选机/健康分/熔断（设计 06 §5） | server/internal/remote/remote.go、internal/scheduler/scheduler.go、internal/ws/ws.go |
| B-09 | WS 断线自愈（指数退避重连/单飞/优雅关闭）+ 权限中间件 requireAdmin | server/internal/remote/remote.go、internal/api/router.go、internal/remote/remote_reconnect_test.go、internal/api/router_auth_test.go |

### 3.2 C 组（前端剪辑页批次）

| 编号 | 可读语义 | 代表位置 |
|---|---|---|
| C-01 | EDL 项目 API 封装 + 预览网关 API（设计 03 §4 / 04 §2） | ui-src/src/api.ts |
| C-02 | hash 路由（#/editor/:projectId）+ 剪辑页三段布局 | ui-src/src/main.ts、pages/editor/index.ts、layout.ts |
| C-03 | 编辑页状态机（5 切片订阅/撤销栈/2s 防抖保存，设计 02 §5） | ui-src/src/pages/editor/editorStore.ts |
| C-04 | 素材面板（授权目录下拉/可见性判定/扫描累加） | ui-src/src/pages/editor/assets.ts |
| C-05 | 预览器装配 + 单击素材定位（01 §4.2/§4.3） | ui-src/src/pages/editor/preview.ts、assets.ts |
| C-06 | 打点（inMs/outMs，源素材时间轴基准） | ui-src/src/pages/editor/clipOps.ts、preview.ts |
| C-07 | 时间线 + 时间尺（宽度∝时长/拖拽裁剪/filmstrip 分格） | ui-src/src/pages/editor/timeline.ts、ruler.ts |
| C-08 | 渲染弹窗（方案下拉=presetKey 枚举/输出名/节点选择/提交） | ui-src/src/pages/editor/dialog.ts、preset.ts |
| C-09 | 任务中心抽屉 + 渲染/代理任务展示派生（阶段文案/成品回看/代理状态） | ui-src/src/pages/editor/taskDrawer.ts、taskView.ts、pages/tasks.ts |
| C-10 | 剪辑快捷键注册表（Space 播放/I/O 打点/Delete 删段/Ctrl+Z/S） | ui-src/src/pages/editor/shortcuts.ts |

### 3.3 D 组（安全类任务）

| 编号 | 可读语义 | 代表位置 |
|---|---|---|
| D-02 | EDL 路径/输出名/载荷白名单校验（FVCC 与 FVCS 同规则双实现，设计 07 §3） | server/internal/edl/edl_validate.go、internal/security/pathvalidator.go |
| D-04 | 审计与告警：EDL 域拒绝事件集中记账 + 突增告警 + 限流拒绝记账（设计 07 §7/§4.5） | server/internal/security/audit.go、internal/api/ratelimit.go、internal/security/security_e2e_test.go |

### 3.4 M4（模块/里程碑编号）

| 编号 | 可读语义 | 代表位置 |
|---|---|---|
| M4 | 预览网关 + 代理工作流模块（设计 04：Range 流/票据/缩略图/三根白名单 + 代理生成/登记/proxy_ready 广播） | server/internal/api/stream.go、internal/media/proxy.go、internal/store/store_proxy.go、internal/api/handlers_proxy.go、internal/security/pathvalidator.go（三根解析）、internal/ws/ws.go（ProxyReadyMsg） |

> 说明：全库检索到的 `M3/M5/M6/M10/M11/M12/M14/M18/M19/M22` 等均位于前端 SVG path 数据中，属误报，非任务编号。

### 3.5 修复①-⑤（2026-09-16 剪辑页缺陷修复批次）

| 编号 | 可读语义 | 代表位置 |
|---|---|---|
| 修复① | 剪辑页多授权目录素材根支持：root 放宽接受「素材根本地绝对路径」，代理任务携带实际 sourceRoot，授权目录下拉切换 | server/internal/security/pathvalidator.go、internal/api/handlers_proxy.go、internal/remote/remote.go、ui-src/src/pages/editor/assets.ts、index.ts |
| 修复② | 代理 E_RENDER_FAILED 闭环：GEN_PROXY 素材共享根与转码同源（后端）；新扫描先清空 → 增量推批累加去重（前端） | server/internal/remote/remote.go、ui-src/src/pages/editor/assets.ts |
| 修复③ | 代理状态可感知：提交落地 queued/角标、消费 WS proxy_ready、页面渲染失败可见错误提示；渲染/代理下发补齐挂载凭据 | server/internal/remote/remote.go、ui-src/src/pages/editor/assets.ts、src/main.ts |
| 修复④ | filmstrip 分格缩略图：按片段像素宽度自适应分格（1~8 格），每格取时间段中点帧 | ui-src/src/pages/editor/timeline.ts |
| 修复⑤ | 预览器默认静音可配置：playerMuted 设置项（缺省 true 保持既有行为） | ui-src/src/pages/settings.ts、pages/editor/index.ts |

### 3.6 测试文件命名中的票号/后缀

| 测试文件 | 命名含义 | 追溯状态 |
|---|---|---|
| `genproxy_share_cred_test.go` | 代理生成 + 共享凭据（share_cred）场景测试（原名 `genproxy_share_cred_126_test.go`，`126` 为任务票号，2026-09-18 git mv 去票号） | 票号 126 的具体任务描述已不可考（代码内无台账对应） |
| `handlers_edl_list_test.go` | EDL 列表接口契约测试（原名 `handlers_edl_list_contract_test.go`，contract 后缀表示按接口契约命名，2026-09-18 git mv 去后缀） | 契约语义可从 03 §4.2 追溯 |

> 结论：测试文件命名混用「被测对象 + 票号/契约后缀」，与实现文件对应关系不直观，属混乱报告 §2.8 扣分项③；P2-7 已于 2026-09-18 落地，两文件统一为 `handlers_<domain>_test.go` 风格（git mv 保留历史），后续新测试沿用该风格。

---

## 4. 章节引用 → 规格文档映射

代码中 `03 §4.2` 式引用指向 `D:\Fnos.VideoConversion\WebVideoEditor_Design\` 下同名编号规格文档（已在版本库内跟踪）。**推荐写法**：`见 WebVideoEditor_Design/03-EDL数据模型与接口契约.md §4.2`。历史注释中的短写 `nn §x.y` 依下表还原：

| 短写 | 文档（WebVideoEditor_Design/） | 常见被引用章节含义 |
|---|---|---|
| 01 | 01-产品需求与交互规格.md | §4.x 素材面板/打点/渲染/任务中心；§5.x 素材可见性判定/状态机；§6 快捷键全表 |
| 02 | 02-前端架构与时间线编辑器设计.md | §5.x 编辑器状态机/保存调度；§6.x 时间线/播放头；§7.x 打点/校验/断流；§8.1 契约 |
| 03 | 03-EDL数据模型与接口契约.md | §2.x 数据模型/派生字段/毫秒约定；§3.x 任务载荷/线协议；§4.x EDL 接口；§5.x 载荷校验；§6 WS 事件；§7 schema_ver |
| 04 | 04-预览网关与代理工作流设计.md | §2.x 预览网关（Range/票据/缩略图/三根）；§3.x 代理工作流；§4.x 代理映射；§6 错误码；§7 改动点索引 |
| 05 | 05-RenderEDL渲染引擎与FFmpeg命令构造器设计.md | §5.1 presetKey 枚举表；§5.2 硬编降级 |
| 06 | 06-调度持久化与渲染节点管理设计.md | §2.x 持久化模型/列迁移；§3.x 线协议；§4.x 调度分流/退避/锁/幂等；§5.x 节点能力/健康分/选机；§6 事件聚合；§7 审计 |
| 07 | 07-安全校验与凭据管理细则.md | §3.x 路径/输出名校验分层；§4.x 安全头/限流；§5.x 凭据红线；§6 载荷深度；§7 审计记账；§9 验收用例 |
| 08 | 08-P0实施计划与验收清单.md | §4.2 权限验收；C-xx 验收用例（如 C-09 任务抽屉） |
| 09 | 09-附录-部署与迁移说明.md | §2 共享根 UNC 语义 |
| 10 | 10-实施进度与下一步.md | §3「下一步」E2E 清单；「C 组决策对齐清单」 |
| FS.md | ⚠️ 无对应文件（仓库内不存在） | `FS.md 8.2`（security.go 引用的四层路径校验原始规格）——外部/历史文档，仓库内**不可考**，语义已由 security.go 内联注释完整自述 |

---

## 5. 维护约定

1. 新代码注释**禁止**再引入未登记的任务编号；确需溯源时按「可读语义（编号）」格式书写。
2. 新规格文档引用一律写 `WebVideoEditor_Design/<文件名> §x.y` 全路径。
3. 本表新增/修改编号映射时同步更新本节，避免再次漂移。
