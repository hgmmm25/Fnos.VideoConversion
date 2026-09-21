// C-05/C-06：预览器与打点（01 §4.3 / 02 §7）
// 播放源：POST /stream/ticket 取票据 → GET /stream?path&ticket&root（04 §2.2，Range 支持拖拽）
// 打点基准恒为**源素材**时间轴：inMs/outMs = round(video.currentTime * 1000)（01 §4.3）
import { el, svgIcon, toast } from '../../ui'
import { api, ApiError } from '../../api'
import type { AssetRef, EDLClip, PreviewAsset } from '../../types'
import {
  clipDurationMs,
  EDL_LIMITS,
  formatMs,
  frameDeltaMs,
  msToFrame,
  msToSec,
  newClipId,
  secToMs,
} from './format'
import type { EditorStore } from './editorStore'

export interface PreviewOptions {
  edlStore: EditorStore
  /** 添加到时间线（index.ts 的 addMarkedClip：含时长校验与 toast 反馈） */
  onAddClip(clip: EDLClip): void
  /** 预览器初始静音态（来自 Settings.playerMuted；缺省 true 保持既有行为） */
  initialMuted?: boolean
}

/** reveal 定位提示：素材元信息 + 已就绪代理（有代理时预览改走 proxy 根） */
export interface RevealHint {
  assetId: string
  durationMs: number
  /** 代理相对路径（WS proxy_ready 后由素材面板回填） */
  proxyFile?: string
}

export interface PreviewController {
  root: HTMLElement
  /** 切素材（必要时）并 seek 到源时间轴 ms；时间线/ruler 定位用（02 §6.4） */
  reveal(file: string, ms: number, hint?: RevealHint): void
  seekMs(ms: number): void
  play(): void
  pause(): void
  toggle(): void
  /** 变速播放（PR 惯例）：dir=-1 快退 / 1 快进 / 0 停止；同向按 2x→3x→4x 递增 */
  shuttle(dir: -1 | 0 | 1): void
  /** 运行时切换静音（设置变更 / 外部联动） */
  setMuted(muted: boolean): void
  /** 当前是否静音 */
  isMuted(): boolean
  /** 时间更新订阅（播放头驱动，02 §6.1）；返回取消订阅函数 */
  onTime(fn: (ms: number, file: string | null) => void): () => void
  markIn(): void
  markOut(): void
  clearMarks(): void
  addMarksToTimeline(): void
  destroy(): void
}

/** 素材面板点击 → 预览器装配体（04 §4.3：代理就绪走 proxy，否则回退 src） */
export function toPreviewAsset(a: AssetRef, fps: number): PreviewAsset {
  const useProxy = a.proxyState === 'ready' && !!a.proxyFile
  return {
    assetId: a.assetId,
    file: a.file,
    root: useProxy ? 'proxy' : 'src',
    path: useProxy ? (a.proxyFile as string) : a.file,
    durationMs: a.durationMs,
    fps: fps || 30,
    visibility: a.visibility,
    proxyFile: a.proxyFile,
    canMark: true,
    proxyMismatch: false,
  }
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, n))
}

function errText(e: unknown): string {
  if (e instanceof ApiError) return e.code && e.code !== 'E_UNKNOWN' ? `${e.message}（${e.code}）` : e.message
  return e instanceof Error ? e.message : String(e)
}

export function buildPreview(opts: PreviewOptions): PreviewController {
  const { edlStore } = opts

  const video = el('video', { class: 'max-h-full max-w-full' }) as HTMLVideoElement
  video.preload = 'auto'
  // 2026-09-16 修复：静音默认值不再硬编码，改由 Settings.playerMuted 决定（缺省 true）
  video.muted = opts.initialMuted !== false
  video.setAttribute('playsinline', 'true')

  const tcEl = el('div', { class: 'px-1.5 py-0.5 rounded bg-black/60 text-white font-mono text-xs' }, [
    'TC 00:00:00.000',
  ])
  const frameEl = el('div', { class: 'px-1.5 py-0.5 rounded bg-black/60 text-white font-mono text-xs' }, [
    '帧 0 / 00:00:00.000',
  ])
  const proxyBadge = el('div', { class: 'px-1.5 py-0.5 rounded bg-warning text-white text-xs hidden' }, [
    'PROXY',
  ])
  const mismatchBadge = el('div', { class: 'px-1.5 py-0.5 rounded bg-danger text-white text-xs hidden' }, [
    '代理与源时长不一致 · 已禁用打点',
  ])
  const hintEl = el(
    'div',
    // P0-2：预览区为恒定深底，辅助文字使用预览区专用高对比令牌 text-preview-ink
    { class: 'absolute inset-0 flex items-center justify-center text-xs text-preview-ink pointer-events-none px-4 text-center' },
    ['点击左侧素材预览；双击或拖拽到时间线可整段添加']
  )

  const overlay = el('div', { class: 'absolute top-2 right-2 flex flex-col items-end gap-1' }, [
    tcEl,
    frameEl,
    proxyBadge,
    mismatchBadge,
  ])
  const videoWrap = el('div', { class: 'relative flex-1 min-h-0 flex items-center justify-center overflow-hidden' }, [
    video,
    hintEl,
    overlay,
  ])

  // ===== 播放控制（01 §4.3）=====
  const playBtn = el('button', { class: 'btn btn-sm flex items-center', title: '播放 / 暂停（空格）' })
  function setPlayIcon(playing: boolean) {
    playBtn.innerHTML = ''
    playBtn.append(svgIcon(playing ? 'pause' : 'play', 14))
  }
  setPlayIcon(false)

  function stepBtn(label: string, ms: number, title: string): HTMLButtonElement {
    const b = el('button', { class: 'btn btn-sm', title }) as HTMLButtonElement
    b.textContent = label
    b.onclick = () => step(ms)
    return b
  }

  playBtn.onclick = () => toggle()
  const back5 = stepBtn('−5s', -5000, '后退 5 秒')
  const back1 = stepBtn('−1s', -1000, '后退 1 秒')
  const prevFrame = stepBtn('−1帧', 0, '上一帧（←）')
  const nextFrame = stepBtn('+1帧', 0, '下一帧（→）')
  const fwd1 = stepBtn('+1s', 1000, '前进 1 秒')
  const fwd5 = stepBtn('+5s', 5000, '前进 5 秒')

  // ===== 变速播放（PR 惯例 J/K/L）：同向 2x→3x→4x，最大 4x；反向重置回 1 档 =====
  const SHUTTLE_RATES = [1, 2, 3, 4]
  let shuttleDir: -1 | 0 | 1 = 0
  let shuttleLevel = 0
  let scrubTimer: number | null = null
  const rewindBtn = el('button', { class: 'btn btn-sm flex items-center', title: '快退（J）：2x → 3x → 4x' }) as HTMLButtonElement
  const ffBtn = el('button', { class: 'btn btn-sm flex items-center', title: '快进（L）：2x → 3x → 4x' }) as HTMLButtonElement
  function syncShuttleBtns(): void {
    rewindBtn.innerHTML = ''
    rewindBtn.append(svgIcon('rewind', 14) as unknown as Node)
    ffBtn.innerHTML = ''
    ffBtn.append(svgIcon('fast-forward', 14) as unknown as Node)
    const b = shuttleDir < 0 ? rewindBtn : shuttleDir > 0 ? ffBtn : null
    if (b && shuttleLevel > 0) {
      b.append(el('span', { class: 'text-xs leading-none font-mono' }, [SHUTTLE_RATES[shuttleLevel] + 'x']))
    }
  }
  rewindBtn.onclick = () => shuttle(-1)
  ffBtn.onclick = () => shuttle(1)
  function stopSeekScrub(): void {
    if (scrubTimer !== null) {
      clearInterval(scrubTimer)
      scrubTimer = null
    }
  }
  function startSeekScrub(): void {
    stopSeekScrub()
    // 负速率不可用（如 Safari）时的降级：每 250ms 向后跳 速率×250ms，模拟倒放
    scrubTimer = window.setInterval(() => {
      const delta = SHUTTLE_RATES[shuttleLevel] * 250
      seekMs(currentMs() + (shuttleDir < 0 ? -delta : delta))
    }, 250)
  }
  function resetShuttle(): void {
    shuttleDir = 0
    shuttleLevel = 0
    stopSeekScrub()
    try {
      video.playbackRate = 1
    } catch {
      /* 忽略 */
    }
    syncShuttleBtns()
  }
  function applyShuttle(): void {
    stopSeekScrub()
    const target = shuttleDir * SHUTTLE_RATES[shuttleLevel]
    let ok = false
    try {
      video.playbackRate = target
      // 负速率支持检测：立即回读，未生效则降级 seek 模拟
      ok = target > 0 ? video.playbackRate === target : video.playbackRate <= 0
    } catch {
      ok = false
    }
    if (!ok && target < 0) startSeekScrub()
    syncShuttleBtns()
  }
  function shuttle(dir: -1 | 0 | 1): void {
    if (!asset) return
    if (dir === 0) {
      resetShuttle()
      return
    }
    if (video.paused) void play()
    if (shuttleDir === dir) shuttleLevel = Math.min(3, shuttleLevel + 1)
    else {
      shuttleDir = dir
      shuttleLevel = 1
    }
    applyShuttle()
  }
  syncShuttleBtns()

  const muteBtn = el('button', { class: 'btn btn-sm flex items-center' }) as HTMLButtonElement
  muteBtn.title = '静音开关'
  // 功能3（预览位）：最大化按钮 —— 铺满视口（再次点击 / Esc 还原）
  let previewFullscreen = false
  const maxBtn = el('button', { class: 'btn btn-sm flex items-center' }) as HTMLButtonElement
  maxBtn.title = '最大化预览窗口'
  function syncMaxBtn() {
    maxBtn.innerHTML = ''
    maxBtn.append(svgIcon(previewFullscreen ? 'minimize' : 'maximize', 14) as unknown as Node)
    maxBtn.title = previewFullscreen ? '还原预览窗口' : '最大化预览窗口'
  }
  function setPreviewFullscreen(on: boolean): void {
    if (previewFullscreen === on) return
    previewFullscreen = on
    root.classList.toggle('editor-preview-fullscreen', on)
    syncMaxBtn()
  }
  maxBtn.onclick = () => setPreviewFullscreen(!previewFullscreen)
  function onEsc(e: KeyboardEvent): void {
    if (e.key === 'Escape' && previewFullscreen) {
      e.preventDefault()
      setPreviewFullscreen(false)
    }
  }
  window.addEventListener('keydown', onEsc)
  function syncMuteBtn() {
    muteBtn.innerHTML = ''
    muteBtn.append(svgIcon(video.muted ? 'mute' : 'volume', 14) as unknown as Node)
    muteBtn.title = video.muted ? '取消静音' : '静音'
  }
  function setMuted(muted: boolean) {
    video.muted = muted
    syncMuteBtn()
  }
  muteBtn.onclick = () => {
    video.muted = !video.muted
    syncMuteBtn()
  }
  syncMuteBtn()
  prevFrame.onclick = () => step(-frameDeltaMs(1, fps()))
  nextFrame.onclick = () => step(frameDeltaMs(1, fps()))

  const controlsRow = el('div', { class: 'flex items-center gap-1 flex-wrap' }, [
    back5,
    back1,
    prevFrame,
    rewindBtn,
    playBtn,
    ffBtn,
    nextFrame,
    fwd1,
    fwd5,
    muteBtn,
    maxBtn,
  ])

  // ===== 进度条（拖拽定位）=====
  const barBg = el('div', { class: 'absolute inset-x-0 h-1.5 rounded bg-neutral-soft' })
  const markRange = el('div', { class: 'absolute h-1.5 rounded bg-signal/60 hidden' })
  const played = el('div', { class: 'absolute inset-y-0 left-0 h-1.5 rounded bg-primary' })
  const head = el('div', { class: 'absolute w-2 h-2 rounded-full bg-primary -translate-x-1/2' })
  const track = el('div', { class: 'relative h-4 flex items-center cursor-pointer select-none' }, [
    barBg,
    markRange,
    played,
    head,
  ])

  // ===== 打点区（C-06）=====
  const inBtn = el('button', { class: 'btn btn-sm', title: '设为入点（I）' }) as HTMLButtonElement
  inBtn.textContent = '['
  const outBtn = el('button', { class: 'btn btn-sm', title: '设为出点（O）' }) as HTMLButtonElement
  outBtn.textContent = ']'
  const clearBtn = el('button', { class: 'btn btn-sm flex items-center', title: '清除打点' }) as HTMLButtonElement
  clearBtn.append(svgIcon('x', 14) as unknown as Node)
  const addBtn = el('button', { class: 'btn btn-sm btn-primary' }, ['添加到时间线']) as HTMLButtonElement
  const marksText = el('span', { class: 'text-xs text-ink-muted font-mono' }, ['入 --:--:--.--- · 出 --:--:--.---'])

  inBtn.onclick = () => markIn()
  outBtn.onclick = () => markOut()
  clearBtn.onclick = () => clearMarks()
  addBtn.onclick = () => addMarksToTimeline()

  const marksRow = el('div', { class: 'flex items-center gap-1 flex-wrap' }, [
    inBtn,
    outBtn,
    clearBtn,
    addBtn,
    marksText,
  ])

  const controls = el('div', { class: 'shrink-0 px-3 py-2 flex flex-col gap-1.5 border-t border-line bg-surface' }, [
    controlsRow,
    track,
    marksRow,
  ])

  const root = el('div', { class: 'h-full flex flex-col min-h-0' }, [videoWrap, controls])

  // ===== 内部状态 =====
  let asset: PreviewAsset | null = null
  let loadSeq = 0
  let retried = false
  let pendingSeek: number | null = null
  let destroyed = false
  let rafId: number | null = null
  const lastPos = new Map<string, { ms: number; at: number }>()
  const timeListeners = new Set<(ms: number, file: string | null) => void>()

  function fps(): number {
    return asset?.fps || edlStore.getState().timeline.fps || 30
  }

  function currentMs(): number {
    return secToMs(video.currentTime || 0)
  }

  function durationMs(): number {
    if (asset && asset.durationMs > 0) return asset.durationMs
    return secToMs(video.duration || 0)
  }

  function rememberPos() {
    if (asset && video.readyState >= 1) {
      lastPos.set(asset.file, { ms: currentMs(), at: Date.now() })
    }
  }

  // ===== 装配 / 换素材 =====
  async function loadAsset(a: PreviewAsset | null) {
    const seq = ++loadSeq
    rememberPos()
    asset = a
    retried = false
    hintEl.classList.toggle('hidden', !!a)
    if (!a) {
      video.removeAttribute('src')
      video.load()
      tick()
      renderMarks()
      return
    }
    proxyBadge.classList.toggle('hidden', a.root !== 'proxy')
    mismatchBadge.classList.toggle('hidden', !a.proxyMismatch)
    try {
      const r = await api.createStreamTicket({ path: a.path, root: a.root })
      if (seq !== loadSeq || destroyed) return
      if (pendingSeek === null) {
        const saved = lastPos.get(a.file)
        if (saved && Date.now() - saved.at <= 3000) pendingSeek = saved.ms
      }
      video.src = api.streamUrl(a.path, r.ticket, a.root)
      video.load()
    } catch (e) {
      if (seq !== loadSeq) return
      toast('预览票据申请失败：' + errText(e), 'error')
    }
  }

  async function reloadCurrent() {
    const a = asset
    if (!a || destroyed) return
    try {
      const r = await api.createStreamTicket({ path: a.path, root: a.root })
      if (destroyed || asset !== a) return
      video.src = api.streamUrl(a.path, r.ticket, a.root)
      video.load()
    } catch (e) {
      toast('预览票据申请失败：' + errText(e), 'error')
    }
  }

  // ===== 播放控制 =====
  function play() {
    if (!asset) return
    void video.play().catch(() => {
      /* 自动播放被拦截或流未就绪，忽略 */
    })
  }

  function pause() {
    video.pause()
  }

  function toggle() {
    if (!asset) return
    if (video.paused) play()
    else pause()
  }

  function seekMs(ms: number) {
    if (!asset) return
    const dur = durationMs()
    const v = dur > 0 ? clamp(Math.round(ms), 0, dur) : Math.max(0, Math.round(ms))
    try {
      video.currentTime = msToSec(v)
    } catch {
      /* metadata 未就绪时忽略，后续 loadedmetadata 会补 seek */
      pendingSeek = v
    }
    tick()
  }

  function step(ms: number) {
    if (!asset) return
    if (ms === 0) return
    seekMs(currentMs() + ms)
  }

  // ===== 打点 =====
  function canMark(): boolean {
    return !!asset && asset.canMark && !asset.proxyMismatch
  }

  function markIn() {
    if (!asset) {
      toast('请先选择素材', 'error')
      return
    }
    if (!canMark()) {
      toast('当前素材不支持打点（代理与源时长不一致，请重建代理）', 'error')
      return
    }
    const s = edlStore.getState()
    const ms = clamp(currentMs(), 0, durationMs())
    if (s.outMs !== null && ms >= s.outMs) edlStore.dispatch({ type: 'clearMarks' })
    edlStore.dispatch({ type: 'setIn', ms })
  }

  function markOut() {
    if (!asset) {
      toast('请先选择素材', 'error')
      return
    }
    if (!canMark()) {
      toast('当前素材不支持打点（代理与源时长不一致，请重建代理）', 'error')
      return
    }
    const s = edlStore.getState()
    const ms = clamp(currentMs(), 0, durationMs())
    if (s.inMs !== null && ms <= s.inMs) {
      toast('出点需晚于入点', 'error')
      return
    }
    edlStore.dispatch({ type: 'setOut', ms })
  }

  function clearMarks() {
    edlStore.dispatch({ type: 'clearMarks' })
  }

  function addMarksToTimeline() {
    if (!asset) {
      toast('请先选择素材', 'error')
      return
    }
    const s = edlStore.getState()
    const inMs = s.inMs ?? 0
    const outMs = s.outMs ?? asset.durationMs
    if (clipDurationMs(inMs, outMs) < EDL_LIMITS.minClipMs) {
      toast(`区间不足 ${EDL_LIMITS.minClipMs}ms，无法添加`, 'error')
      return
    }
    opts.onAddClip({
      clipId: newClipId(),
      assetId: asset.assetId,
      file: asset.file,
      inMs,
      outMs,
      speed: 1.0,
      sourceDurationMs: asset.durationMs,
      transition: null,
    })
  }

  /** 定位：必要时先切素材，再 seek（02 §6.4） */
  function reveal(file: string, ms: number, hint?: RevealHint) {
    const wantProxy = !!hint?.proxyFile
    if (asset && asset.file === file) {
      // 同一素材但代理刚就绪 → 无需重建 src（代理仅影响首次装配），直接 seek
      seekMs(ms)
      return
    }
    if (!hint) {
      seekMs(ms)
      return
    }
    const st = edlStore.getState()
    pendingSeek = Math.round(ms)
    const next: PreviewAsset = {
      assetId: hint.assetId,
      file,
      root: wantProxy ? 'proxy' : 'src',
      path: wantProxy ? String(hint.proxyFile) : file,
      durationMs: hint.durationMs,
      fps: st.timeline.fps || 30,
      visibility: wantProxy ? 'proxy_ready' : 'direct',
      proxyFile: hint.proxyFile,
      canMark: true,
    }
    edlStore.dispatch({ type: 'setPreviewAsset', asset: next })
  }

  // ===== 渲染 =====
  function tick() {
    const ms = currentMs()
    const dur = durationMs()
    tcEl.textContent = 'TC ' + formatMs(ms)
    frameEl.textContent = `帧 ${msToFrame(ms, fps())} / ${formatMs(dur)}`
    const pct = dur > 0 ? clamp(ms / dur, 0, 1) : 0
    played.style.width = pct * 100 + '%'
    head.style.left = pct * 100 + '%'
    for (const fn of timeListeners) fn(ms, asset ? asset.file : null)
  }

  function renderMarks() {
    const s = edlStore.getState()
    const dur = s.previewAsset?.durationMs || 0
    if (s.inMs === null && s.outMs === null) {
      markRange.classList.add('hidden')
    } else if (dur > 0) {
      const a = clamp(s.inMs ?? 0, 0, dur)
      const b = clamp(s.outMs ?? dur, 0, dur)
      markRange.classList.remove('hidden')
      markRange.style.left = (a / dur) * 100 + '%'
      markRange.style.width = (Math.max(0, b - a) / dur) * 100 + '%'
    }
    marksText.textContent = `入 ${s.inMs === null ? '--:--:--.---' : formatMs(s.inMs)} · 出 ${
      s.outMs === null ? '--:--:--.---' : formatMs(s.outMs)
    }`
    const disabled = !s.previewAsset || !s.previewAsset.canMark || !!s.previewAsset.proxyMismatch
    inBtn.disabled = disabled
    outBtn.disabled = disabled
    addBtn.disabled = disabled
    clearBtn.disabled = s.inMs === null && s.outMs === null
  }

  function startRaf() {
    if (rafId !== null) return
    const loop = () => {
      tick()
      rafId = video.paused ? null : window.requestAnimationFrame(loop)
    }
    rafId = window.requestAnimationFrame(loop)
  }

  function stopRaf() {
    if (rafId !== null) {
      window.cancelAnimationFrame(rafId)
      rafId = null
    }
  }

  // ===== 事件 =====
  video.addEventListener('loadedmetadata', () => {
    if (pendingSeek !== null) {
      try {
        video.currentTime = msToSec(pendingSeek)
      } catch {
        /* 忽略 */
      }
      pendingSeek = null
    }
    tick()
  })
  video.addEventListener('timeupdate', () => {
    if (video.paused) tick()
  })
  video.addEventListener('play', () => {
    setPlayIcon(true)
    startRaf()
  })
  video.addEventListener('pause', () => {
    setPlayIcon(false)
    stopRaf()
    tick()
    resetShuttle()
  })
  video.addEventListener('ended', () => {
    setPlayIcon(false)
    stopRaf()
    tick()
    resetShuttle()
  })
  video.addEventListener('error', () => {
    if (!asset) return
    if (!retried) {
      // 断流重建一次（换票重连），仍失败才提示（02 §7.1）
      retried = true
      void reloadCurrent()
      return
    }
    toast('预览流加载失败，可尝试生成代理后重试', 'error')
  })

  track.addEventListener('pointerdown', (e) => {
    if (!asset) return
    e.preventDefault()
    track.setPointerCapture(e.pointerId)
    const seekAt = (ev: PointerEvent) => {
      const r = track.getBoundingClientRect()
      const pct = r.width > 0 ? clamp((ev.clientX - r.left) / r.width, 0, 1) : 0
      seekMs(Math.round(pct * durationMs()))
    }
    seekAt(e)
    const move = (ev: PointerEvent) => seekAt(ev)
    const up = () => {
      track.removeEventListener('pointermove', move)
      track.removeEventListener('pointerup', up)
      track.removeEventListener('pointercancel', up)
    }
    track.addEventListener('pointermove', move)
    track.addEventListener('pointerup', up)
    track.addEventListener('pointercancel', up)
  })

  // ===== store 订阅 =====
  const unsub = edlStore.subscribe((s, changed) => {
    if (changed.includes('preview')) void loadAsset(s.previewAsset)
    if (changed.includes('marks')) renderMarks()
    if (changed.includes('selection')) {
      const clip = s.clips.find((c) => c.clipId === s.selectedClipId)
      if (!clip) return
      if (asset && asset.file === clip.file) {
        seekMs(clip.inMs)
        return
      }
      const file = clip.file
      const ms = clip.inMs
      const hint = { assetId: clip.assetId, durationMs: clip.sourceDurationMs }
      // 避免在 store 通知过程中再次派发（重入）
      window.setTimeout(() => {
        if (!destroyed) reveal(file, ms, hint)
      }, 0)
    }
  })

  renderMarks()
  tick()

  return {
    root,
    reveal,
    seekMs,
    play,
    pause,
    toggle,
    shuttle,
    setMuted,
    isMuted: () => video.muted,
    onTime(fn) {
      timeListeners.add(fn)
      return () => timeListeners.delete(fn)
    },
    markIn,
    markOut,
    clearMarks,
    addMarksToTimeline,
    destroy() {
      destroyed = true
      stopRaf()
      unsub()
      window.removeEventListener('keydown', onEsc)
      timeListeners.clear()
      video.removeAttribute('src')
      video.load()
      root.remove()
    },
  }
}
