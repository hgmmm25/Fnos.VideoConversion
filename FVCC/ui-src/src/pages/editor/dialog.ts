// C-08：渲染弹窗（01 §4.4 顶栏 / 工具条「渲染导出」；08 C-08 验收：方案下拉 + 输出名 + 节点选择 + 提交）
// 关键契约：
// - 提交前强制 flushSave（02 §5.4）：保存未完成则阻止提交；
// - 方案下拉 = 服务端 presetKey 枚举（05 §5.1，见 preset.ts 的决策说明）；
// - 请求体 { presetKey, outputName, serverId?, force? }，响应 { taskId, status, serverId, output, totalMs, fastCopyAllowed }（03 §4.4）；
// - 服务端幂等（checksum 命中进行中/成品仍在 → 复用同一 taskId）；force=true 跳过复用。
import { el, svgIcon, toast, setupDialogAccessibility } from '../../ui'
import { api, ApiError } from '../../api'
import { store } from '../../store'
import type { RenderRequest, RenderSubmitResult } from '../../types'
import { DEFAULT_PRESET_KEY, RENDER_PRESETS, fallbackHint, findPreset } from './preset'

export interface RenderDialogOptions {
  projectId: string
  projectName: string
  /** 提交前置钩子：返回 false 表示保存未就绪，阻止提交（02 §5.4） */
  beforeSubmit(): Promise<boolean>
  /** 提交成功：打开任务中心抽屉并让用户看到新任务（01 §4.5） */
  onSubmitted(r: RenderSubmitResult): void
  /** 关闭后回调（取消/提交/按 Esc 均触发），调用方用于把单例句柄置空 */
  onClosed?(): void
}

export interface RenderDialogHandle {
  close(): void
}

/** 输出名非法字符（服务端 normalizeOutputBase 同样拒绝，此处提前提示） */
const ILLEGAL_NAME = /[\\/:*?"<>|]/

function errText(e: unknown): string {
  if (e instanceof ApiError) return e.code && e.code !== 'E_UNKNOWN' ? `${e.message}（${e.code}）` : e.message
  return e instanceof Error ? e.message : String(e)
}

function defaultOutputName(projectName: string): string {
  const d = new Date()
  const stamp = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
  const base = (projectName || 'export').replace(ILLEGAL_NAME, '_').trim().slice(0, 48)
  return `${base || 'export'}_${stamp}`
}

function field(label: string, control: HTMLElement, hint?: HTMLElement): HTMLElement {
  const box = el('div', { class: 'flex flex-col gap-1' })
  box.append(el('div', { class: 'text-xs text-ink-muted' }, [label]), control)
  if (hint) box.append(hint)
  return box
}

export function openRenderDialog(opts: RenderDialogOptions): RenderDialogHandle {
  let busy = false
  let closed = false

  const overlay = el('div', {
    class: 'fixed inset-0 z-[60] flex items-center justify-center bg-black/50 p-4',
    // P2-2：渲染弹窗补对话框语义（Esc / 焦点陷阱 / 返回焦点由下方 setupDialogAccessibility 兜底）
    role: 'dialog',
    'aria-modal': 'true',
  })
  const card = el('div', { class: 'card w-full max-w-lg p-4 flex flex-col gap-3' })

  const head = el('div', { class: 'flex items-center gap-2' })
  const closeBtn = el('button', { class: 'btn btn-sm flex items-center', title: '关闭（Esc）' })
  closeBtn.append(svgIcon('x', 14) as unknown as Node)
  const titleEl = el('div', { class: 'font-semibold text-sm flex-1' }, ['渲染导出'])
  head.append(svgIcon('film', 16) as unknown as Node, titleEl, closeBtn)

  // ===== 渲染方案（presetKey，05 §5.1）=====
  const presetSel = el('select', { class: 'input w-full' }) as HTMLSelectElement
  for (const p of RENDER_PRESETS) {
    presetSel.append(el('option', { value: p.key, title: p.args }, [p.label]))
  }
  presetSel.value = DEFAULT_PRESET_KEY
  const presetHint = el('div', { class: 'text-xs text-ink-muted' }, [fallbackHint(presetSel.value)])
  presetSel.onchange = () => {
    presetHint.textContent = fallbackHint(presetSel.value)
  }

  // ===== 输出名 =====
  const nameInput = el('input', {
    class: 'input w-full',
    maxlength: '64',
    placeholder: '输出文件名（不含扩展名）',
  }) as HTMLInputElement
  nameInput.value = defaultOutputName(opts.projectName)
  const nameHint = el('div', { class: 'text-xs text-ink-muted' }, [
    '服务端补 .mp4；重名自动追加 _1/_2（03 §4.4）',
  ])

  // ===== 渲染节点（06 §5.3：留空 = 由调度器选机）=====
  const nodeSel = el('select', { class: 'input w-full' }) as HTMLSelectElement
  nodeSel.append(el('option', { value: '' }, ['自动选择（按调度策略）']))
  for (const s of store.servers || []) {
    nodeSel.append(
      el('option', { value: s.id }, [`${s.name || s.id} · ${s.ip}:${s.port} · ${s.status || '未知'}`])
    )
  }
  const nodeHint = el('div', { class: 'text-xs text-ink-muted' }, [
    '指定节点离线时服务端返回 E_NODE_OFFLINE（409）',
  ])

  // ===== force（跳过幂等复用）=====
  const forceCb = el('input', { type: 'checkbox', class: 'rounded' }) as HTMLInputElement
  const forceRow = el('label', { class: 'flex items-center gap-2 text-xs text-ink-muted cursor-pointer' }, [
    forceCb,
    el('span', {}, ['忽略幂等复用，强制新建任务（force）']),
  ])

  card.append(
    head,
    field('渲染方案', presetSel, presetHint),
    field('输出名', nameInput, nameHint),
    field('渲染节点', nodeSel, nodeHint),
    forceRow
  )

  // ===== 底部操作 =====
  const foot = el('div', { class: 'flex items-center justify-end gap-2 pt-1' })
  const cancelBtn = el('button', { class: 'btn btn-sm' }, ['取消'])
  const submitBtn = el('button', { class: 'btn btn-sm btn-primary' }, ['提交渲染'])
  foot.append(cancelBtn, submitBtn)
  card.append(foot)
  overlay.append(card)

  const prevActive = document.activeElement as HTMLElement | null

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      close()
      return
    }
    const t = e.target as HTMLElement | null
    const tag = t && typeof t.tagName === 'string' ? t.tagName.toUpperCase() : ''
    const typing = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || (!!t && t.isContentEditable === true)
    if (!typing) {
      // 模态期间屏蔽全局剪辑快捷键（Space/I/O/Delete…，C-10）
      e.stopPropagation()
    }
  }

  function close() {
    if (closed) return
    closed = true
    window.removeEventListener('keydown', onKey, true)
    overlay.remove()
    if (prevActive && document.contains(prevActive)) prevActive.focus()
    opts.onClosed?.()
  }

  async function submit() {
    if (busy || closed) return
    const name = nameInput.value.trim()
    if (!name) {
      toast('请填写输出名', 'error')
      return
    }
    if (ILLEGAL_NAME.test(name)) {
      toast('输出名含非法字符（\\ / : * ? " < > |），请修改', 'error')
      return
    }
    busy = true
    submitBtn.setAttribute('disabled', 'true')
    submitBtn.textContent = '提交中…'
    try {
      const ready = await opts.beforeSubmit()
      if (!ready) {
        toast('保存未完成，暂不能提交渲染（02 §5.4）', 'error')
        return
      }
      const body: RenderRequest = { presetKey: presetSel.value, outputName: name }
      if (nodeSel.value) body.serverId = nodeSel.value
      if (forceCb.checked) body.force = true

      const r = await api.renderProject(opts.projectId, body)
      const preset = findPreset(presetSel.value)
      const reused = (store.tasks || []).some((t) => t.id === r.taskId)
      if (reused) {
        toast(`已复用已有任务 ${r.taskId}（幂等键命中，03 §4.4）`, 'info')
      } else if (preset && preset.encoder === 'copy' && !r.fastCopyAllowed) {
        toast('同源条件不满足，服务端已降级为转码（05 §5.2）', 'info')
      } else {
        toast(`渲染任务已提交：${r.taskId}`, 'success')
      }
      opts.onSubmitted(r)
      void store.loadTasks()
      close()
    } catch (e) {
      toast('提交失败：' + errText(e), 'error')
    } finally {
      busy = false
      submitBtn.removeAttribute('disabled')
      submitBtn.textContent = '提交渲染'
    }
  }

  closeBtn.onclick = () => close()
  cancelBtn.onclick = () => close()
  submitBtn.onclick = () => void submit()
  overlay.addEventListener('mousedown', (e) => {
    if (e.target === overlay) close()
  })
  window.addEventListener('keydown', onKey, true)

  document.body.append(overlay)
  titleEl.id = 'render-dialog-title'
  overlay.setAttribute('aria-labelledby', titleEl.id)
  // P2-2：焦点陷阱 + Esc 兜底 + 返回焦点（与上方 onKey 幂等共存）
  setupDialogAccessibility({ overlay, titleEl, onClose: close, initialFocus: nameInput })
  nameInput.focus()

  return { close }
}
