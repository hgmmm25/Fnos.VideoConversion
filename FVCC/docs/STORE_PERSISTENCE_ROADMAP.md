# FVCC Store 持久化健壮性方案（备份 / 迁移 / 快照回滚）

> 依据《FVCC_项目现状与开发步骤.md》§8 步骤 4 编制（2026-09-21 方案评审稿）。**2026-10-11 已按阶段 A~D 全部落地（v1.5.8）**，落地记录见文末变更记录与 `docs/IMPROVEMENT_LOG.md` §十二。
> 目标：补齐 `internal/store/store.go` 头部 `TODO(P1): 定时备份、配置迁移、快照回滚`（已随落地移除），在既有原子写基础上构建可恢复、可回滚、可升级的持久化体系。

## 1. 现状盘点

> 本节为 2026-09-21 方案稿快照，⚠️/❌ 项已于 v1.5.8 收口（见变更记录），仅作设计依据保留。

| 能力 | 现状 | 位置 |
|---|---|---|
| 原子写 | ✅ `saveJSON`：tmp 写入 → Rename → 失败回退旧文件 | `store.go` |
| 定时落盘 | ✅ Flush 5s 间隔 + 退出时 Flush | `store.go:722` |
| 崩溃恢复 | ✅ Load 时非终态任务重置 QUEUE（"was interrupted"） | `store.go Load` |
| 多文件一致性 | ⚠️ 各 json 独立原子写，无整体事务/校验和 | `Flush` |
| 定时备份 | ❌ 无 | — |
| 配置迁移 | ❌ 仅 projects 有 `SchemaVer` 补缺省，无版本化迁移链 | `Load` |
| 快照回滚 | ❌ 无；且审计类型缺 restore 类动作 | `internal/security/audit.go` |

## 2. 备份设计

### 2.1 触发与周期

- **定时备份**：沿用调度器/Store 既有定时器（复用 Flush 协程），默认每 30 分钟一次；备份目录 `<dataDir>/backup/`。
- **关键写前备份**：涉及节点/凭据/设置变更（`UpsertServer` / `SaveSettings` / `SaveProfiles`）时写前快照一份。
- **手动备份**：新增 `POST /api/store/backup`（admin），立即备份并返回备份 ID。

### 2.2 备份内容与保留

| 项 | 设计 |
|---|---|
| 格式 | `backup-<UTC yyyyMMddHHmmss>.zip`，内含全部 `*.json` + `schema_ver` 清单 |
| 加密 | 含凭据密文（secret.key 不备份，回滚时沿用现密钥即可解密） |
| 保留策略 | 定时备份保留最近 12 份（约 6h 窗口），手动备份永久保留直至用户删除 |
| 校验 | zip 内附 `manifest.json`（文件清单 + SHA-256），恢复前先验签 |

### 2.3 与原子写协同

备份读取内存索引序列化（与 Flush 同一快照语义），不加锁重复序列化；zip 写入用独立 tmp+Rename，避免备份文件本身半写。

## 3. 迁移设计

### 3.1 schema 版本化

- 复用现有 `StoreSchemaVersion`（tasks.json 已用）；各文件 `Version` 字段升级为迁移链锚点。
- 新增 `internal/store/migrate.go`：`var migrations = []Migration{ {From:1, To:2, Apply: func(...) error} }`。
- `Load` 流程改为：读文件 → `Version < Current` 时按序执行迁移 → 迁移结果落盘 → 加载。

### 3.2 迁移纪律

1. 迁移必须**幂等**（重复执行结果一致）——崩溃后重跑不损坏数据；
2. 迁移前自动备份（见 §2.1 关键写前备份），迁移失败回滚备份并报错，不静默降级；
3. 迁移不可逆时写 `audit`：`schema.migrate from=1 to=2`；
4. 每个迁移独立小步（单文件、单语义），禁止在迁移里顺带修 bug。

### 3.3 首批候选迁移

- `projects.json` 的 `SchemaVer=0 → 1` 由"加载时补缺省"改为显式迁移记录（收口 03 §7）；
- 后续字段新增/重命名统一走迁移链，不再用加载时兼容分支。

## 4. 快照回滚设计

### 4.1 入口与前置

- `POST /api/store/restore`（admin）：body = `{backupID}` 或 `{snapshotID}`。
- 前置：目标备份 manifest 校验通过；当前有未 Flush 脏数据时先 Flush（防回滚丢失最近落盘）。
- 回滚动作本身先落审计：新增审计类型 `snapshot.restore`（`internal/security/audit.go` 扩枚举）。

### 4.2 一致性规则

1. **整体替换**：从 zip 恢复全部 json 后统一 Rename 换入，任一步失败即回退（复用 saveJSON 回退语义）；
2. **运行时态重置**：恢复后内存索引整体重建（重新 Load），运行中调度任务按崩溃恢复语义重置 QUEUE；
3. **禁止部分回滚**：不支持按单文件回滚（破坏跨文件一致性），仅整包恢复；
4. 回滚后追加审计 `snapshot.restore backup=<id> status=ok`，失败记 `security.alert`。

### 4.3 与审计/告警联动

- 回滚成功/失败均写 `audit_log.json`（`AuditSink` 既有链路）；
- 回滚属于破坏性动作，前端确认卡片 + `RequireAdmin` 双重把关。

## 5. 实施阶段拆分

| 阶段 | 内容 | 验证 |
|---|---|---|
| A | 备份：定时 + 手动 + manifest 校验 + 保留策略 | 单测：备份可解压、校验和一致；手动备份接口 |
| B | 迁移框架：migrate.go + Load 迁移链 + 幂等性 | 单测：旧版本文件升级后字段正确、重复执行幂等 |
| C | 回滚：restore 接口 + 整包恢复 + 审计 + 崩溃回退 | 单测：损坏 zip 拒绝、恢复后 Load 正常、审计落库 |
| D | 收口：TODO 移除、文档同步、覆盖率补充 | 全量 go test -race + coverage 门禁 |

## 6. 评审要点（2026-10-11 确认，随阶段 A~D 落地关闭）

1. ✅ 备份周期 30min / 保留 12 份——按此落地（auto/prewrite/premigration/prerestore 共用 12 份轮转池，manual 永久）；
2. ✅ `secret.key` 不随备份走、回滚沿用现密钥——按此落地（备份内 servers/settings 保持 `enc:v1:` 密文）；
3. ✅ 恢复后调度任务重置 QUEUE——接受（restore 换入后经 `postLoadLocked` 执行，与崩溃恢复语义一致）；
4. ✅ 手动备份/回滚本轮**仅后端 API**（GET backups / POST backup / POST restore，均 RequireAdmin），前端入口待评审后另行排期。

变更记录：
- 2026-09-21 方案评审稿（未落地代码）。
- 2026-10-11 **已落地（v1.5.8）**，阶段 A~D 四次本地提交：A 备份 `efef4bd`（backup.go，定时 30min×12 + 写前备份 + 手动 API + manifest SHA-256 验签）；B 迁移框架 `16f1043`（migrate.go）；C 快照回滚 `59e4004`（restore.go + `POST /api/store/restore` + `snapshot.restore` 审计）；D 收口（TODO 移除 + 版本 1.5.8 + 文档回写）。
- **落地时对 SQLite 主载体的两处有意偏差**：① §3.1 的文件级 `Version` 锚点改为独立 **`data_version`** 数据面锚点（与 SQLite DDL `schema_version` 正交；载体 = `import_meta` 表 + `data_version.json` 镜像；目标值 3，链 1→2 task-columns-default、2→3 project-schema-ver；全新安装直接打锚不跑链，锚点高于目标拒绝启动）；② §4.2 的"Rename 换入 + 重新 Load"改为**先验签解析、后内存换入**（zip 条目不落地解包，条目名精确匹配白名单防 zip-slip，单条 64MB 上限；回滚前强制 prerestore 备份，失败中止=零副作用；换入后全量 persist 回载体并幂等重跑迁移链）。备份内容源同为内存快照（§2.3 语义保持）。
