// C-09：任务中心右侧抽屉（01 §4.5：顶栏入口 → 右侧抽屉；抽屉内复用 pages/tasks.ts 的任务卡片）
// 设计要点：
// - 列表**惰性挂载**，关闭时只收起不销毁：store 订阅与卡片 DOM 保持，进度不丢（08 C-09 验收）；
// - 打开时主动 store.loadTasks() 拉一次最新（WS 断线期间的兜底）；
// - 复用 taskView.ts 的进度/阶段派生与成品回看，避免任务文案出现第二份实现。
import { el, svgIcon } from '../../ui'
import { store } from '../../store'
import { renderTasks } from '../tasks'

export interface TaskDrawer {
  /** 挂到编辑器根节点（fixed 定位，不占据布局） */
  root: HTMLElement
  toggle(): void
  open(): void
  close(): void
  isOpen(): boolean
  destroy(): void
}

export function buildTaskDrawer(titleText = '任务中心'): TaskDrawer {
  let opened = false
  let mounted = false
  let disposeList: (() => void) | null = null

  const mask = el('div', { class: 'fixed inset-0 z-[55] bg-black/40 hidden' })
  const titleEl = el('div', { class: 'font-semibold text-sm flex-1' }, [titleText])
  const panel = el('div', {
    class:
      'fixed top-0 right-0 bottom-0 z-[56] w-[480px] max-w-[92vw] bg-surface border-l border-line flex flex-col translate-x-full transition-transform duration-200',
    // P2-2：任务中心抽屉补对话框语义
    role: 'dialog',
    'aria-modal': 'true',
  })

  const head = el('div', { class: 'h-11 shrink-0 flex items-center gap-2 px-3 border-b border-line' })
  const closeBtn = el('button', { class: 'btn btn-sm flex items-center', title: '关闭（Esc）' })
  closeBtn.append(svgIcon('x', 14) as unknown as Node)
  const reloadBtn = el('button', { class: 'btn btn-sm flex items-center', title: '刷新任务列表' })
  reloadBtn.append(svgIcon('refresh', 14) as unknown as Node)
  head.append(svgIcon('list', 16) as unknown as Node, titleEl, reloadBtn, closeBtn)

  const body = el('div', { class: 'flex-1 min-h-0 overflow-hidden' })
  panel.append(head, body)
  const root = el('div', {}, [mask, panel])

  let prevActive: HTMLElement | null = null

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && opened) {
      e.stopPropagation()
      close()
      return
    }
    // P2-2：焦点陷阱（Tab 在抽屉内循环）
    if (e.key === 'Tab' && opened) {
      const items = Array.from(
        panel.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
        )
      )
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

  function open() {
    if (!mounted) {
      disposeList = renderTasks(body)
      mounted = true
    }
    opened = true
    prevActive = document.activeElement instanceof HTMLElement ? document.activeElement : null
    mask.classList.remove('hidden')
    panel.classList.remove('translate-x-full')
    void store.loadTasks()
    // P2-2：抽屉标题供读屏器关联
    titleEl.id = 'drawer-' + Math.random().toString(36).slice(2, 9)
    panel.setAttribute('aria-labelledby', titleEl.id)
    closeBtn.focus()
  }

  function close() {
    opened = false
    mask.classList.add('hidden')
    panel.classList.add('translate-x-full')
    if (prevActive && prevActive.isConnected) prevActive.focus()
    prevActive = null
  }

  closeBtn.onclick = () => close()
  reloadBtn.onclick = () => void store.loadTasks()
  mask.onclick = () => close()
  window.addEventListener('keydown', onKey, true)

  return {
    root,
    toggle() {
      opened ? close() : open()
    },
    open,
    close,
    isOpen() {
      return opened
    },
    destroy() {
      window.removeEventListener('keydown', onKey, true)
      disposeList?.()
      disposeList = null
      root.remove()
    },
  }
}
