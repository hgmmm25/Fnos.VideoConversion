// C-07：时间线（01 §4.4 / 02 §6）
// 单轨 v1：Ruler + 片段块（宽度 ∝ 时长，最小 40px）+ 把手裁剪（左右各 6px）
//        + 拖拽换位（2px 蓝色插入线）+ 素材拖入 + 选中 + 播放头 + 工具条
// 坐标口径：轨道为**全局时间轴**（片段首尾相接，毫秒）；片段内为**源素材时间轴**
// 兜底宽度导致 x = ms/totalMs*width 不再成立 → 由本文件向 ruler 注入分段线性映射（ruler.ts）
import { el, svgIcon, toast, confirmDialog } from '../../ui'
import { api } from '../../api'
import type { AssetRef, EDLClip } from '../../types'
import { RENDER_PRESETS, type PresetOption } from './preset'
import { buildRuler, type RulerMap } from './ruler'
import { clipDurationMs, formatMs, EDL_LIMITS } from './format'
import type { EditorStore } from './editorStore'

/** 片段块最小宽度（01 §4.4：宽度 ∝ 时长，无缩放级别时最小 40px） */
const MIN_CLIP_PX = 40
/** 左/右边缘裁剪把手宽度（01 §4.4：6px） */
const HANDLE_PX = 6
/** 片段块高度 */
const ROW_H = 56
/** 修复④：filmstrip 单格最小宽度 / 最大格数（04 §2.4） */
const FRAME_MIN_CELL_PX = 56
const FRAME_MAX_CELLS = 8
/** 轨道最小宽度（容器更窄时也保证可拖放） */
const MIN_TRACK_PX = 320
/** 素材面板拖拽载荷 MIME（assets.ts 写入） */
const ASSET_MIME = 'application/x-wve-asset'
/** 内部片段换位载荷 MIME */
const CLIP_MIME = 'application/x-wve-clip'

/** 毫秒夹取（修复⑥：与 editorStore.trimClip 的 clamp 同口径） */
function clampMs(v: number, lo: number, hi: number): number {
  return v < lo ? lo : v > hi ? hi : v
}

export interface TimelineOptions {
  edlStore: EditorStore
  /** 定位：切到该素材并 seek 到源素材时间轴 ms（02 §6.4） */
  onLocate(file: string, ms: number, hint: { assetId: string; durationMs: number }): void
  /** 素材面板拖入 → 整段添加（复用 index.addWholeClip，含 atIndex） */
  onDropAsset(asset: AssetRef, atIndex: number): void
  /** [+ 添加当前素材]：按打点区间或整段添加（preview.addMarksToTimeline） */
  onAddCurrent(): void
  /** [渲染导出]：与顶栏同入口 */
  onRender(): void
  /** [渲染方案 ▾]：05 §5.1 presetKey 枚举（前端仅展示；代理专用项已排除，见 preset.ts） */
  presets: PresetOption[]
  selectedPreset: string
  onSelectPreset(key: string): void
  /** 素材面板已扫描素材：拖拽校验 + "素材已变更"检测（02 §7.3） */
  lookupAsset(assetId: string): AssetRef | undefined
}

export interface TimelinePanel {
  /** Ruler + 轨道（挂到 layout.timeline） */
  root: HTMLElement
  /** 工具条（挂到 layout.toolbar） */
  toolbar: HTMLElement
  /** 播放器时间回调（源素材时间轴）→ 播放头（02 §6.1） */
  setTime(localMs: number, file: string | null): void
  destroy(): void
}

interface Seg {
  clip: EDLClip
  x: number
  w: number
  /** 全局时间轴起点（ms） */
  startMs: number
  durMs: number
}

function baseName(file: string): string {
  return file.split('/').pop() || file
}

function clipBoxClass(selected: boolean, changed: boolean): string {
  return (
    'absolute top-1 rounded overflow-hidden border bg-elevated select-none cursor-pointer ' +
    (selected ? 'border-primary ring-1 ring-primary' : changed ? 'border-danger' : 'border-line')
  )
}

// ===== P1-5：clip 类型数据色（与列表页状态色 signal/success/danger 彻底解耦） =====
type ClipKind = 'video' | 'audio' | 'image' | 'text' | 'marker'
const CLIP_KIND_CLASS: Record<ClipKind, string> = {
  video: 'bg-clip-video',
  audio: 'bg-clip-audio',
  image: 'bg-clip-image',
  text: 'bg-clip-text',
  marker: 'bg-clip-marker',
}
const CLIP_KIND_LABEL: Record<ClipKind, string> = {
  video: '视频片段',
  audio: '音频片段',
  image: '图像片段',
  text: '文字片段',
  marker: '标记片段',
}
const VIDEO_EXTS = new Set(['mp4', 'mov', 'mkv', 'avi', 'webm', 'm4v', 'ts', 'm2ts', 'mts', 'wmv', 'flv', 'mpg', 'mpeg'])
const AUDIO_EXTS = new Set(['mp3', 'wav', 'aac', 'flac', 'm4a', 'ogg', 'opus', 'wma', 'ac3'])
const IMAGE_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg', 'tif', 'tiff'])
const TEXT_EXTS = new Set(['srt', 'ass', 'ssa', 'vtt', 'txt', 'sub'])
function clipKindOf(file: string): ClipKind {
  const ext = (file.split('.').pop() || '').toLowerCase()
  if (VIDEO_EXTS.has(ext)) return 'video'
  if (AUDIO_EXTS.has(ext)) return 'audio'
  if (IMAGE_EXTS.has(ext)) return 'image'
  if (TEXT_EXTS.has(ext)) return 'text'
  return 'video'
}

export function buildTimeline(opts: TimelineOptions): TimelinePanel {
  const { edlStore } = opts
  let destroyed = false

  // ===== 内部状态 =====
  /** 片段添加顺序（「排序: 按添加」依据；页面重载后按当前顺序兜底） */
  const addedSeq = new Map<string, number>()
  let seqCounter = 0
  let segs: Seg[] = []
  let totalMs = 0
  let trackW = MIN_TRACK_PX
  let clipEls: HTMLElement[] = []
  let changedIds = new Set<string>()
  let dragClipId: string | null = null
  let lastLocalMs = 0
  let lastFile: string | null = null
  let lastContainerW = 0

  // ===== 工具条（01 §4.4）=====
  const addBtn = el('button', { class: 'btn btn-sm flex items-center gap-1', title: '添加当前素材（按打点区间，未打点则整段）' })
  addBtn.append(svgIcon('plus', 14) as unknown as Node, el('span', {}, ['添加当前素材']))
  addBtn.onclick = () => opts.onAddCurrent()

  const delBtn = el('button', { class: 'btn btn-sm flex items-center gap-1', title: '删除选中片段（Delete）' })
  delBtn.append(svgIcon('trash', 14) as unknown as Node, el('span', {}, ['删除选中']))
  delBtn.onclick = () => removeSelected()

  const clearBtn = el('button', { class: 'btn btn-sm flex items-center gap-1', title: '清空时间线' })
  clearBtn.append(svgIcon('x', 14) as unknown as Node, el('span', {}, ['清空']))
  clearBtn.onclick = () => void clearAll()

  // 2026-09-20 UI 精简：删除「按添加 / 按素材名」排序下拉——
  // 排序会重排时间线片段顺序且选中后立即重置回「按添加」，状态不持久，作用有限

  const profileSel = el('select', { class: 'input text-xs w-44 shrink-0', title: '渲染方案（服务端 presetKey 枚举，05 §5.1）' }) as HTMLSelectElement
  profileSel.onchange = () => opts.onSelectPreset(profileSel.value)

  // 需求4/5：不再在时间线工具条重复渲染「渲染导出」按钮与「总时长」统计——
  // 渲染导出统一走顶栏（layout.ts renderBtn）；总时长统计统一由 editor/index.ts statsEl 提供。

  const toolbar = el('div', { class: 'flex items-center gap-1.5 w-full min-w-0' }, [
    addBtn,
    delBtn,
    clearBtn,
    profileSel,
  ])
  fillProfiles()

  // ===== 轨道 =====
  const empty = el(
    'div',
    { class: 'absolute inset-0 flex items-center justify-center text-xs text-ink-muted pointer-events-none px-4 text-center' },
    ['时间线为空：双击素材整段添加，或按住打点后点「添加当前素材」']
  )
  const insertLine = el('div', { class: 'absolute top-0 bottom-0 w-0.5 bg-primary z-20 pointer-events-none hidden' })
  const playhead = el('div', { class: 'absolute top-0 bottom-0 w-px bg-signal z-20 pointer-events-none hidden' })
  const track = el('div', { class: 'relative min-h-full' }, [empty, insertLine, playhead])
  track.style.height = ROW_H + 8 + 'px'

  const map: RulerMap = {
    xOf(ms: number): number {
      if (!segs.length) return 0
      const m = Math.max(0, ms)
      for (let i = 0; i < segs.length; i++) {
        const s = segs[i]
        if (m < s.startMs + s.durMs || i === segs.length - 1) {
          if (s.durMs <= 0) return s.x
          const r = Math.max(0, Math.min(1, (m - s.startMs) / s.durMs))
          return s.x + r * s.w
        }
      }
      return trackW
    },
    msOf(x: number): number {
      if (!segs.length) return 0
      const px = Math.max(0, x)
      for (let i = 0; i < segs.length; i++) {
        const s = segs[i]
        if (px < s.x + s.w || i === segs.length - 1) {
          if (s.w <= 0) return s.startMs
          const r = Math.max(0, Math.min(1, (px - s.x) / s.w))
          return s.startMs + r * s.durMs
        }
      }
      return totalMs
    },
    width: () => trackW,
  }

  const ruler = buildRuler((ms) => locateGlobal(ms), map)

  // ===== C-阶段（7.3 editor 行）：时间轴密度条 =====
  // 48 格统计片段重叠度，映射 4 档 token 色阶（低→高密度），纯 CSS 零依赖，不叠加图表库。
  const DENSITY_CLASS = ['bg-transparent', 'bg-primary/15', 'bg-primary/35', 'bg-primary/65', 'bg-primary']
  const density = el('div', { class: 'relative hidden h-3.5 shrink-0 border-b border-line bg-surface-alt' })
  density.setAttribute('role', 'img')
  density.setAttribute('aria-label', '时间轴片段密度')

  function drawDensity(): void {
    density.replaceChildren()
    if (!segs.length) {
      density.classList.add('hidden')
      return
    }
    density.classList.remove('hidden')
    const N = 48
    const cells = new Array<number>(N).fill(0)
    const segDur = totalMs / N
    for (const s of segs) {
      if (s.durMs <= 0) continue
      const i0 = Math.max(0, Math.floor(s.startMs / segDur))
      const i1 = Math.min(N - 1, Math.floor((s.startMs + s.durMs) / segDur))
      for (let i = i0; i <= i1; i++) cells[i]++
    }
    const desc: string[] = []
    for (let i = 0; i < N; i++) {
      const n = cells[i]
      // 绝对档位（非相对 max）：单轨无重叠时每格 1 层显示浅档，重叠≥2/3/5 逐级加深
      const level = n === 0 ? 0 : n >= 5 ? 4 : n >= 3 ? 3 : n === 2 ? 2 : 1
      const t0 = Math.round((i * segDur) / 1000)
      const t1 = Math.round(((i + 1) * segDur) / 1000)
      if (n > 0) desc.push(`${t0}s-${t1}s ${n} 片段`)
      const cellAttrs: Record<string, string> = { class: `flex-1 h-full ${DENSITY_CLASS[level]} transition-colors` }
      if (n > 0) cellAttrs.title = `${t0}s-${t1}s · ${n} 个片段重叠`
      const cell = el('div', cellAttrs)
      density.append(cell)
    }
    density.setAttribute('aria-label', `时间轴片段密度：${desc.join('，') || '无片段'}`)
  }

  const root = el('div', { class: 'min-h-full flex flex-col' }, [ruler.root, density, track])

  // ===== 几何：分段线性（含最小宽度兜底）=====
  function containerWidth(): number {
    const w = root.parentElement?.clientWidth || 0
    return Math.max(MIN_TRACK_PX, Math.floor(w) - 2)
  }

  function computeSegs(avail: number): void {
    const clips = edlStore.getState().clips
    const durs = clips.map((c) => clipDurationMs(c.inMs, c.outMs))
    const D = durs.reduce((a, b) => a + b, 0)
    totalMs = D
    segs = []
    if (!clips.length) {
      trackW = Math.max(MIN_TRACK_PX, avail)
      return
    }
    const width = Math.max(MIN_TRACK_PX, avail)
    if (D <= 0) {
      const w = Math.max(MIN_CLIP_PX, width / clips.length)
      let x = 0
      for (const c of clips) {
        segs.push({ clip: c, x, w, startMs: 0, durMs: 0 })
        x += w
      }
      trackW = Math.max(width, x)
      return
    }
    // 1) 先按比例；不足 MIN_CLIP_PX 的固定为 MIN_CLIP_PX，剩余空间在其余片段间再分配
    const fixed = new Array<boolean>(clips.length).fill(false)
    let remainW = width
    let remainD = D
    for (let pass = 0; pass < clips.length; pass++) {
      let changed = false
      for (let i = 0; i < clips.length; i++) {
        if (fixed[i]) continue
        const natural = (remainW * durs[i]) / remainD
        if (natural < MIN_CLIP_PX) {
          fixed[i] = true
          remainW -= MIN_CLIP_PX
          remainD -= durs[i]
          changed = true
        }
      }
      if (!changed) break
    }
    // 2) 逐段累计 x 与全局起点
    let x = 0
    let startMs = 0
    clips.forEach((c, i) => {
      const w = fixed[i] ? MIN_CLIP_PX : Math.max(MIN_CLIP_PX, (remainW * durs[i]) / remainD)
      segs.push({ clip: c, x, w, startMs, durMs: durs[i] })
      x += w
      startMs += durs[i]
    })
    trackW = Math.max(width, Math.ceil(x))
  }

  // ===== 渲染 =====
  function render(): void {
    if (destroyed) return
    const clips = edlStore.getState().clips
    for (const c of clips) if (!addedSeq.has(c.clipId)) addedSeq.set(c.clipId, ++seqCounter)
    lastContainerW = containerWidth()
    computeSegs(lastContainerW)
    track.style.width = trackW + 'px'
    ruler.root.style.width = trackW + 'px'
    drawClips(edlStore.getState().selectedClipId)
    drawDensity()
    ruler.render(totalMs, trackW)
    updatePlayhead()
  }

  function drawClips(selectedId: string | null): void {
    for (const n of clipEls) n.remove()
    clipEls = []
    changedIds = new Set<string>()
    if (!segs.length) {
      empty.classList.remove('hidden')
      return
    }
    empty.classList.add('hidden')
    segs.forEach((seg, i) => {
      const node = buildClipEl(seg, i)
      clipEls.push(node)
      track.append(node)
    })
    // 选中态在 append 之后统一应用（buildClipEl 内已知选中态，此处仅同步 changed 集合）
    for (const node of clipEls) {
      const cid = node.getAttribute('data-clip') || ''
      node.className = clipBoxClass(cid === selectedId, changedIds.has(cid))
    }
  }

  /** 修复④：按可见宽度自适应分格数（1~8 格；窄片段至少 1 格，避免请求风暴） */
  function frameCols(visibleW: number): number {
    return Math.max(1, Math.min(FRAME_MAX_CELLS, Math.floor(visibleW / FRAME_MIN_CELL_PX)))
  }

  function buildClipEl(seg: Seg, index: number): HTMLElement {
    const c = seg.clip
    const dur = seg.durMs
    const asset = opts.lookupAsset(c.assetId)
    const changed = !!asset && asset.durationMs > 0 && asset.durationMs !== c.sourceDurationMs
    if (changed) changedIds.add(c.clipId)
    // P1-5：clip 类型数据色（由素材扩展名推断；未知类型兜底 video）
    const kind = clipKindOf(c.file)

    const box = el('div', {
      class: clipBoxClass(false, changed),
      draggable: 'true',
      title: `${formatMs(c.inMs)} → ${formatMs(c.outMs)} · 时长 ${formatMs(dur)}${
        changed ? ' · 素材已变更，请复核出点' : ''
      }`,
      'data-clip': c.clipId,
    })
    box.style.left = seg.x + 'px'
    box.style.width = Math.max(MIN_CLIP_PX, seg.w) + 'px'
    box.style.height = ROW_H + 'px'

    // 修复④：缩略图按时间段分格（filmstrip）——原实现仅取入点单帧并被 object-cover 拉伸
    // 04 §2.4：格数随片段像素宽度自适应，每格取该时间段中点帧，避免相邻格取到同一关键帧
    const strip = el('div', { class: 'flex w-full overflow-hidden bg-neutral-soft' })
    strip.style.height = '30px'
    const cols = frameCols(Math.max(MIN_CLIP_PX, seg.w))
    for (let k = 0; k < cols; k++) {
      const cell = el('div', { class: 'flex-1 min-w-0 overflow-hidden' })
      const im = el('img', { class: 'w-full h-full object-cover', alt: '', loading: 'lazy' }) as HTMLImageElement
      const t = dur > 0 ? Math.round(c.inMs + ((k + 0.5) * dur) / cols) : Math.round(c.inMs)
      im.src = api.thumbUrl(c.file, Math.max(0, t), 'src')
      im.onerror = () => {
        im.removeAttribute('src')
        im.classList.add('opacity-30')
      }
      cell.append(im)
      strip.append(cell)
    }

    const label = el('div', { class: 'px-1 pt-0.5 text-xs leading-tight text-ink truncate' }, [
      `#${index + 1} ${baseName(c.file).slice(0, 12)}`,
    ])
    const durEl = el('div', { class: 'px-1 text-xs text-ink-muted truncate font-mono' }, [formatMs(dur)])

    const hIn = el('div', { class: 'absolute left-0 top-0 bottom-0 cursor-ew-resize hover:bg-primary/40', title: '拖动调整入点（, / . 可逐帧微调）' })
    hIn.style.width = HANDLE_PX + 'px'
    const hOut = el('div', { class: 'absolute right-0 top-0 bottom-0 cursor-ew-resize hover:bg-primary/40', title: '拖动调整出点（, / . 可逐帧微调）' })
    hOut.style.width = HANDLE_PX + 'px'

    const del = el('button', { class: 'absolute top-0.5 right-1 text-xs leading-none px-1 py-0.5 rounded bg-black/60 text-white hidden' }, ['×'])
    del.title = '删除该片段'
    del.onclick = (e) => {
      e.stopPropagation()
      edlStore.dispatch({ type: 'removeClip', clipId: c.clipId })
    }

    box.append(strip, label, durEl, hIn, hOut, del)
    // P1-5：左侧 3px 类型色条（数据色承载 clip 类型语义；选中/变更状态仍由 border 表达）
    const kindBar = el('span', {
      class: 'absolute left-0 top-0 bottom-0 w-[3px] pointer-events-none ' + CLIP_KIND_CLASS[kind],
      title: CLIP_KIND_LABEL[kind],
    })
    box.append(kindBar)
    box.addEventListener('mouseenter', () => del.classList.remove('hidden'))
    box.addEventListener('mouseleave', () => del.classList.add('hidden'))
    box.onclick = (e) => {
      e.stopPropagation()
      edlStore.dispatch({ type: 'select', clipId: c.clipId })
    }
    box.addEventListener('dragstart', (e) => {
      dragClipId = c.clipId
      box.classList.add('opacity-50')
      if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = 'move'
        e.dataTransfer.setData(CLIP_MIME, c.clipId)
        e.dataTransfer.setData('text/plain', c.clipId)
      }
    })
    box.addEventListener('dragend', () => {
      dragClipId = null
      box.classList.remove('opacity-50')
      hideInsertLine()
    })
    hIn.addEventListener('pointerdown', (e) => startTrim(e, c, 'in', seg))
    hOut.addEventListener('pointerdown', (e) => startTrim(e, c, 'out', seg))
    return box
  }

  /** 把手裁剪：拖动期间只做视觉预览，松手才 dispatch（单次入撤销栈，02 §5.2）
   *  修复⑥：换算比例与「实际绘制宽度」统一（w0/dur0），并在毫秒域按 store 同口径夹取，
   *  解决窄片段被 MIN_CLIP_PX 撑大后"位移与时长换算不一致"的问题；左把手同时移动左边界。
   */
  function startTrim(e: PointerEvent, clip: EDLClip, edge: 'in' | 'out', seg: Seg): void {
    e.preventDefault()
    e.stopPropagation()
    const handle = e.currentTarget as HTMLElement
    const box = handle.parentElement as HTMLElement | null
    const startX = e.clientX
    const w0 = Math.max(MIN_CLIP_PX, seg.w)
    const dur0 = seg.durMs > 0 ? seg.durMs : clipDurationMs(clip.inMs, clip.outMs)
    const pxPerMs = dur0 > 0 ? w0 / dur0 : 0
    const minMs = EDL_LIMITS.minClipMs
    const base = edge === 'in' ? clip.inMs : clip.outMs
    // 与 editorStore.trimClip 同口径的合法区间（in ∈ [0, out-min]；out ∈ [in+min, 源时长]）
    const srcDur = clip.sourceDurationMs && clip.sourceDurationMs > 0 ? clip.sourceDurationMs : Number.MAX_SAFE_INTEGER
    let ms = base
    edlStore.dispatch({ type: 'select', clipId: clip.clipId })
    handle.setPointerCapture(e.pointerId)

    const move = (ev: PointerEvent) => {
      const raw = pxPerMs > 0 ? (ev.clientX - startX) / pxPerMs : 0
      const dms =
        edge === 'in'
          ? clampMs(raw, -clip.inMs, dur0 - minMs)
          : clampMs(raw, -(dur0 - minMs), srcDur - clip.outMs)
      ms = base + dms
      if (!box) return
      const dx = dms * pxPerMs
      if (edge === 'in') {
        // 左把手：拖哪边动哪边（原实现固定 left 只改宽度，视觉与鼠标不同步）
        box.style.left = seg.x + dx + 'px'
        box.style.width = Math.max(MIN_CLIP_PX, w0 - dx) + 'px'
      } else {
        box.style.width = Math.max(MIN_CLIP_PX, w0 + dx) + 'px'
      }
    }
    const up = () => {
      handle.removeEventListener('pointermove', move)
      handle.removeEventListener('pointerup', up)
      handle.removeEventListener('pointercancel', up)
      edlStore.dispatch({ type: 'trimClip', clipId: clip.clipId, edge, ms: Math.round(ms) })
    }
    handle.addEventListener('pointermove', move)
    handle.addEventListener('pointerup', up)
    handle.addEventListener('pointercancel', up)
  }

  // ===== 插入线 / 拖放 =====
  function indexAtX(clientX: number): number {
    const r = track.getBoundingClientRect()
    const x = clientX - r.left
    for (let i = 0; i < segs.length; i++) {
      if (x < segs[i].x + segs[i].w / 2) return i
    }
    return segs.length
  }

  function showInsertLine(idx: number): void {
    const x = idx <= 0 ? 0 : idx >= segs.length ? trackW : segs[idx].x
    insertLine.style.left = x + 'px'
    insertLine.classList.remove('hidden')
  }

  function hideInsertLine(): void {
    insertLine.classList.add('hidden')
  }

  track.addEventListener('dragover', (e) => {
    const dt = e.dataTransfer
    if (!dt) return
    const types = Array.from(dt.types || [])
    const fromAssets = types.includes(ASSET_MIME)
    const fromClips = types.includes(CLIP_MIME)
    if (!fromAssets && !fromClips) return
    e.preventDefault()
    dt.dropEffect = fromAssets ? 'copy' : 'move'
    showInsertLine(indexAtX(e.clientX))
  })

  track.addEventListener('dragleave', (e) => {
    if (e.target === track) hideInsertLine()
  })

  track.addEventListener('drop', (e) => {
    const dt = e.dataTransfer
    if (!dt) return
    const idx = indexAtX(e.clientX)
    hideInsertLine()
    const raw = dt.getData(ASSET_MIME)
    if (raw) {
      e.preventDefault()
      try {
        const asset = JSON.parse(raw) as AssetRef
        if (asset && asset.assetId && asset.file) opts.onDropAsset(asset, idx)
      } catch {
        toast('拖入的素材数据无法解析', 'error')
      }
      return
    }
    const clipId = dt.getData(CLIP_MIME) || dragClipId
    if (!clipId) return
    e.preventDefault()
    const clips = edlStore.getState().clips
    const srcIdx = clips.findIndex((c) => c.clipId === clipId)
    if (srcIdx < 0) return
    const toIndex = srcIdx < idx ? idx - 1 : idx
    edlStore.dispatch({ type: 'moveClip', clipId, toIndex })
  })

  // 轨道空白点击 → 定位
  track.addEventListener('pointerdown', (e) => {
    if (e.target !== track) return
    const r = track.getBoundingClientRect()
    locateGlobal(map.msOf(e.clientX - r.left))
  })

  // ===== 定位 / 播放头 =====
  function segAtGlobal(g: number): Seg | null {
    if (!segs.length) return null
    for (const s of segs) {
      if (g >= s.startMs && g < s.startMs + s.durMs) return s
    }
    return segs[segs.length - 1]
  }

  function locateGlobal(g: number): void {
    const seg = segAtGlobal(g)
    if (!seg) return
    const local = Math.round(seg.clip.inMs + Math.max(0, Math.min(seg.durMs, g - seg.startMs)))
    edlStore.dispatch({ type: 'select', clipId: seg.clip.clipId })
    opts.onLocate(seg.clip.file, local, { assetId: seg.clip.assetId, durationMs: seg.clip.sourceDurationMs })
  }

  function updatePlayhead(): void {
    if (!lastFile) {
      playhead.classList.add('hidden')
      return
    }
    const seg = segs.find(
      (s) => s.clip.file === lastFile && lastLocalMs >= s.clip.inMs && lastLocalMs <= s.clip.outMs
    )
    if (!seg) {
      playhead.classList.add('hidden')
      return
    }
    playhead.classList.remove('hidden')
    playhead.style.left = map.xOf(seg.startMs + (lastLocalMs - seg.clip.inMs)) + 'px'
  }

  // ===== 工具条动作 =====
  function removeSelected(): void {
    const id = edlStore.getState().selectedClipId
    if (!id) {
      toast('请先选中片段', 'error')
      return
    }
    edlStore.dispatch({ type: 'removeClip', clipId: id })
  }

  async function clearAll(): Promise<void> {
    if (!edlStore.getState().clips.length) return
    if (!(await confirmDialog('确定清空时间线？该操作可用 Ctrl+Z 撤销。'))) return
    edlStore.dispatch({ type: 'clear' })
  }

  function fillProfiles(): void {
    profileSel.innerHTML = ''
    const list = opts.presets && opts.presets.length ? opts.presets : RENDER_PRESETS
    if (!list.length) {
      profileSel.append(el('option', { value: '' }, ['无可用方案']))
      profileSel.disabled = true
      return
    }
    profileSel.disabled = false
    for (const p of list) {
      profileSel.append(el('option', { value: p.key, title: p.args }, [p.label]))
    }
    const wanted = opts.selectedPreset
    profileSel.value = wanted && list.some((p) => p.key === wanted) ? wanted : list[0].key
    if (profileSel.value !== opts.selectedPreset) opts.onSelectPreset(profileSel.value)
  }

  // ===== 订阅与生命周期 =====
  const unsub = edlStore.subscribe((s, changed) => {
    if (changed.includes('clips')) render()
    else if (changed.includes('selection')) {
      for (const node of clipEls) {
        const cid = node.getAttribute('data-clip') || ''
        node.className = clipBoxClass(cid === s.selectedClipId, changedIds.has(cid))
      }
    }
  })

  const ro = new ResizeObserver(() => {
    if (destroyed) return
    const w = containerWidth()
    // 宽度未变则不重绘，避免与 track.style.width 互相触发
    if (Math.abs(w - lastContainerW) < 2) return
    render()
  })
  if (root.parentElement) ro.observe(root.parentElement)
  else ro.observe(root)

  render()

  return {
    root,
    toolbar,
    setTime(localMs: number, file: string | null) {
      lastLocalMs = localMs
      lastFile = file
      updatePlayhead()
    },
    destroy() {
      destroyed = true
      ro.disconnect()
      unsub()
      root.remove()
      toolbar.remove()
    },
  }
}
