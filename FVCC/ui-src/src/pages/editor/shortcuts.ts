// C-10：快捷键注册表（01 §6 全表）
// Space 播放/暂停（输入框聚焦除外） · I/O 打点 · ←/→ ∓1 帧 · Shift+←/→ ∓1s
// ,/. 入点 ∓1 帧 / 出点 ±1 帧 · Backspace/Delete 删除选中片段 · Ctrl+S 保存
// Ctrl+Enter 渲染导出 · Ctrl+Z / Ctrl+Shift+Z 撤销/重做 · Esc 关闭抽屉/取消拖拽
import { frameDeltaMs } from './format'
import type { EditorStore } from './editorStore'
import type { PreviewController } from './preview'

export interface ShortcutOptions {
  edlStore: EditorStore
  preview: PreviewController
  /** Ctrl+Enter：渲染导出（先 flushSave，见 index.handleRender） */
  onRender(): void
  /** Esc：关闭任务抽屉 / 取消当前拖拽 */
  onEscape?(): void
}

/** 输入类元素聚焦时，非 Ctrl 组合键交还给输入框（01 §6：Space 等除外） */
function isEditableTarget(target: EventTarget | null): boolean {
  const node = target as HTMLElement | null
  if (!node || typeof node.tagName !== 'string') return false
  const tag = node.tagName.toUpperCase()
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || node.isContentEditable === true
}

export function bindEditorShortcuts(opts: ShortcutOptions): () => void {
  const { edlStore, preview } = opts
  /** 预览器当前时间（源素材时间轴）：由 onTime 回调维护，供逐帧步进使用 */
  let lastMs = 0
  let lastFile: string | null = null

  const unsub = preview.onTime((ms, file) => {
    lastMs = ms
    lastFile = file
  })

  function fps(): number {
    return edlStore.getState().timeline.fps || 30
  }

  function seekBy(deltaMs: number): void {
    if (!lastFile) return
    preview.seekMs(Math.max(0, lastMs + deltaMs))
  }

  /** 选中片段的入点/出点逐帧微调（, / .） */
  function nudgeMark(edge: 'in' | 'out', dir: 1 | -1): void {
    const s = edlStore.getState()
    const clip = s.clips.find((c) => c.clipId === s.selectedClipId)
    if (!clip) return
    const cur = edge === 'in' ? clip.inMs : clip.outMs
    edlStore.dispatch({
      type: 'trimClip',
      clipId: clip.clipId,
      edge,
      ms: cur + dir * frameDeltaMs(1, fps()),
    })
  }

  function onKey(e: KeyboardEvent): void {
    const editable = isEditableTarget(e.target)
    const mod = e.ctrlKey || e.metaKey

    if (e.key === 'Escape') {
      opts.onEscape?.()
      return
    }

    if (mod) {
      const k = e.key.toLowerCase()
      if (k === 's') {
        e.preventDefault()
        void edlStore.flushSave()
      } else if (k === 'enter') {
        e.preventDefault()
        opts.onRender()
      } else if (k === 'z') {
        e.preventDefault()
        edlStore.dispatch({ type: e.shiftKey ? 'redo' : 'undo' })
      }
      return
    }

    if (editable) return

    switch (e.key) {
      case ' ':
        e.preventDefault()
        preview.toggle()
        return
      case 'i':
      case 'I':
        preview.markIn()
        return
      case 'o':
      case 'O':
        preview.markOut()
        return
      case 'ArrowLeft':
        e.preventDefault()
        seekBy(e.shiftKey ? -1000 : -frameDeltaMs(1, fps()))
        return
      case 'ArrowRight':
        e.preventDefault()
        seekBy(e.shiftKey ? 1000 : frameDeltaMs(1, fps()))
        return
      case ',':
        nudgeMark('in', -1)
        return
      case '.':
        nudgeMark('out', 1)
        return
      case 'Backspace':
      case 'Delete': {
        const id = edlStore.getState().selectedClipId
        if (!id) return
        e.preventDefault()
        edlStore.dispatch({ type: 'removeClip', clipId: id })
        return
      }
      default:
        return
    }
  }

  window.addEventListener('keydown', onKey)
  return () => {
    window.removeEventListener('keydown', onKey)
    unsub()
  }
}
