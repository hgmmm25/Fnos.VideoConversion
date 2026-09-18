// C-07 辅助：时间尺（02 §6.2）
// 刻度间隔在 [1,5,10,30,60,300] 秒中取第一个满足 interval * pxPerSec >= 60 的值
import { el } from '../../ui'
import { formatMsShort, msToSec } from './format'

const INTERVALS_MS = [1000, 5000, 10000, 30000, 60000, 300000]
export const RULER_H = 22

/**
 * 时间线坐标映射（02 §6.2）
 * 片段存在 MIN_PX 兜底宽度时，`x = ms / totalMs * width` 不再成立，
 * 因此由 timeline.ts 注入**分段线性映射**；不注入时退化为按总时长比例映射。
 */
export interface RulerMap {
  /** 全局毫秒 → 轨道 x（px） */
  xOf(ms: number): number
  /** 轨道 x（px，相对 ruler 左边界） → 全局毫秒 */
  msOf(x: number): number
  /** 轨道总宽度（px） */
  width(): number
}

export interface Ruler {
  root: HTMLElement
  /** 轨道宽度变化后重绘刻度；totalMs 仅用于退化模式 */
  render(totalMs: number, width: number): void
}

export function pickTickIntervalMs(totalMs: number, width: number): number {
  if (totalMs <= 0 || width <= 0) return INTERVALS_MS[0]
  const pxPerSec = width / msToSec(totalMs)
  for (const it of INTERVALS_MS) {
    if (msToSec(it) * pxPerSec >= 60) return it
  }
  return INTERVALS_MS[INTERVALS_MS.length - 1]
}

export function buildRuler(onSeek: (ms: number) => void, map?: RulerMap): Ruler {
  const root = el('div', {
    class: 'sticky top-0 z-10 bg-surface-alt border-b border-line select-none cursor-text',
    style: `height:${RULER_H}px`,
  })
  let totalMs = 0
  let width = 0

  function msAt(clientX: number): number {
    const r = root.getBoundingClientRect()
    if (r.width <= 0) return 0
    if (map) {
      const ms = map.msOf(clientX - r.left)
      return Math.max(0, Math.min(totalMs, Math.round(ms)))
    }
    if (totalMs <= 0) return 0
    const ratio = Math.max(0, Math.min(1, (clientX - r.left) / r.width))
    return Math.round(ratio * totalMs)
  }

  root.addEventListener('pointerdown', (e) => {
    e.preventDefault()
    root.setPointerCapture(e.pointerId)
    onSeek(msAt(e.clientX))
    const move = (ev: PointerEvent) => onSeek(msAt(ev.clientX))
    const up = () => {
      root.removeEventListener('pointermove', move)
      root.removeEventListener('pointerup', up)
      root.removeEventListener('pointercancel', up)
    }
    root.addEventListener('pointermove', move)
    root.addEventListener('pointerup', up)
    root.addEventListener('pointercancel', up)
  })

  function render(total: number, w: number) {
    totalMs = total
    width = map ? map.width() : w
    root.innerHTML = ''
    if (totalMs <= 0 || width <= 0) return
    const interval = pickTickIntervalMs(totalMs, width)
    const frag = document.createDocumentFragment()
    for (let ms = 0; ms <= totalMs; ms += interval) {
      const left = map ? map.xOf(ms) : (ms / totalMs) * width
      const tick = el('div', { class: 'absolute top-0 h-full pointer-events-none' }, [
        el('div', { class: 'absolute bottom-0 h-2 w-px bg-line' }),
        el('div', { class: 'absolute top-0.5 whitespace-nowrap font-mono text-xs text-ink-muted' }, [
          formatMsShort(ms),
        ]),
      ])
      tick.style.left = left + 'px'
      frag.appendChild(tick)
    }
    root.appendChild(frag)
  }

  return { root, render }
}
