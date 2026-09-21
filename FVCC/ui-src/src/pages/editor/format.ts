// C-03：时间换算与 EDL 约束常量（与 Go 侧同源：FVCC/server/handlers_edl.go 常量 + 03 §2.5）
// 全局统一使用**整数毫秒**作为时间坐标，禁止在前端引入秒为单位的中间态（03 §2.4 毫秒约定）

/** 与后端 handlers_edl.go 保持一致的硬约束，超限在提交前即被拦截 */
export const EDL_LIMITS = {
  /** edlMaxClips */
  maxClips: 200,
  /** edlMinClipMs */
  minClipMs: 100,
  /** edlMaxTotalMs = 6h */
  maxTotalMs: 6 * 60 * 60 * 1000,
  /** edlMaxNameLen */
  maxNameLen: 64,
} as const

/** 毫秒 → HH:MM:SS.mmm（时间线/工具条展示口径） */
export function formatMs(ms: number): string {
  const total = Math.max(0, Math.floor(ms))
  const two = (n: number) => String(n).padStart(2, '0')
  const h = Math.floor(total / 3600000)
  const m = Math.floor((total % 3600000) / 60000)
  const s = Math.floor((total % 60000) / 1000)
  const msec = total % 1000
  return `${two(h)}:${two(m)}:${two(s)}.${String(msec).padStart(3, '0')}`
}

/** 毫秒 → MM:SS（轨道刻度等紧凑场景） */
export function formatMsShort(ms: number): string {
  const total = Math.max(0, Math.floor(ms))
  const two = (n: number) => String(n).padStart(2, '0')
  const m = Math.floor(total / 60000)
  const s = Math.floor((total % 60000) / 1000)
  return `${two(m)}:${two(s)}`
}

/**
 * 秒（HTMLMediaElement.currentTime）→ 整数毫秒
 * 打点统一入口：inMs = round(video.currentTime * 1000)（01 §4.3 / 02 §7.3）
 */
export function secToMs(sec: number): number {
  if (!Number.isFinite(sec) || sec < 0) return 0
  return Math.round(sec * 1000)
}

/** 整数毫秒 → 秒（仅用于 video.currentTime 赋值） */
export function msToSec(ms: number): number {
  return Math.max(0, ms) / 1000
}

/** 毫秒 → 帧号（向下取整，用于 ±1 帧步进） */
export function msToFrame(ms: number, fps: number): number {
  if (!fps || fps <= 0) return 0
  return Math.floor(Math.max(0, ms) / (1000 / fps))
}

/** 帧数 → 毫秒（P0：仅改变预览时间，见 02 §7.2） */
export function frameDeltaMs(frames: number, fps: number): number {
  if (!fps || fps <= 0) return 0
  return Math.round((frames * 1000) / fps)
}

/** 片段时长（整数毫秒） */
export function clipDurationMs(inMs: number, outMs: number): number {
  return Math.max(0, Math.round(outMs) - Math.round(inMs))
}

/** 生成片段 ID：c_<8hex>（03 §2.1） */
export function newClipId(): string {
  return 'c_' + randHex(8)
}

/** 生成素材 ID：a_<8hex>（03 §2.1） */
export function newAssetId(): string {
  return 'a_' + randHex(8)
}

function randHex(len: number): string {
  const buf = new Uint8Array(Math.ceil(len / 2))
  crypto.getRandomValues(buf)
  return Array.from(buf, (b) => b.toString(16).padStart(2, '0'))
    .join('')
    .slice(0, len)
}
