// 轻量 UI 工具：toast / confirm / element 构造 / 格式化 / SVG 图标

// ===== SVG 图标 =====
export type IconName =
  | 'list' | 'video' | 'server' | 'settings' | 'history' | 'film'
  | 'folder' | 'folder-open' | 'file-video' | 'search' | 'refresh'
  | 'plus' | 'x' | 'back' | 'inbox' | 'play' | 'pause' | 'stop'
  | 'check' | 'warning' | 'edit' | 'trash' | 'save' | 'circle'
  | 'upload' | 'download' | 'clock' | 'link' | 'grip' | 'scissors'
  | 'grid' | 'arrow-right' | 'volume' | 'mute' | 'maximize' | 'minimize'
  | 'rewind' | 'fast-forward'

const ICON_PATHS: Record<IconName, string> = {
  list: '<line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/>',
  video: '<polygon points="23 7 16 12 23 17 23 7"/><rect x="1" y="5" width="15" height="14" rx="2" ry="2"/>',
  server: '<rect x="2" y="2" width="20" height="8" rx="2" ry="2"/><rect x="2" y="14" width="20" height="8" rx="2" ry="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
  history: '<path d="M3 3v5h5"/><path d="M3.05 13A9 9 0 1 0 6 5.3L3 8"/><path d="M12 7v5l4 2"/>',
  film: '<rect x="2" y="2" width="20" height="20" rx="2.5"/><line x1="7" y1="2" x2="7" y2="22"/><line x1="17" y1="2" x2="17" y2="22"/><line x1="2" y1="12" x2="22" y2="12"/><line x1="2" y1="7" x2="7" y2="7"/><line x1="2" y1="17" x2="7" y2="17"/><line x1="17" y1="7" x2="22" y2="7"/><line x1="17" y1="17" x2="22" y2="17"/>',
  folder: '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>',
  'folder-open': '<path d="M6 14l1.45-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.55 6a2 2 0 0 1-1.94 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.93a2 2 0 0 1 1.66.9l.82 1.2a2 2 0 0 0 1.66.9H18a2 2 0 0 1 2 2v2"/>',
  'file-video': '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="10" y1="12" x2="10" y2="18"/><line x1="14" y1="14" x2="14" y2="16"/><line x1="9" y1="15" x2="15" y2="15"/>',
  search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>',
  refresh: '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M3 21v-5h5"/>',
  plus: '<line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>',
  x: '<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>',
  back: '<line x1="19" y1="12" x2="5" y2="12"/><polyline points="12 19 5 12 12 5"/>',
  inbox: '<polyline points="22 12 16 12 14 15 10 15 8 12 2 12"/><path d="M5.45 5.11L2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>',
  play: '<polygon points="5 3 19 12 5 21 5 3"/>',
  pause: '<rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/>',
  stop: '<rect x="5" y="5" width="14" height="14" rx="2"/>',
  check: '<polyline points="20 6 9 17 4 12"/>',
  warning: '<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>',
  edit: '<path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>',
  trash: '<polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>',
  save: '<path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/>',
  circle: '<circle cx="12" cy="12" r="10"/>',
  upload: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/>',
  download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>',
  clock: '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>',
  link: '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>',
  grip: '<circle cx="9" cy="5" r="1.5"/><circle cx="15" cy="5" r="1.5"/><circle cx="9" cy="12" r="1.5"/><circle cx="15" cy="12" r="1.5"/><circle cx="9" cy="19" r="1.5"/><circle cx="15" cy="19" r="1.5"/>',
  scissors: '<circle cx="6" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><line x1="20" y1="4" x2="8.12" y2="15.88"/><line x1="14.47" y1="14.48" x2="20" y2="20"/><line x1="8.12" y1="8.12" x2="12" y2="12"/>',
  grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  'arrow-right': '<line x1="5" y1="12" x2="19" y2="12"/><polyline points="12 5 19 12 12 19"/>',
  volume: '<polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/>',
  mute: '<polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/>',
  maximize: '<path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"/>',
  minimize: '<path d="M8 3v3a2 2 0 0 1-2 2H3m18 0h-3a2 2 0 0 1-2-2V3m0 18v-3a2 2 0 0 1 2-2h3M3 16h3a2 2 0 0 1 2 2v3"/>',
  rewind: '<polygon points="11 19 2 12 11 5 11 19"/><polygon points="22 19 13 12 22 5 22 19"/>',
  'fast-forward': '<polygon points="13 19 22 12 13 5 13 19"/><polygon points="2 19 11 12 2 5 2 19"/>',
}

export function svgIcon(name: IconName, size = 16): SVGSVGElement {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  svg.setAttribute('width', String(size))
  svg.setAttribute('height', String(size))
  svg.setAttribute('viewBox', '0 0 24 24')
  svg.setAttribute('fill', 'none')
  svg.setAttribute('stroke', 'currentColor')
  svg.setAttribute('stroke-width', '2')
  svg.setAttribute('stroke-linecap', 'round')
  svg.setAttribute('stroke-linejoin', 'round')
  svg.innerHTML = ICON_PATHS[name]
  return svg
}

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string | boolean> = {},
  children: (Node | string)[] = []
): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag)
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') e.className = String(v)
    else if (k === 'html') e.innerHTML = String(v)
    else if (typeof v === 'boolean') {
      ;(e as Record<string, boolean>)[k] = v
    } else {
      e.setAttribute(k, v)
    }
  }
  for (const c of children) e.appendChild(typeof c === 'string' ? document.createTextNode(c) : c)
  return e
}

// ===== 骨架屏（P2-1：列表/表单加载态，替代纯文本"加载中..."与 spinner）=====
export function skeleton(w = '100%', h = '0.75rem', className = ''): HTMLElement {
  return el('div', { class: `skeleton rounded ${className}`, style: `width:${w};height:${h}` })
}

/** 表单/列表骨架行：label 短条 + 控件条，共 n 组 */
export function skeletonRows(n: number, h = '0.75rem'): HTMLElement[] {
  const rows: HTMLElement[] = []
  for (let i = 0; i < n; i++) {
    rows.push(el('div', { class: 'space-y-2' }, [skeleton('35%', '0.625rem'), skeleton('100%', h)]))
  }
  return rows
}

/** 仅对读屏器可见的文本（颜色/图标之外的状态等价物） */
export function srOnly(text: string): HTMLElement {
  return el('span', { class: 'sr-only' }, [text])
}

export function formatSize(bytes: number): string {
  if (!bytes || bytes < 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

// P1-3：统一 HH:MM:SS 时间码（h 不补零，m/s 补零；如 0:12:34 / 1:05:09）
export function formatDuration(sec: number): string {
  if (!sec || sec < 0) return '-'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = Math.floor(sec % 60)
  return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
}

export function formatTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-CN', { hour12: false })
}

// ===== Toast =====
let toastTimer: number | null = null
export function toast(msg: string, type: 'info' | 'error' | 'success' = 'info') {
  let box = document.getElementById('toast-box')
  if (!box) {
    box = el('div', { id: 'toast-box', class: 'fixed top-4 right-4 z-50 flex flex-col gap-2' })
    document.body.appendChild(box)
  }
  const color =
    type === 'error'
      ? 'bg-danger text-white'
      : type === 'success'
      ? 'bg-success text-white'
      : 'bg-ink text-surface'
  const t = el(
    'div',
    { class: `${color} text-sm px-4 py-2 rounded shadow-lg transition-opacity` },
    [msg]
  )
  box.appendChild(t)
  setTimeout(() => {
    t.classList.add('opacity-0')
    setTimeout(() => t.remove(), 300)
  }, 2500)
  void toastTimer
}

// ===== 自建 overlay 无障碍（P2-2：role=dialog / aria-modal / Esc / 焦点陷阱 / 返回焦点）=====
export interface DialogAccessibilityOptions {
  overlay: HTMLElement
  titleEl?: HTMLElement | null
  /** Esc / 触发关闭时调用；不传则直接 overlay.remove() */
  onClose?: () => void
  /** 初始焦点元素；不传则聚焦第一个可聚焦元素 */
  initialFocus?: HTMLElement | null
}

/**
 * 为自建浮层补全对话框语义：
 * - role="dialog" + aria-modal="true" + aria-labelledby（指向 titleEl）
 * - Esc 关闭（透传给 onClose）
 * - Tab 焦点陷阱（首尾循环）
 * - 关闭后恢复触发前的焦点
 * 返回 cleanup（移除监听 + 恢复焦点），组件自行 close 时应调用。
 */
export function setupDialogAccessibility(opts: DialogAccessibilityOptions): () => void {
  const { overlay, titleEl, onClose, initialFocus } = opts
  const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null

  overlay.setAttribute('role', 'dialog')
  overlay.setAttribute('aria-modal', 'true')
  if (titleEl) {
    const id = 'dlg-' + Math.random().toString(36).slice(2, 9)
    titleEl.id = id
    overlay.setAttribute('aria-labelledby', id)
  }
  overlay.tabIndex = -1

  const focusables = () =>
    Array.from(
      overlay.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
      )
    )

  const onKeydown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      close()
      return
    }
    if (e.key === 'Tab') {
      const items = focusables()
      if (items.length === 0) return
      const first = items[0]
      const last = items[items.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
  }
  overlay.addEventListener('keydown', onKeydown)

  function close() {
    overlay.removeEventListener('keydown', onKeydown)
    if (previouslyFocused && previouslyFocused.isConnected) previouslyFocused.focus()
    if (onClose) onClose()
    else overlay.remove()
  }

  // 打开后聚焦（微任务等待 overlay 挂载完成）
  queueMicrotask(() => {
    if (initialFocus) initialFocus.focus()
    else focusables()[0]?.focus()
  })

  return close
}

// ===== Confirm 弹窗（Promise）=====
export function confirmDialog(message: string, title = '确认操作', danger = false): Promise<boolean> {
  return new Promise((resolve) => {
    const overlay = el('div', {
      class: 'fixed inset-0 bg-black/40 z-50 flex items-center justify-center',
    })
    const modal = el('div', { class: 'card w-80 p-5 shadow-xl' })
    // P1-2：危险态不再用 2px 彩色描边，改由标题红字 + 危险按钮表达风险级别
    const titleColor = danger ? 'rgb(var(--c-danger-hover))' : ''
    const titleEl = el('h3', { class: 'font-semibold text-base mb-2' }, [title])
    if (titleColor) titleEl.style.color = titleColor
    const msgEl = el('p', { class: 'text-sm text-ink-muted mb-4' }, [message])
    const btns = el('div', { class: 'flex justify-end gap-2' })
    const cancel = el('button', { class: 'btn' }, ['取消'])
    const ok = el('button', { class: `btn ${danger ? 'btn-danger' : 'btn-primary'}` }, ['确定'])
    let settled = false
    const finish = (result: boolean) => {
      if (settled) return
      settled = true
      cleanup()
      overlay.remove()
      resolve(result)
    }
    cancel.onclick = () => finish(false)
    ok.onclick = () => finish(true)
    btns.append(cancel, ok)
    modal.append(titleEl, msgEl, btns)
    overlay.appendChild(modal)
    overlay.onclick = (e) => {
      if (e.target === overlay) finish(false)
    }
    document.body.appendChild(overlay)
    const cleanup = setupDialogAccessibility({ overlay, titleEl, onClose: () => finish(false) })
    ok.focus()
  })
}

// ===== 批量操作逐项结果（P2-1：成功 N / 失败 M 及原因）=====
export interface BatchFailure {
  name: string
  reason: string
}

export function showBatchResult(opLabel: string, okCount: number, failures: BatchFailure[]) {
  if (failures.length === 0) {
    toast(`${opLabel}成功 ${okCount} 个`, okCount > 0 ? 'success' : 'info')
    return
  }
  const overlay = el('div', { class: 'fixed inset-0 bg-black/40 z-50 flex items-center justify-center p-3' })
  const modal = el('div', { class: 'card w-full max-w-md p-5 shadow-xl' })
  const titleEl = el('h3', { class: 'font-semibold text-base mb-2' }, ['批量操作结果'])
  const summary = el('p', { class: 'text-sm mb-3' }, [
    `${opLabel}成功 ${okCount} 个，失败 ${failures.length} 个`,
  ])
  const list = el('div', {
    class: 'max-h-64 overflow-auto border border-line rounded p-2 space-y-1 bg-surface-alt',
  })
  for (const f of failures) {
    list.appendChild(
      el('div', { class: 'text-xs flex gap-2' }, [
        el('span', { class: 'font-medium shrink-0 max-w-[40%] truncate' }, [f.name]),
        el('span', { class: 'text-danger break-all' }, [f.reason]),
      ])
    )
  }
  const ok = el('button', { class: 'btn btn-primary' }, ['知道了'])
  const close = () => overlay.remove()
  ok.onclick = close
  modal.append(titleEl, summary, list, el('div', { class: 'flex justify-end gap-2 mt-4' }, [ok]))
  overlay.appendChild(modal)
  overlay.onclick = (e) => {
    if (e.target === overlay) close()
  }
  document.body.appendChild(overlay)
  setupDialogAccessibility({ overlay, titleEl, onClose: close })
  ok.focus()
}

// 通用表格空状态（P2-1：空态承载"下一步动作"）
export function emptyState(
  text: string,
  icon: IconName = 'inbox',
  action?: { label: string; onClick: () => void }
): HTMLElement {
  const box = el(
    'div',
    { class: 'flex flex-col items-center justify-center py-16 text-ink-muted' },
    [el('div', { class: 'mb-2' }, [svgIcon(icon, 40) as unknown as Node]), el('p', {}, [text])]
  )
  if (action) {
    const btn = el('button', { class: 'btn mt-3', type: 'button' }, [action.label])
    btn.onclick = action.onClick
    box.appendChild(btn)
  }
  return box
}
