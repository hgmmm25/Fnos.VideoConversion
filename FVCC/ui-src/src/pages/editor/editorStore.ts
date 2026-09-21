// C-03：编辑页状态机（02 §5.1~§5.4）
// 三段布局/面板只通过本 store 读写 EDL，禁止直接改 DOM 或调 api
import type { EDLClip, PreviewAsset, SaveState, Timeline } from '../../types'
import { ApiError } from '../../api'
import { clipDurationMs, EDL_LIMITS } from './format'

export interface EditorState {
  projectId: string | null
  rev: number // 服务端版本号，乐观锁依据
  name: string
  timeline: Timeline // {width,height,fps,sampleRate,audio}
  clips: EDLClip[] // P0：单轨，顺序即时间线顺序
  /** 时间轴缩放（功能2：ALT+滚轮 / 双指；视图层共享，不进撤销栈、不触发保存） */
  scale: number
  selectedClipId: string | null
  previewAsset: PreviewAsset | null // 当前预览素材（含代理映射，04 §4.3）
  inMs: number | null // 预览器待添加片段的入点
  outMs: number | null // 预览器待添加片段的出点
  save: SaveState // 'saved'|'dirty'|'saving'|'conflict'|'error'（types.ts 的 SaveState）
  lastSavedAt: string | null
  rendering: { taskId: string; progress: number; stage: string } | null
}

export type EditorAction =
  | { type: 'load'; payload: { rev: number; name: string; timeline: Timeline; clips: EDLClip[] } }
  | { type: 'setName'; name: string }
  | { type: 'setScale'; scale: number }
  | { type: 'setPreviewAsset'; asset: PreviewAsset | null }
  | { type: 'setIn'; ms: number }
  | { type: 'setOut'; ms: number }
  | { type: 'clearMarks' }
  | { type: 'addClip'; clip: EDLClip; atIndex?: number }
  | { type: 'setClips'; clips: EDLClip[] }
  | { type: 'removeClip'; clipId: string }
  | { type: 'moveClip'; clipId: string; toIndex: number }
  | { type: 'trimClip'; clipId: string; edge: 'in' | 'out'; ms: number }
  | { type: 'select'; clipId: string | null }
  | { type: 'clear' }
  | { type: 'saveStart' }
  | { type: 'saveOk'; rev: number; at: string }
  | { type: 'saveFail'; conflict: boolean }
  | { type: 'setRendering'; payload: EditorState['rendering'] }
  | { type: 'undo' }
  | { type: 'redo' }

/**
 * 订阅切片（02 §5.3 的 5 个切片 + C-03 扩展）
 * 扩展 'preview'/'marks'：预览器装配 video.src 代价高，必须与 'selection'（片段选中）解耦，
 * 否则选中片段会触发预览器重建、播放被重置。
 */
export type EditorSlice =
  | 'clips'
  | 'name'
  | 'save'
  | 'rendering'
  | 'selection'
  | 'preview'
  | 'marks'
  | 'scale'

export interface EditorDerived {
  totalMs: number
  clipCount: number
  dirty: boolean
}

/** 保存实现由页面装配层（index.ts）注入：内部只做 api.updateProject */
export type SaveHandler = (snapshot: Readonly<EditorState>) => Promise<{ rev: number; savedAt: string }>

export interface EditorStore {
  readonly projectId: string
  getState(): Readonly<EditorState>
  dispatch(a: EditorAction): void
  subscribe(fn: (s: Readonly<EditorState>, changed: EditorSlice[]) => void): () => void
  canUndo(): boolean
  canRedo(): boolean
  derive(): EditorDerived
  /** Ctrl+S / 提交渲染前调用：取消防抖定时器并立即保存 */
  flushSave(): Promise<void>
  /** 取消定时器与订阅，页面卸载时调用 */
  destroy(): void
}

interface Snapshot {
  clips: EDLClip[]
  timeline: Timeline
  name: string
}

const UNDO_MAX = 50
const DEBOUNCE_MS = 2000
const RETRY_DELAYS = [1000, 3000, 9000]
/** 功能2：时间轴缩放范围（ALT+滚轮 / 双指；跨设备统一夹具，下限保持片段 ≥MIN_CLIP_PX 可读） */
export const EDITOR_SCALE_MIN = 0.25
export const EDITOR_SCALE_MAX = 8
/** 5.2：只有这 5 个动作入撤销栈（+ C-06 排序批量替换 setClips） */
const UNDOABLE: ReadonlySet<EditorAction['type']> = new Set([
  'addClip',
  'setClips',
  'removeClip',
  'moveClip',
  'trimClip',
  'clear',
])
/** 触发防抖保存的动作（clips/name 变更 + undo/redo，02 §5.3/§5.4） */
const SAVE_TRIGGERING: ReadonlySet<EditorAction['type']> = new Set([
  'setName',
  'addClip',
  'setClips',
  'removeClip',
  'moveClip',
  'trimClip',
  'clear',
  'undo',
  'redo',
])

function initialTimeline(): Timeline {
  return { width: 1920, height: 1080, fps: 30, sampleRate: 48000, audio: true }
}

function initialState(projectId: string): EditorState {
  return {
    projectId,
    rev: 0,
    name: '',
    timeline: initialTimeline(),
    clips: [],
    scale: 1,
    selectedClipId: null,
    previewAsset: null,
    inMs: null,
    outMs: null,
    save: 'saved',
    lastSavedAt: null,
    rendering: null,
  }
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, n))
}

function moveItem<T>(arr: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || from >= arr.length) return arr
  const next = arr.slice()
  const [item] = next.splice(from, 1)
  next.splice(clamp(to, 0, next.length), 0, item)
  return next
}

export function createEditorStore(
  projectId: string,
  opts: { save?: SaveHandler; debounceMs?: number } = {}
): EditorStore {
  let state: EditorState = initialState(projectId)
  const undoStack: Snapshot[] = []
  const redoStack: Snapshot[] = []
  const listeners = new Set<(s: Readonly<EditorState>, changed: EditorSlice[]) => void>()
  let derivedCache: EditorDerived | null = null

  const debounceMs = opts.debounceMs ?? DEBOUNCE_MS
  let saveTimer: number | null = null
  let saveSeq = 0
  let destroyed = false

  function snapshot(s: EditorState): Snapshot {
    return { clips: s.clips, timeline: s.timeline, name: s.name }
  }

  function pushUndo() {
    undoStack.push(snapshot(state))
    if (undoStack.length > UNDO_MAX) undoStack.shift()
    redoStack.length = 0
  }

  function clipIndex(clipId: string): number {
    return state.clips.findIndex((c) => c.clipId === clipId)
  }

  // ===== 保存调度（02 §5.4）=====
  function cancelTimer() {
    if (saveTimer !== null) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
  }

  function scheduleSave() {
    if (!opts.save || destroyed) return
    cancelTimer()
    saveTimer = window.setTimeout(() => {
      saveTimer = null
      void runSave()
    }, debounceMs)
  }

  function delay(ms: number): Promise<void> {
    return new Promise((r) => setTimeout(r, ms))
  }

  async function runSave(): Promise<void> {
    if (!opts.save) return
    const seq = ++saveSeq
    state = { ...state, save: 'saving' }
    notify(['save'])
    for (let attempt = 0; ; attempt++) {
      const snap = snapshot(state)
      try {
        const r = await opts.save({ ...state, ...snap })
        if (seq !== saveSeq) return // 期间又发生了新的保存请求，丢弃本次结果
        state = { ...state, save: 'saved', rev: r.rev, lastSavedAt: r.savedAt }
        notify(['save'])
        return
      } catch (e) {
        if (seq !== saveSeq) return
        const conflict = e instanceof ApiError && e.code === 'E_REV_CONFLICT'
        if (conflict) {
          state = { ...state, save: 'conflict' }
          notify(['save'])
          return
        }
        if (attempt >= RETRY_DELAYS.length) {
          state = { ...state, save: 'error' }
          notify(['save'])
          return
        }
        await delay(RETRY_DELAYS[attempt])
      }
    }
  }

  function notify(changed: EditorSlice[]) {
    for (const fn of listeners) fn(state, changed)
    if (changed.includes('clips')) derivedCache = null
  }

  function reducer(s: EditorState, a: EditorAction): { next: EditorState; changed: EditorSlice[] } {
    switch (a.type) {
      case 'load': {
        const p = a.payload
        return {
          next: {
            ...s,
            rev: p.rev,
            name: p.name,
            timeline: p.timeline,
            clips: p.clips,
            scale: 1,
            selectedClipId: null,
            inMs: null,
            outMs: null,
            save: 'saved',
            rendering: null,
          },
          changed: ['clips', 'name', 'save', 'selection', 'preview', 'marks', 'scale'],
        }
      }
      case 'setScale': {
        const scale = clamp(a.scale, EDITOR_SCALE_MIN, EDITOR_SCALE_MAX)
        if (scale === s.scale) return { next: s, changed: [] }
        return { next: { ...s, scale }, changed: ['scale'] }
      }
      case 'setName': {
        const name = a.name.slice(0, EDL_LIMITS.maxNameLen)
        if (name === s.name) return { next: s, changed: [] }
        return { next: { ...s, name, save: 'dirty' }, changed: ['name', 'save'] }
      }
      case 'setPreviewAsset': {
        if (s.previewAsset === a.asset) return { next: s, changed: [] }
        return {
          next: { ...s, previewAsset: a.asset, inMs: null, outMs: null },
          changed: ['preview', 'marks'],
        }
      }
      case 'setIn': {
        const inMs = Math.max(0, Math.round(a.ms))
        return { next: { ...s, inMs }, changed: ['marks'] }
      }
      case 'setOut': {
        const outMs = Math.max(0, Math.round(a.ms))
        return { next: { ...s, outMs }, changed: ['marks'] }
      }
      case 'clearMarks': {
        if (s.inMs === null && s.outMs === null) return { next: s, changed: [] }
        return { next: { ...s, inMs: null, outMs: null }, changed: ['marks'] }
      }
      case 'addClip': {
        if (s.clips.length >= EDL_LIMITS.maxClips) return { next: s, changed: [] }
        if (clipDurationMs(a.clip.inMs, a.clip.outMs) < EDL_LIMITS.minClipMs) {
          return { next: s, changed: [] }
        }
        if (s.clips.some((c) => c.clipId === a.clip.clipId)) return { next: s, changed: [] }
        const at = a.atIndex === undefined ? s.clips.length : clamp(a.atIndex, 0, s.clips.length)
        const clips = s.clips.slice()
        clips.splice(at, 0, a.clip)
        return {
          next: {
            ...s,
            clips,
            selectedClipId: a.clip.clipId,
            inMs: null,
            outMs: null,
            save: 'dirty',
          },
          changed: ['clips', 'selection', 'marks', 'save'],
        }
      }
      case 'setClips': {
        const clips = a.clips.slice()
        if (clips.length > EDL_LIMITS.maxClips) return { next: s, changed: [] }
        if (clips.some((c) => clipDurationMs(c.inMs, c.outMs) < EDL_LIMITS.minClipMs)) {
          return { next: s, changed: [] }
        }
        const sameOrder =
          clips.length === s.clips.length &&
          clips.every((c, i) => c.clipId === s.clips[i].clipId && c.inMs === s.clips[i].inMs && c.outMs === s.clips[i].outMs)
        if (sameOrder) return { next: s, changed: [] }
        const stillSelected = clips.some((c) => c.clipId === s.selectedClipId)
        return {
          next: { ...s, clips, selectedClipId: stillSelected ? s.selectedClipId : null, save: 'dirty' },
          changed: stillSelected ? ['clips', 'save'] : ['clips', 'selection', 'save'],
        }
      }
      case 'removeClip': {
        const idx = clipIndex(a.clipId)
        if (idx < 0) return { next: s, changed: [] }
        const clips = s.clips.slice()
        clips.splice(idx, 1)
        return {
          next: {
            ...s,
            clips,
            selectedClipId: s.selectedClipId === a.clipId ? null : s.selectedClipId,
            save: 'dirty',
          },
          changed: ['clips', 'selection', 'save'],
        }
      }
      case 'moveClip': {
        const idx = s.clips.findIndex((c) => c.clipId === a.clipId)
        if (idx < 0) return { next: s, changed: [] }
        const clips = moveItem(s.clips, idx, a.toIndex)
        if (clips === s.clips) return { next: s, changed: [] }
        return { next: { ...s, clips, save: 'dirty' }, changed: ['clips', 'save'] }
      }
      case 'trimClip': {
        const idx = s.clips.findIndex((c) => c.clipId === a.clipId)
        if (idx < 0) return { next: s, changed: [] }
        const cur = s.clips[idx]
        const ms = Math.round(a.ms)
        let next: EDLClip
        if (a.edge === 'in') {
          const inMs = clamp(ms, 0, cur.outMs - EDL_LIMITS.minClipMs)
          if (inMs === cur.inMs) return { next: s, changed: [] }
          next = { ...cur, inMs }
        } else {
          const upper = cur.sourceDurationMs && cur.sourceDurationMs > 0 ? cur.sourceDurationMs : Number.MAX_SAFE_INTEGER
          const outMs = clamp(ms, cur.inMs + EDL_LIMITS.minClipMs, upper)
          if (outMs === cur.outMs) return { next: s, changed: [] }
          next = { ...cur, outMs }
        }
        const clips = s.clips.slice()
        clips[idx] = next
        return { next: { ...s, clips, save: 'dirty' }, changed: ['clips', 'save'] }
      }
      case 'select': {
        if (s.selectedClipId === a.clipId) return { next: s, changed: [] }
        return { next: { ...s, selectedClipId: a.clipId }, changed: ['selection'] }
      }
      case 'clear': {
        if (!s.clips.length) return { next: s, changed: [] }
        return {
          next: { ...s, clips: [], selectedClipId: null, inMs: null, outMs: null, save: 'dirty' },
          changed: ['clips', 'selection', 'marks', 'save'],
        }
      }
      case 'saveStart': {
        return { next: { ...s, save: 'saving' }, changed: ['save'] }
      }
      case 'saveOk': {
        return {
          next: { ...s, save: 'saved', rev: a.rev, lastSavedAt: a.at },
          changed: ['save'],
        }
      }
      case 'saveFail': {
        return { next: { ...s, save: a.conflict ? 'conflict' : 'error' }, changed: ['save'] }
      }
      case 'setRendering': {
        return { next: { ...s, rendering: a.payload }, changed: ['rendering'] }
      }
      case 'undo': {
        const prev = undoStack.pop()
        if (!prev) return { next: s, changed: [] }
        redoStack.push(snapshot(s))
        return {
          next: { ...s, clips: prev.clips, timeline: prev.timeline, name: prev.name, save: 'dirty' },
          changed: ['clips', 'name', 'save'],
        }
      }
      case 'redo': {
        const next = redoStack.pop()
        if (!next) return { next: s, changed: [] }
        undoStack.push(snapshot(s))
        return {
          next: { ...s, clips: next.clips, timeline: next.timeline, name: next.name, save: 'dirty' },
          changed: ['clips', 'name', 'save'],
        }
      }
      default:
        return { next: s, changed: [] }
    }
  }

  function dispatch(a: EditorAction) {
    if (destroyed) return
    if (UNDOABLE.has(a.type)) {
      // 仅在动作确实会改变状态时入栈，避免空操作污染撤销栈
      const probe = reducer(state, a)
      if (probe.changed.length === 0) return
      pushUndo()
      state = probe.next
      notify(probe.changed)
    } else {
      const { next, changed } = reducer(state, a)
      state = next
      if (changed.length) notify(changed)
    }
    // 防抖保存：仅 clips/name 两类变更触发（02 §5.4）
    if (SAVE_TRIGGERING.has(a.type) && state.save === 'dirty') scheduleSave()
  }

  function derive(): EditorDerived {
    if (!derivedCache) {
      let totalMs = 0
      for (const c of state.clips) totalMs += clipDurationMs(c.inMs, c.outMs)
      derivedCache = { totalMs, clipCount: state.clips.length, dirty: state.save !== 'saved' }
    }
    return derivedCache
  }

  return {
    projectId,
    getState: () => state,
    dispatch,
    subscribe(fn) {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    canUndo: () => undoStack.length > 0,
    canRedo: () => redoStack.length > 0,
    derive,
    async flushSave() {
      if (!opts.save) return
      cancelTimer()
      await runSave()
    },
    destroy() {
      destroyed = true
      cancelTimer()
      listeners.clear()
    },
  }
}
