
# 03 · EDL 数据模型与接口契约

> 归属：上位方案 §4.3（EDL 数据结构与协议）、§5（任务生命周期）
> 对应源码：`FVCC/server/models.go`、`FVCC/server/router.go`、`FVCC/server/remote.go`、`FVCC/ui-src/src/types.ts`、`FVCS/pkg/protocol/protocol.go`、`FVCS/pkg/server/server.go`

---

## 1. 范围与非目标

- **范围**：EDL 的字段级定义（前后端双方）、时间与路径的表示约定、REST/WS 接口签名与样例、校验规则与错误码、既有 Transcode 协议的兼容策略。
- **非目标**：EDL → ffmpeg 命令的构造算法（见 05）；预览网关内部实现（见 04）；持久化表结构（见 06）；白名单实现细则（见 07）。
- **一致性要求**：本文件是前后端"唯一契约源"。任何字段变更必须先改本文件，再同步 `types.ts` 与 `models.go`。

---

## 2. EDL 数据模型

### 2.1 前端 TypeScript 类型

```ts
// FVCC/ui-src/src/types.ts 追加

/** 素材可见性（与 01 §5.2 状态机一致） */
export type AssetVisibility = 'direct' | 'need_proxy' | 'proxy_queued' | 'proxy_ready'

export interface AssetRef {
  assetId: string            // 'a_<8hex>'
  file: string               // 相对 sourceRoot 的 POSIX 路径，如 'demo/a_01.mp4'
  durationMs: number         // ffprobe 时长（打点基准）
  width: number
  height: number
  fps: number
  videoCodec: string         // 'h264' | 'hevc' | 'prores' ...
  bitrate: number            // bps
  visibility: AssetVisibility
  proxyFile?: string         // 代理相对路径；无代理时省略
}

/** 时间线输出参数 */
export interface Timeline {
  width: number              // 输出宽，默认取首个片段素材宽
  height: number
  fps: number                // 默认 30
  sampleRate: number         // 默认 48000
  audio: boolean             // P0 恒为 true（P0 不做纯视频导出）
}

/** 时间线片段 */
export interface EDLClip {
  clipId: string             // 'c_<8hex>'
  assetId: string            // 关联素材
  file: string               // 冗余存源相对路径（渲染期免二次查表，且在素材被改名后仍可报错定位）
  inMs: number               // 入点（源素材时间轴，整数毫秒）
  outMs: number              // 出点（源素材时间轴，整数毫秒，开区间语义：不含 outMs 帧）
  speed: number              // P0 恒为 1.0；[P1+] 变速预留
  sourceDurationMs: number   // 打点时的素材时长快照 → 用于"素材已变更"校验
  transition?: null          // [P1+] 转场预留，P0 必须为 null 或省略
}

/** 项目（= 持久化的 EDL） */
export interface Project {
  id: string                 // 'p_<8hex>'
  name: string
  rev: number                // 乐观锁版本号，从 1 开始，每次成功 PUT +1
  timeline: Timeline
  clips: EDLClip[]
  createdAt: string          // ISO8601
  updatedAt: string
}

export type ProjectSummary = Pick<Project, 'id' | 'name' | 'rev' | 'updatedAt'> & { clipCount: number }

/** 渲染输出方案（P0 只允许预设键，见 §5.2） */
export interface RenderProfile {
  presetKey: string                      // 枚举，如 'h264_nvenc_p5' | 'libx264_medium' | 'hevc_qsv_balanced'
  container: 'mp4'                       // P0 固定
  video: { codec: string; crf: number; preset: string; pixFmt: string; bitrate?: string }
  audio: { codec: 'aac'; bitrate: string; channels: number; sampleRate: number }
  fastCopyAllowed: boolean               // 服务端计算后回填，前端仅展示
}
```

### 2.2 Go 结构体（FVCC 侧）

```go
// FVCC/server/models.go 追加

type Timeline struct {
    Width      int  `json:"width"`
    Height     int  `json:"height"`
    FPS        int  `json:"fps"`
    SampleRate int  `json:"sampleRate"`
    Audio      bool `json:"audio"`
}

type EDLClip struct {
    ClipID           string  `json:"clipId"`
    AssetID          string  `json:"assetId"`
    File             string  `json:"file"`             // 相对 SourceRoot 的 POSIX 路径
    InMs             int64   `json:"inMs"`
    OutMs            int64   `json:"outMs"`
    Speed            float64 `json:"speed"`
    SourceDurationMs int64   `json:"sourceDurationMs"`
    Transition       *Transition `json:"transition,omitempty"` // P0 恒 nil
}

type Project struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Rev       int       `json:"rev"`
    Timeline  Timeline  `json:"timeline"`
    Clips     []EDLClip `json:"clips"`
    CreatedAt string    `json:"createdAt"`
    UpdatedAt string    `json:"updatedAt"`
}

// 线协议载荷（下发给 FVCS）
type WireClip struct {
    File  string  `json:"file"`
    In    string  `json:"in"`    // "HH:MM:SS.mmm"
    Out   string  `json:"out"`
    Speed float64 `json:"speed"`
}

type RenderTaskPayload struct {
    Type       string      `json:"type"`        // 恒为 "RenderEDL"
    ProjectID  string      `json:"projectId"`
    ProjectRev int         `json:"projectRev"`
    SourceRoot string      `json:"sourceRoot"`  // 相对共享根的素材根，如 "/media/videos"
    DestRoot   string      `json:"destRoot"`
    Output     string      `json:"output"`      // 相对 DestRoot 的文件名，P0 强制 .mp4
    Timeline   Timeline    `json:"timeline"`
    Clips      []WireClip  `json:"clips"`
    Profile    RenderProfile `json:"profile"`
    TotalMs    int64       `json:"totalMs"`     // Σ(outMs-inMs)，服务端计算，用于进度加权
    Checksum   string      `json:"checksum"`    // 对 clips+profile 规范化后的 sha256 前 16 位，见 06 §4.4 幂等键
}
```

### 2.3 路径与根目录约定

| 项 | 约定 |
|---|---|
| `SourceRoot` | 相对共享根的素材根，POSIX 风格，不以 `/` 结尾，如 `/media/videos`；由 `Settings.videoRoot` 提供 |
| `DestRoot` | 成品根，如 `/media/exports`；由 `Settings.exportRoot` 提供，缺省时等于 `SourceRoot` 下的 `_exports` |
| `clip.file` | 相对 `SourceRoot` 的 POSIX 路径，禁止以 `/` 开头、禁止含 `..` 段、禁止反斜杠 |
| `output` | 相对 `DestRoot` 的文件名（P0 不允许子目录），扩展名强制 `.mp4` |
| 磁盘绝对路径 | **协议中不出现**。FVCS 侧由 `smb.BuildSMBPath(shareBaseUNC, sourceRoot+"/"+file)` 拼接 UNC 路径 |
| 分隔符转换 | 前端/协议统一 `/`；仅 FVCS 在拼接 UNC 时转换，转换函数为 `pkg/smb/smb.go` 既有 `BuildSMBPath` |

> 与上位方案的一致性：上位方案 §4.3.2 示例中的 `"/media/videos"` 与 `"a_01.mp4"` 即本表的 `SourceRoot` 与 `clip.file`；本文件补充了"必须是相对 SourceRoot 的路径"这一约束，语义未变。

### 2.4 时间表示与换算（三态统一）

| 场景 | 表示 | 依据 |
|---|---|---|
| UI 内存态 / 持久化 JSON（`projects.edl_json`） | **整数毫秒**（`inMs/outMs/sourceDurationMs/totalMs`） | 避免浮点累加误差（README A3） |
| REST 请求/响应 | 整数毫秒 | 同上 |
| FVCC → FVCS 线协议（`WireClip.in/out`） | **`HH:MM:SS.mmm`** 字符串 | 与 ffmpeg `-ss/-t` 参数文本一致，且便于正则白名单 |
| ffmpeg 命令行 | 同线协议 | 直接透传，不做二次格式化 |

换算函数（前后端同源规则，必须逐位一致）：

```
msToTimecode(ms):                      // 全部整数运算后拼接，禁止用浮点秒
  sign = ms < 0 ? "-" : ""; ms = |ms|
  h = ms / 3600000; ms %= 3600000
  m = ms / 60000;   ms %= 60000
  s = ms / 1000;    ms %= 1000
  return f"{sign}{h:02d}:{m:02d}:{s:02d}.{ms:03d}"

timecodeToMs(tc):
  match ^(\d{2,}):([0-5]\d):([0-5]\d)\.(\d{3})$
  return ((h*3600 + m*60 + s) * 1000) + ms

frameOf(ms, fps) = floor(ms * fps / 1000)
```

**约定**：`outMs` 为**开区间**（不含 `outMs` 时刻那一帧）；`时长 = outMs - inMs`（≥ 100ms）。ffmpeg 侧用 `-ss <in> -t <(out-in)>` 表达，与开区间语义一致。

### 2.5 字段约束与 P0 预留

| 字段 | P0 约束 | 超限处理 |
|---|---|---|
| `clips` 数量 | 1 ~ 200 | `E_EDL_INVALID` |
| 单片段时长 | ≥ 100ms | 前端钳制 + 后端 `E_EDL_INVALID` |
| 入点 | `0 ≤ inMs < outMs ≤ sourceDurationMs` | `E_EDL_INVALID` |
| `speed` | 恒 1.0（`|speed-1.0| < 1e-6`） | `E_EDL_INVALID`（P0 不支持变速） |
| `transition` | 必须为 `null`/省略 | `E_EDL_INVALID`（P2 才允许） |
| `timeline.fps` | 1 ~ 120 | `E_EDL_INVALID` |
| `timeline.width/height` | 16 ~ 7680 且为偶数 | `E_EDL_INVALID` |
| `clips` 总时长 | ≤ 6 小时 | `E_EDL_INVALID` |
| EDL JSON 体积 | ≤ 256 KB | `E_EDL_TOO_LARGE` |

**P1+/P2 预留（P0 不实现，但类型上留位，避免后期破坏兼容）**：`EDLClip.transition`、`EDLClip.speed != 1`、`Profile.video.bitrate`。P0 服务端对预留字段一律"存在即拒绝"，确保不会静默忽略。

---

## 3. 线协议（FVCC → FVCS）

### 3.1 命令扩展

```go
// FVCS/pkg/protocol/protocol.go 追加
const (
    CmdCreateRenderEDL CmdType = "CreateRenderEDL"
    CmdCreateGenProxy  CmdType = "CreateGenProxy"   // 见 04 §3
)

type WebSocketRequest struct {
    // ...既有字段保持不变...
    Cmd           CmdType         `json:"cmd"`
    Key           string          `json:"key"`
    TaskID        string          `json:"taskId,omitempty"`         // FVCC 侧任务 ID（新增，用于对账）
    TaskType      string          `json:"taskType,omitempty"`       // "TRANSCODE"(默认) | "RENDER_EDL" | "GEN_PROXY"
    Priority      string          `json:"priority,omitempty"`
    SMBPath       string          `json:"smbPath,omitempty"`        // 源根 UNC（用于存在性预检）
    SMBOutputPath string          `json:"smbOutputPath,omitempty"`  // 目标目录 UNC
    CredentialID  string          `json:"credentialId,omitempty"`   // 本地凭据档案键；空=默认档案（README A6）
    SMBUser       string          `json:"smbUser,omitempty"`        // [deprecated] 仅兼容旧客户端
    SMBPassword   string          `json:"smbPassword,omitempty"`    // [deprecated] 不再由 FVCC 下发
    Payload       json.RawMessage `json:"payload,omitempty"`        // RenderEDL / GenProxy 的结构化载荷
}
```

**兼容策略**

| 请求形态 | FVCS 行为 |
|---|---|
| `taskType` 空 + 有 `ffmpegArgs` | 走既有 Transcode 路径（完全不变） |
| `taskType="RENDER_EDL"` + `payload` | 走 05 的构造器；忽略 `ffmpegArgs` |
| `taskType="RENDER_EDL"` + 同时带 `ffmpegArgs`/`extraArgs` | **拒绝**：`E_PROTO_FIELD_CONFLICT`（防止绕过命令构造器） |
| 带 `smbPassword` 非空 | 接受但记录 `WARN` 审计日志（迁移期），优先使用 `credentialId` |

### 3.2 请求示例

```json
{
  "cmd": "CreateRenderEDL",
  "key": "<X-Auth-Key>",
  "taskId": "t_1757980000_ab12cd",
  "taskType": "RENDER_EDL",
  "priority": "normal",
  "smbPath": "\\\\NAS\\media\\videos",
  "smbOutputPath": "\\\\NAS\\media\\exports",
  "credentialId": "nas-media-default",
  "payload": {
    "type": "RenderEDL",
    "projectId": "p_1a2b3c4d",
    "projectRev": 7,
    "sourceRoot": "/media/videos",
    "destRoot": "/media/exports",
    "output": "demo_cujian_20260910.mp4",
    "timeline": { "width": 1920, "height": 1080, "fps": 30, "sampleRate": 48000, "audio": true },
    "clips": [
      { "file": "demo/a_01.mp4", "in": "00:01:22.400", "out": "00:03:45.000", "speed": 1 },
      { "file": "demo/a_02.mp4", "in": "00:00:00.000", "out": "00:02:10.500", "speed": 1 },
      { "file": "demo/a_03.mp4", "in": "00:00:12.000", "out": "00:00:40.200", "speed": 1 }
    ],
    "profile": {
      "presetKey": "h264_nvenc_p5",
      "container": "mp4",
      "video": { "codec": "h264_nvenc", "crf": 18, "preset": "p5", "pixFmt": "yuv420p" },
      "audio": { "codec": "aac", "bitrate": "192k", "channels": 2, "sampleRate": 48000 },
      "fastCopyAllowed": true
    },
    "totalMs": 341300,
    "checksum": "9f2c41ab77d0e3c5"
  }
}
```

### 3.3 响应示例

```json
{ "ok": true, "taskId": "task_1757980000123456789", "status": "waiting", "serverTaskId": "task_1757980000123456789" }
```

失败：

```json
{ "ok": false, "code": "E_PAYLOAD_INVALID", "msg": "clips[1].out 早于 in", "detail": { "index": 1 } }
```

> 字段名 `code/msg/detail` 与 FVCS 既有 `WebSocketResponse` 的 `error` 字段关系：新增任务类型使用新字段，**存量 Transcode 响应保持 `error` 字段不变**（避免破坏现有前端解析）。FVCC 侧解析时两者兼容。

---

## 4. REST 接口契约

### 4.1 通用约定

- 前缀：`/app/fvcc/api`（与 `FVCC/server/router.go` 的 `gwPrefix` 一致，对应 README A2）。
- 鉴权：沿用 `authMiddleware`（`X-Auth-Key` 头或 Cookie 会话）；写操作要求 admin（沿用 `gateway.go: requireAdmin`）。
- 响应：成功 `{...}` 或 `{items:[...]}`；失败统一 `{ ok:false, code, msg, detail? }` + 相应 HTTP 状态码（400/403/404/409/413/500/507）。

### 4.2 项目 CRUD

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/edl/projects` | 列出项目（含 `clipCount`） |
| POST | `/edl/projects` | 新建项目 |
| GET | `/edl/projects/{id}` | 读取项目（含 clips） |
| PUT | `/edl/projects/{id}` | 覆盖保存（乐观锁） |
| DELETE | `/edl/projects/{id}` | 删除项目（仅删元数据，不动素材） |

```http
POST /app/fvcc/api/edl/projects
Content-Type: application/json
X-Auth-Key: <key>

{ "name": "demo_粗剪", "timeline": { "width": 1920, "height": 1080, "fps": 30, "sampleRate": 48000, "audio": true } }
```

```json
200 OK
{
  "id": "p_1a2b3c4d", "name": "demo_粗剪", "rev": 1,
  "timeline": { "width": 1920, "height": 1080, "fps": 30, "sampleRate": 48000, "audio": true },
  "clips": [], "createdAt": "2026-09-10T14:02:11+08:00", "updatedAt": "2026-09-10T14:02:11+08:00"
}
```

```http
PUT /app/fvcc/api/edl/projects/p_1a2b3c4d
Content-Type: application/json

{ "rev": 1, "name": "demo_粗剪", "timeline": { "width":1920, "height":1080, "fps":30, "sampleRate":48000, "audio":true },
  "clips": [ { "clipId":"c_00000001", "assetId":"a_0000000a", "file":"demo/a_01.mp4",
               "inMs":82400, "outMs":225000, "speed":1.0, "sourceDurationMs":600000 } ] }
```

```json
200 OK   { "rev": 2, "savedAt": "2026-09-10T14:05:33+08:00" }
409 Conflict
{ "ok": false, "code": "E_REV_CONFLICT", "msg": "项目已在其他页面被修改", "detail": { "serverRev": 3, "clientRev": 1 } }
```

### 4.3 乐观锁规则

1. `PUT` 必带 `rev`；`rev !== server.rev` → 409。
2. 成功保存后服务端 `rev += 1` 并写 `updated_at`。
3. 项目在 `Dirty` 状态被另一客户端保存后，前端收到 409，弹出"覆盖云端 / 载入云端"（01 §5.1）；选择"覆盖云端"时以 `detail.serverRev` 作为新 `rev` 重发。
4. 渲染提交会**冻结 rev**：`RenderTaskPayload.projectRev` 记录提交时刻的版本，渲染中的项目若被继续编辑，不影响已提交任务；任务详情页提示"该项目已更新，可重新渲染"（比对 `projectRev` 与当前 `rev`）。

### 4.4 渲染提交

```http
POST /app/fvcc/api/edl/projects/p_1a2b3c4d/render
Content-Type: application/json

{ "presetKey": "h264_nvenc_p5", "outputName": "demo_cujian_20260910", "serverId": "s_01" }
```

- `outputName` 不含扩展名，服务端补 `.mp4` 并做重名处理（追加 `_1`/`_2`）。
- `serverId` 可省略（P0 省略即按 06 §5.3 选机）。

```json
200 OK
{ "taskId": "t_1757980000_ab12cd", "status": "QUEUE", "serverId": "s_01", "output": "demo_cujian_20260910.mp4", "totalMs": 341300, "fastCopyAllowed": true }
```

失败样例：

```json
400 { "ok": false, "code": "E_EDL_INVALID", "msg": "clips[2] 出点超出素材时长", "detail": { "index": 2, "maxOutMs": 40200 } }
403 { "ok": false, "code": "E_ASSET_NOT_IN_ROOT", "msg": "素材不在授权目录内", "detail": { "file": "demo/../secret.mp4" } }
409 { "ok": false, "code": "E_NODE_OFFLINE", "msg": "指定渲染节点离线" }
507 { "ok": false, "code": "E_DISK_FULL", "msg": "目标目录空间不足", "detail": { "needBytes": 2147483648, "freeBytes": 314572800 } }
```

### 4.5 预览与辅助接口

| 方法 | 路径 | 说明 | 细节 |
|---|---|---|---|
| POST | `/stream/ticket` | 申请一次性预览票据 | 见 04 §2.3 |
| GET | `/stream?path=&ticket=` | Range 视频流 | 见 04 §2.2 |
| GET | `/thumb?path=&t=` | 抽帧缩略图（JPEG，320×180） | 见 04 §2.4 |
| POST | `/proxy` | 请求生成代理 | 见 04 §3.2 |
| GET | `/profiles?compatible=render_edl` | 渲染方案候选 | 复用现有方案接口 + 兼容性过滤 |

```http
POST /app/fvcc/api/stream/ticket
{ "path": "demo/a_01.mp4" }
```
```json
200 OK { "ticket": "tk_9f2c41ab77d0e3c5", "expiresAt": "2026-09-10T14:10:33+08:00" }
```

---

## 5. 校验规则与错误码

### 5.1 分层校验（四道闸门）

```mermaid
flowchart LR
    A["前端钳制<br/>（第二章 §5）"] --> B["FVCC 提交校验<br/>（本文件 §5.2）"]
    B --> C["FVCC 下发前二次校验<br/>+ 素材存在性预检"]
    C --> D["FVCS 载荷白名单校验<br/>（07 §3）"]
    D --> E["构造器内部断言<br/>（05 §5）"]
```

任一闸门拒绝 → 不产生渲染任务；错位信息必须包含可定位的 `index`/`field`。

### 5.2 后端结构化白名单（FVCC 与 FVCS 共用的规则集）

| 字段 | 规则 | 违反错误码 |
|---|---|---|
| `file` | 长度 1..255；不得含 `\`、`:`、`*`、`?`、`"`、`<`、`>`、`|`、控制字符；不得含 `..` 段；不得以 `/` 开头；扩展名 ∈ `{mp4,mov,mkv,m4v,avi,mxf}`（大小写不敏感） | `E_EDL_INVALID` |
| `in` / `out` | `^\d{2,}:[0-5]\d:[0-5]\d\.\d{3}$` 且 `out > in` | `E_EDL_INVALID` |
| `output` | 同 `file` 规则，扩展名强制 `mp4`，不得含路径分隔符 | `E_EDL_INVALID` |
| `presetKey` | 必须命中服务端枚举表（见 05 §5.2），不在表内即拒绝 | `E_PROFILE_INVALID` |
| `timeline` | 见 §2.5 数值范围；`width/height` 必须偶数 | `E_EDL_INVALID` |
| `payload` 额外字段 | 出现未声明字段 → 拒绝（`DisallowUnknownFields` 或显式检查） | `E_PAYLOAD_INVALID` |
| 源文件存在性 | 逐 clip `os.Stat`（FVCC 侧）/ `MountSMB` 后 stat（FVCS 侧） | `E_ASSET_MISSING` |
| 源路径越界 | 规范化后必须以 `SourceRoot` 前缀开头 | `E_ASSET_NOT_IN_ROOT` |
| 目标盘余量 | `freeBytes ≥ 2.0 × estimatedOutputBytes` | `E_DISK_FULL` |

> 与上位方案 §6 的一致性：上位方案要求"结构化白名单、不用黑名单"，本表即其展开；`file` 字段采用"字符集允许 + 结构禁止"双重约束，替代既有 `ValidateExtraFFmpegArgs` 的黑名单正则（改动点见 07 §3.3）。

### 5.3 错误码表

| 错误码 | HTTP | 触发位置 | 是否可重试 |
|---|---|---|---|
| `E_EDL_INVALID` | 400 | FVCC 提交校验 / FVCS 载荷校验 | 否 |
| `E_EDL_TOO_LARGE` | 413 | FVCC | 否 |
| `E_PAYLOAD_INVALID` | 400 | FVCS | 否 |
| `E_PROFILE_INVALID` | 400 | FVCC / FVCS | 否 |
| `E_PROTO_FIELD_CONFLICT` | 400 | FVCS | 否 |
| `E_ASSET_MISSING` | 404 | FVCC / FVCS | 否 |
| `E_ASSET_NOT_IN_ROOT` | 403 | FVCC | 否 |
| `E_REV_CONFLICT` | 409 | FVCC | 用户决定 |
| `E_NODE_OFFLINE` | 409 | FVCC 调度器 | 自动（冷却） |
| `E_SMB_MOUNT_FAILED` | 500 | FVCS | 自动（冷却，≤2 次） |
| `E_RENDER_FAILED` | 500 | FVCS | 是 |
| `E_FFMPEG_MISSING` | 500 | FVCS | 否（需运维） |
| `E_DISK_FULL` | 507 | FVCC / FVCS | 否 |
| `E_TIMEOUT_STALL` | 500 | FVCS（5min 无进度） | 是 |

---

## 6. WS 事件契约（FVCC → 浏览器）

```ts
export type WSEvent =
  | { type: 'task_update'; taskId: string; status: string; progress: number; msg?: string;
      stage?: 'prepare'|'segment'|'concat'|'mux'|'proxy'; seg?: { index: number; total: number } }
  | { type: 'task_created'; task: TaskSummary }
  | { type: 'proxy_ready'; assetId: string; proxyFile: string; durationMs: number }
  | { type: 'node_status'; serverId: string; online: boolean; health: number }
```

| 事件 | 触发时机 | 现存实现差异 |
|---|---|---|
| `task_update` | FVCS `Progress` 上报后由 FVCC 转发 | 既有 `BroadcastTaskUpdate(taskID, status, progress, msg)` 需扩展 `stage/seg`（06 §6） |
| `proxy_ready` | GenProxy 成功且元数据回填后 | 新增 |
| `node_status` | 心跳超时/恢复、健康分跨档 | 新增 |

---

## 7. 版本与兼容

| 项 | 策略 |
|---|---|
| 接口版本 | 复用现有无版本号路径；不兼容变更通过新增路径段（如 `/edl/v2/...`）实现 |
| 项目 JSON 版本 | `projects.schema_ver` 字段（默认 1）；读取时高于当前支持版本 → 只读打开并提示升级客户端 |
| 传输字段 | 新增字段一律 `omitempty`；删除字段先标记 `[deprecated]` 保留 ≥1 个版本 |
| 前端缓存 | `#/editor` 页面首次加载时校验 `GET /api/info` 的 `version`，与构建版本不一致时提示强刷 |

---

## 8. 源码改动点索引

| 文件 | 改动 | 关联 |
|---|---|---|
| `FVCC/server/models.go` | 新增 `Timeline/EDLClip/Project/WireClip/RenderTaskPayload/RenderProfile/Transition` | §2.2 |
| `FVCC/server/handlers.go` | 新增 `listEDLProjects/createEDLProject/getEDLProject/updateEDLProject/deleteEDLProject/renderEDLProject/handleStreamTicket` | §4 |
| `FVCC/server/edl_validate.go` | **新增**：跨端共用校验规则（§5.2）的 Go 实现 | §5.2、07 §3.2 |
| `FVCC/server/router.go` | 注册 `/edl/projects*`、`/edl/projects/{id}/render`、`/stream`、`/stream/ticket`、`/thumb`、`/proxy` | §4 |
| `FVCC/server/remote.go` | 新增 `CreateRenderEDL(ctx, serverID, payload, smb)`；`wsCmd` 增加 `taskType/payload/credentialId/smbPath/smbOutputPath` | §3.1、06 §4.2 |
| `FVCC/server/store.go` | 新增 `projects` 表读写与 `rev` 乐观锁；`tasks` 表新增 `task_type/payload_json/project_id/stage/seg_*` | 06 §2 |
| `FVCC/ui-src/src/types.ts` | 新增 §2.1 全部类型 + `WSEvent` | §2.1、§6 |
| `FVCS/pkg/protocol/protocol.go` | 新增命令常量、请求字段、`ValidateRenderEDLPayload`（替代黑名单） | §3.1、07 §3.3 |
| `FVCS/pkg/server/server.go` | `handleWebSocket` 分派 `CreateRenderEDL`；新增 `handleCreateRenderEDL`（仿 `handleCreateSMBTask` L447） | 05 §2 |
| `FVCS/pkg/task/task.go` | `Task` 新增 `TaskType/Payload/Stage/SegTotal/SegDone/TotalMs`；新增 `CreateRenderTask`（仿 `CreateSMBTask` L616） | 06 §3 |

