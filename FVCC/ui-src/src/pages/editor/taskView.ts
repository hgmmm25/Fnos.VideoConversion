// C-08/C-09 共用：渲染/代理任务的展示派生（06 §2.2 stage、§6 WSEvent）
// 任务页（pages/tasks.ts）、编辑器任务抽屉（pages/editor/taskDrawer.ts）、渲染提交回执共用同一套派生，
// 避免"阶段中文名/进度文案"在多处各写一份。
import { toast } from '../../ui'
import { api } from '../../api'
import type { Task } from '../../types'

/** 渲染阶段 / 代理阶段中文名（06 §2.2 tasks.stage + 03 §6 WSEvent 的 'proxy'） */
export const STAGE_LABEL: Record<string, string> = {
  prepare: '准备',
  segment: '分段渲染',
  concat: '拼接',
  mux: '封装',
  finalize: '收尾',
  proxy: '生成代理',
}

export function isRenderTask(t: Pick<Task, 'taskType'>): boolean {
  return t.taskType === 'RENDER_EDL'
}

export function isProxyTask(t: Pick<Task, 'taskType'>): boolean {
  return t.taskType === 'GEN_PROXY'
}

/** 阶段文本：'分段渲染 2/4'；无阶段时返回空串 */
export function stageText(t: Task): string {
  const stage = STAGE_LABEL[String(t.stage || '')] || String(t.stage || '')
  if (!stage) return ''
  if (t.segTotal && t.segTotal > 1) {
    return `${stage} ${Math.max(1, Number(t.segIndex) || 1)}/${t.segTotal}`
  }
  return stage
}

/** 卡片/抽屉的进度文案：渲染任务优先展示阶段与段序号，其余沿用百分比 */
export function progressLabel(t: Task, pct: number, fallback = ''): string {
  const base = `${pct.toFixed(0)}%`
  if (isRenderTask(t) || isProxyTask(t)) {
    const stage = stageText(t)
    return stage ? `${stage} · ${base}` : base
  }
  return fallback ? `${fallback} ${base}` : base
}

function pad2(n: number): string {
  return n < 10 ? '0' + n : String(n)
}

/** 已用时/耗时：运行中按 now-createdAt 计，終态按 updatedAt-createdAt 计 */
export function elapsedText(t: Task): string {
  const created = Date.parse(t.createdAt)
  if (!Number.isFinite(created)) return ''
  const end = isTerminalStatus(t.status) ? Date.parse(t.updatedAt) : Date.now()
  const ms = Math.max(0, (Number.isFinite(end) ? end : Date.now()) - created)
  const totalSec = Math.floor(ms / 1000)
  const h = Math.floor(totalSec / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const s = totalSec % 60
  const body = h > 0 ? `${h}:${pad2(m)}:${pad2(s)}` : `${pad2(m)}:${pad2(s)}`
  return (isTerminalStatus(t.status) ? '耗时 ' : '已用时 ') + body
}

/** 渲染/代理任务是否仍在推进（用于抽屉 1s 刷新计时） */
export function isRunning(t: Task): boolean {
  return !isTerminalStatus(t.status) && t.status !== 'PAUSED' && t.status !== 'ERROR'
}

/** 任务可在线预览的产物路径（渲染任务为 outputFile，代理任务为代理文件） */
export function outputPreviewPath(t: Task): string {
  if (isProxyTask(t)) return t.outputName || t.outputFile
  return t.outputFile || t.outputName
}

/** 新窗口预览产物（dest 根，走 /stream 票据，04 §2.3） */
export async function previewOutput(t: Task): Promise<void> {
  const path = outputPreviewPath(t)
  if (!path) {
    toast('任务未产出可预览文件', 'error')
    return
  }
  const root = isProxyTask(t) ? 'proxy' : 'dest'
  try {
    const tk = await api.createStreamTicket({ path, root })
    window.open(api.streamUrl(path, tk.ticket, root), '_blank', 'noopener')
  } catch (e) {
    toast('预览失败: ' + (e as Error).message, 'error')
  }
}

/** 复制产物路径（NAS 绝对路径，便于用户到文件管理器/下载页取件）
 *  需求7：原实现仅依赖 navigator.clipboard，非安全上下文（http + 非 localhost）下
 *  必然失败并 toast「复制失败，请手动选择」；现增加 execCommand 兜底，最大限度减少失败。 */
export async function copyOutputPath(t: Task): Promise<void> {
  const path = outputPreviewPath(t)
  if (!path) {
    toast('任务未产出文件', 'error')
    return
  }
  if (await writeClipboard(path)) {
    toast('已复制路径', 'success')
    return
  }
  if (fallbackCopyText(path)) {
    toast('已复制路径', 'success')
    return
  }
  toast('复制失败，请手动选择：' + path, 'error')
}

async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* 权限被拒 / 非安全上下文，走兜底 */
  }
  return false
}

/** execCommand 兜底：非安全上下文下同样可用（需用户手势触发，任务条按钮恰好满足） */
function fallbackCopyText(text: string): boolean {
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '-9999px'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    ta.setSelectionRange(0, ta.value.length)
    const ok = document.execCommand('copy')
    ta.remove()
    return ok
  } catch {
    return false
  }
}

function isTerminalStatus(s: Task['status']): boolean {
  return s === 'COMPLETED' || s === 'CANCELLED'
}
