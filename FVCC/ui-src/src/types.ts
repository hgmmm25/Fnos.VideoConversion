// 与后端 models.go 对齐的数据类型

export type TaskStatus =
  | 'QUEUE'
  | 'UPLOADING'
  | 'WAITING_TRANS'
  | 'TRANSCODING'
  | 'WAITING_DOWN'
  | 'DOWNLOADING'
  | 'COMPLETED'
  | 'ERROR'
  | 'PAUSED'
  | 'CANCELLED'
  // 06 §2.3 新增：冷却等待中（调度器退避），到期由服务端转回 QUEUE
  | 'COOLDOWN'

export type RetryType = 'retryable' | 'non_retryable'

/** 任务类型（FVCC models.go TaskType，06 §2.2）。空/TRANSCODE 为既有转码链路。 */
export type TaskType = 'TRANSCODE' | 'RENDER_EDL' | 'GEN_PROXY'

export interface Task {
  id: string
  orderId: number
  sourceFile: string
  outputFile: string
  fileName: string
  outputName: string
  serverId: string
  serverName: string
  profileId: string
  profileName: string
  ffmpegArgs: string
  status: TaskStatus
  progress: number
  retryCount: number
  retryType: RetryType
  coolDownUntil: string | null
  coolDownSec: number
  uploadChunkIndex: number
  remoteTaskId: string
  downloadOffset: number
  errorMsg: string
  // ===== B-01：tasks 表扩展列（06 §2.2），与 FVCC models.go Task 同形 =====
  taskType?: TaskType
  projectId?: string
  projectRev?: number
  payloadJson?: string
  checksum?: string
  credentialId?: string
  stage?: string
  segIndex?: number
  segTotal?: number
  totalMs?: number
  fastCopyAllowed?: boolean
  outTimeMs?: number
  speed?: string
  cooldownReason?: string
  createdAt: string
  updatedAt: string
}

export interface Server {
  id: string
  name: string
  ip: string
  port: number
  authKey: string
  keyExpireAt: string | null
  status: string
  lockExpireSec: number
  isLocal: boolean
}

export interface Profile {
  id: string
  name: string
  // 视频参数
  vcodec: string
  width: number
  height: number
  aspectRatio: string
  fps: string
  fpsCustom: number
  rateControl: string
  crf: number
  bitrate: string
  preset: string
  pixFmt: string
  profile: string
  tune: string
  rotate: number
  gop: number
  bframes: number
  scaleAlgo: string
  // 视频扩展参数
  videoMaxRate: number
  videoBufSize: number
  videoBrightness: number
  videoContrast: number
  videoSaturation: number
  enableYadif: boolean
  enableUnsharp: boolean
  unsharpStrength: number
  colorSpace: string
  videoRefs: number
  x264AQStrength: number
  nvencSpatialAQ: boolean
  nvencTemporalAQ: boolean
  videoScThreshold: number
  ctuSize: number
  rdLevel: number
  // 音频参数
  acodec: string
  channels: string
  sampleRate: string
  sampleRateCustom: number
  audioBitrate: string
  audioBitrateCustom: number
  sampleFmt: string
  aacProfile: string
  volume: number
  silence: boolean
  // 音频扩展参数
  audioDynNorm: boolean
  audioCutoff: number
  opusCompLevel: number
  audioSyncOffset: number
  // 高级封装参数
  hwAccel: string
  movFastStart: boolean
  threadCount: string
  // 其他
  extraArgs: string
  customFfmpeg: boolean
  customFfmpegArgs: string
  outputSuffix: string
  qualityControl: string
  constantQuality: number
  outputPath: string
  deleteSource: boolean
}

export interface StreamInfo {
  index: number
  codecType: string
  codecName: string
  codecLongName: string
  width: number
  height: number
  rFrameRate: string
  bitRate: string
  sampleRate: string
  channels: number
  channelLayout: string
  tags: Record<string, string>
}

export interface VideoInfo {
  path: string
  fileName: string
  format: string
  size: number
  duration: number
  resolution: string
  width: number
  height: number
  codec: string
  bitrate: string
  fps: string
  audioCodec: string
  audioBitrate: string
  sampleRate: string
  channels: number
  streamCount: number
  probed: boolean
  streams: StreamInfo[]
}

export interface Metrics {
  totalTasks: number
  totalHistory: number
  totalServers: number
  onlineServers: number
  byStatus: Record<string, number>
  // P2-1 可观测性扩展（2026-09-17 与后端 /metrics 同步）
  offlineServers: number
  activeTasks: number
  queuedTasks: number
  failedTasks: number
  completedTotal: number
  nodeCapsCount: number
}

export interface AppInfo {
  app: string
  version: string
  runtime: string
  serverTime: string
  gatewayUser: { uid: string; username: string; isAdmin: boolean; present: boolean }
  ffprobe: boolean
  accessPaths: string[]
}

export interface BrowseEntry {
  name: string
  path: string
  size?: number
  /** 仅授权根列表（path 为空）返回：是否为缺省素材根（2026-09-16 修复①，多授权目录） */
  default?: boolean
}

// 回收站条目（P2-5 删除回收站化）
export interface TrashItem {
  name: string
  path: string
  origPath: string
  size: number
  modTime: string
}

export interface BrowseResult {
  authorized?: boolean
  current: string
  parent?: string
  dirs: BrowseEntry[]
  files?: BrowseEntry[]
}

export interface Settings {
  schedulerIntervalSec: number
  chunkSizeMB: number
  maxRetry: number
  historyLimit: number
  outputSuffix: string
  accessiblePaths?: string[]
  transferMode: string
  /** @deprecated SMB模式现直接读取fnOS用户共享，不再需要手动指定共享路径 */
  smbSharePath?: string
  smbUser: string
  smbPassword: string
  logLevel: string
  maxLocalTranscodeCount: number
  /** 编辑页预览器默认静音（2026-09-16 新增；缺省 true，可在设置页关闭） */
  playerMuted?: boolean
}

export const LOG_LEVELS = [
  { value: 'DEBUG', label: 'DEBUG - 调试信息' },
  { value: 'INFO', label: 'INFO - 一般信息' },
  { value: 'WARN', label: 'WARN - 警告信息' },
  { value: 'ERROR', label: 'ERROR - 错误信息' },
  { value: 'FATAL', label: 'FATAL - 致命错误' },
]

export const STATUS_LABEL: Record<TaskStatus, string> = {
  QUEUE: '排队中',
  UPLOADING: '上传中',
  WAITING_TRANS: '待转码',
  TRANSCODING: '转码中',
  WAITING_DOWN: '待下载',
  DOWNLOADING: '下载中',
  COMPLETED: '已完成',
  ERROR: '错误',
  PAUSED: '已暂停',
  CANCELLED: '已取消',
  COOLDOWN: '冷却中',
}

export const STATUS_CLASS: Record<TaskStatus, string> = {
  QUEUE: 'bg-neutral-soft text-ink-muted',
  // P1-1：进行中（上传/转码/下载）统一信号色；等待中归中性，不再用彩色
  UPLOADING: 'bg-signal-soft text-signal-soft-text',
  WAITING_TRANS: 'bg-neutral-soft text-ink-muted',
  TRANSCODING: 'bg-signal-soft text-signal-soft-text',
  WAITING_DOWN: 'bg-neutral-soft text-ink-muted',
  DOWNLOADING: 'bg-signal-soft text-signal-soft-text',
  COMPLETED: 'bg-success-soft text-success-soft-text',
  ERROR: 'bg-danger-soft text-danger-soft-text',
  PAUSED: 'bg-neutral-soft text-ink-muted',
  CANCELLED: 'bg-neutral-soft text-neutral-soft-text',
  COOLDOWN: 'bg-warning-soft text-warning-soft-text',
}

export function isTerminal(s: TaskStatus): boolean {
  return s === 'COMPLETED' || s === 'CANCELLED'
}

// ===== C-01：EDL 剪辑类型（03 §2.1 契约字段，毫秒一律为整数）=====

/** 素材可见性（与 01 §5.2 状态机一致） */
export type AssetVisibility = 'direct' | 'need_proxy' | 'proxy_queued' | 'proxy_ready'

/**
 * 代理生命周期（2026-09-16 修复：proxy 状态此前只存在于文档与 WS 定义里，前端无落地状态）
 * none    = 无代理任务
 * queued  = 已提交 POST /proxy，等待任务中心完成（对应 AssetVisibility.proxy_queued）
 * ready   = 收到 WS proxy_ready 且已登记代理文件（对应 AssetVisibility.proxy_ready）
 * invalid = 代理生成失败/校验不一致，可重新提交
 */
export type ProxyState = 'none' | 'queued' | 'ready' | 'invalid'

/**
 * 预览/抽帧根（04 §2.6），对应 `/stream`、`/thumb`、`/proxy` 的 root 参数。
 * 2026-09-16 修复①：除 `src` | `proxy` | `dest` 三个枚举值外，服务端亦接受
 * 「授权素材根的本地绝对路径」，以及「授权素材根下的 _proxy 目录」，
 * 供剪辑页在多个授权目录间切换时按当前素材根请求缩略图/预览/代理。
 */
export type MediaRoot = string

/**
 * 预览器当前装配的素材（02 §5.1 / 04 §4.3）
 * 由 `pickPreviewSource(asset)` 派生：代理就绪走 proxy，否则回退 src
 */
export interface PreviewAsset {
  assetId: string
  /** 源文件相对路径（打点与 EDL 写入均以它为准） */
  file: string
  /** 当前播放所用的根：src 直读 | proxy 代理 */
  root: 'src' | 'proxy'
  /** 当前播放路径（root=proxy 时为代理相对路径） */
  path: string
  /** 打点基准时长（源文件时长，毫秒） */
  durationMs: number
  /** 帧率（timeline.fps 优先，HTMLVideoElement 无 fps 时回退此值） */
  fps: number
  visibility: AssetVisibility
  /** 代理相对路径（存在时列出） */
  proxyFile?: string
  /** mode=keyframe_only 的代理禁用打点（04 §4.3） */
  canMark: boolean
  /** 代理时长与源不一致等异常 → 禁用打点并提示重建（02 §7.3） */
  proxyMismatch?: boolean
}

export interface AssetRef {
  assetId: string // 'a_<8hex>'
  file: string // 相对 sourceRoot 的 POSIX 路径，如 'demo/a_01.mp4'
  durationMs: number // ffprobe 时长（打点基准）
  width: number
  height: number
  fps: number
  videoCodec: string // 'h264' | 'hevc' | 'prores' ...
  bitrate: number // bps
  visibility: AssetVisibility
  proxyFile?: string // 代理相对路径；无代理时省略
  /** 代理生命周期（缺省视为 none；由素材面板维护，WS proxy_ready 驱动） */
  proxyState?: ProxyState
  /** 代理时长（毫秒，proxy_ready 下发；与源差异过大时可提示复核） */
  proxyDurationMs?: number
  // 03 §2.1 未列、由素材面板（C-04）从扫描结果补齐的展示字段
  fileName?: string
  sizeBytes?: number
}

/** 时间线输出参数 */
export interface Timeline {
  width: number // 输出宽，默认取首个片段素材宽
  height: number
  fps: number // 默认 30
  sampleRate: number // 默认 48000
  audio: boolean // P0 恒为 true（P0 不做纯视频导出）
}

/** [P1+] 转场预留，P0 必须为 null 或省略 */
export interface Transition {
  type: string
  durationMs: number
}

/** 时间线片段 */
export interface EDLClip {
  clipId: string // 'c_<8hex>'
  assetId: string // 关联素材
  file: string // 冗余存源相对路径（渲染期免二次查表，改名后仍可报错定位）
  inMs: number // 入点（源素材时间轴，整数毫秒）
  outMs: number // 出点（源素材时间轴，整数毫秒，开区间：不含 outMs 帧）
  speed: number // P0 恒为 1.0
  sourceDurationMs: number // 打点时的素材时长快照 → 用于"素材已变更"校验
  transition?: Transition | null
}

/** 项目（= 持久化的 EDL） */
export interface Project {
  id: string // 'p_<8hex>'
  name: string
  rev: number // 乐观锁版本号，从 1 开始，每次成功 PUT +1
  timeline: Timeline
  clips: EDLClip[]
  createdAt: string // ISO8601
  updatedAt: string
  // 03 §2.1 未列、由 FVCC normalizeProject 重算后随详情返回的派生字段
  clipCount?: number
  totalMs?: number
  schemaVer?: number
  lastRenderTaskId?: string
}

export type ProjectSummary = Pick<Project, 'id' | 'name' | 'rev' | 'updatedAt'> & {
  clipCount: number
  // 海报帧（动效改进 2026-09-20）：首片段 file+inMs，卡片式视图复用 /thumb 抽帧；无片段时省略
  posterFile?: string
  posterMs?: number
}

/** POST /edl/projects 请求体（03 §4.2）；timeline 可省略/部分，服务端按 03 §2.2 回填缺省值 */
export interface ProjectCreateRequest {
  name: string
  timeline?: Partial<Timeline>
}

/** PUT /edl/projects/{id} 请求体（乐观锁，03 §4.2/§4.3） */
export interface ProjectUpdateRequest {
  rev: number
  name: string
  timeline: Timeline
  clips: EDLClip[]
}

/** PUT 成功响应（03 §4.2） */
export interface ProjectUpdateResult {
  rev: number
  savedAt: string
}

/** 渲染阶段（06 §2.2 tasks.stage；03 §6 的 WSEvent 另含 'proxy'） */
export type RenderStage = 'prepare' | 'segment' | 'concat' | 'mux' | 'finalize'

/** 渲染输出方案：P0 只允许预设键（03 §5.2 / 05 §5.1） */
export interface RenderProfile {
  presetKey: string // 枚举，如 'h264_nvenc_p5' | 'libx264_medium' | 'hevc_qsv_balanced'
  container: 'mp4' // P0 固定
  video: { codec: string; crf: number; preset: string; pixFmt: string; bitrate?: string }
  audio: { codec: 'aac'; bitrate: string; channels: number; sampleRate: number }
  fastCopyAllowed: boolean // 服务端计算后回填，前端仅展示
}

/** POST /edl/projects/{id}/render 请求体（03 §4.4；force 见 06 §4.4） */
export interface RenderRequest {
  presetKey: string
  outputName: string // 不含扩展名，服务端补 .mp4
  serverId?: string // 省略即按 06 §5.3 选机
  force?: boolean // true 跳过幂等复用
}

/** 渲染提交响应（03 §4.4） */
export interface RenderSubmitResult {
  taskId: string
  status: TaskStatus
  serverId: string
  output: string
  totalMs: number
  fastCopyAllowed: boolean
}

/** 渲染任务视图（taskType=RENDER_EDL，由 Task 派生，字段名与 FVCC JSON 对齐） */
export interface RenderTask {
  taskId: string
  status: TaskStatus
  projectId: string
  projectRev: number // 提交时刻冻结的版本（03 §4.3）
  stage?: RenderStage
  segIndex?: number
  segTotal?: number
  totalMs: number
  outTimeMs: number
  speed?: string
  output: string
  serverId: string
  errorMsg?: string
}

/** 预览票据（04 §2.3）：非一次性，绑定 path + 来源 IP，有效期 300s */
export interface StreamTicket {
  ticket: string // 'tk_<32hex>'
  expiresAt: string
}

/** POST /stream/ticket 请求体 */
export interface StreamTicketRequest {
  path: string
  root?: MediaRoot
}

/** POST /proxy 请求体（04 §3.2） */
export interface ProxyRequest {
  assetId: string
  file: string
  root?: MediaRoot
}

/** POST /proxy 响应（04 §3.2） */
export interface ProxyRequestResult {
  taskId: string
  status: TaskStatus
  proxyFile: string
  /** 2026-09-16 修复：同素材已有进行中/已就绪任务时服务端去重命中，前端不得据此误报"代理已就绪" */
  deduplicated?: boolean
}

/** 编辑器保存状态（01 §5.1 / 02 §5.3） */
export type SaveState = 'saved' | 'dirty' | 'saving' | 'conflict' | 'error'

// ===== WS 事件（FVCC → 浏览器，03 §6）=====

/** 任务摘要（03 §6 task_created 载荷，字段取自 FVCC Task 的列表视图） */
export type TaskSummary = Pick<
  Task,
  'id' | 'status' | 'progress' | 'createdAt' | 'updatedAt'
> & {
  taskType?: TaskType
  projectId?: string
}

export interface WSTaskUpdateEvent {
  type: 'task_update'
  taskId: string
  status: TaskStatus
  progress: number
  msg?: string
  stage?: RenderStage | 'proxy'
  seg?: { index: number; total: number }
}

export interface WSTaskCreatedEvent {
  type: 'task_created'
  task: TaskSummary
}

export interface WSProxyReadyEvent {
  type: 'proxy_ready'
  assetId: string
  proxyFile: string
  durationMs: number
}

export interface WSNodeStatusEvent {
  type: 'node_status'
  serverId: string
  online: boolean
  health: number
}

export type WSEvent =
  | WSTaskUpdateEvent
  | WSTaskCreatedEvent
  | WSProxyReadyEvent
  | WSNodeStatusEvent
