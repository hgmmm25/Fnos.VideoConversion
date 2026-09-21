// P1-4：全局命令面板（Ctrl/Cmd+K / 顶栏命令框触发）
// 能力：跳转页面 + 项目搜索（按项目名过滤；素材按文件名搜索成本较高，留待 P2）
// 依赖：ui.ts 的 el/svgIcon、api.listProjects()、hash 路由（#/editor/:projectId）
import { el, svgIcon, setupDialogAccessibility, type IconName } from './ui'
import { api } from './api'
import type { ProjectSummary } from './types'

export interface CmdPage {
  id: string
  label: string
  icon: IconName
}

export interface CommandPaletteOptions {
  pages: CmdPage[]
  /** 页面跳转回调（main.ts 注入 navigate） */
  onNavigate: (id: string) => void
}

interface CmdEntry {
  kind: 'page' | 'project'
  id: string
  label: string
  sub: string
  icon: IconName
}

const ROW_BASE =
  'w-full text-left px-4 py-2.5 flex items-center gap-3 text-sm transition-colors min-h-11 '

export function initCommandPalette(opts: CommandPaletteOptions): {
  open: () => void
  destroy: () => void
} {
  let overlay: HTMLElement | null = null
  let inputEl: HTMLInputElement | null = null
  let listEl: HTMLElement | null = null
  let projects: ProjectSummary[] = []
  let projectsLoaded = false
  let entries: CmdEntry[] = []
  let sel = 0

  function loadProjects() {
    if (projectsLoaded) return
    projectsLoaded = true
    api
      .listProjects()
      .then((r) => {
        projects = r.projects ?? r.items ?? []
        if (overlay) render()
      })
      .catch(() => {
        /* 项目列表加载失败不影响页面跳转 */
      })
  }

  function run(e: CmdEntry) {
    close()
    if (e.kind === 'page') opts.onNavigate(e.id)
    else location.hash = '#/editor/' + encodeURIComponent(e.id)
  }

  function highlight() {
    const list = listEl
    if (!list) return
    list.querySelectorAll<HTMLButtonElement>('button[data-idx]').forEach((row, i) => {
      row.className = ROW_BASE + (i === sel ? 'bg-surface-hover' : 'hover:bg-surface-hover')
    })
  }

  function render() {
    const list = listEl
    const input = inputEl
    if (!overlay || !list || !input) return
    const q = input.value.trim().toLowerCase()
    const pageEntries: CmdEntry[] = opts.pages
      .filter((p) => !q || p.label.toLowerCase().includes(q) || p.id.includes(q))
      .map((p) => ({ kind: 'page', id: p.id, label: p.label, sub: '页面', icon: p.icon }))
    const matched = q
      ? projects.filter((p) => p.name.toLowerCase().includes(q))
      : projects.slice(0, 6)
    const projEntries: CmdEntry[] = matched.map((p) => ({
      kind: 'project',
      id: p.id,
      label: p.name,
      sub: `项目 · ${p.clipCount ?? 0} 素材`,
      icon: 'film',
    }))
    entries = [...pageEntries, ...projEntries]
    sel = Math.min(sel, Math.max(entries.length - 1, 0))
    list.innerHTML = ''
    if (!entries.length) {
      list.append(
        el('div', { class: 'px-4 py-6 text-center text-sm text-ink-muted' }, ['无匹配结果'])
      )
      return
    }
    entries.forEach((e, i) => {
      const row = el('button', {
        class: ROW_BASE + (i === sel ? 'bg-surface-hover' : 'hover:bg-surface-hover'),
        'data-idx': String(i),
      })
      row.append(svgIcon(e.icon, 16) as unknown as Node)
      const label = el('span', { class: 'truncate' }, [e.label])
      const sub = el('span', { class: 'ml-2 text-xs text-ink-muted shrink-0' }, [e.sub])
      const body = el('div', { class: 'flex-1 min-w-0 flex items-center' })
      body.append(label, sub)
      row.append(body)
      row.onclick = () => run(e)
      row.onmouseenter = () => {
        if (sel !== i) {
          sel = i
          highlight()
        }
      }
      list.appendChild(row)
    })
  }

  function close() {
    overlay?.remove()
    overlay = null
    inputEl = null
    listEl = null
  }

  function open() {
    if (overlay) {
      inputEl?.focus()
      return
    }
    loadProjects()
    overlay = el('div', {
      class: 'fixed inset-0 bg-black/40 z-50 flex items-start justify-center px-4 pt-[12vh]',
    })
    const panel = el('div', { class: 'card w-[560px] max-w-full shadow-xl overflow-hidden' })
    const titleEl = el('span', { class: 'text-xs text-ink-muted' }, ['全局命令'])
    const head = el('div', { class: 'flex items-center gap-2 px-4 pt-3 pb-1' }, [
      svgIcon('search', 16) as unknown as Node,
      titleEl,
      el('span', { class: 'ml-auto text-xs text-ink-muted' }, ['⌘K']),
    ])
    inputEl = el('input', {
      class:
        'input w-full rounded-none border-0 border-b border-line bg-transparent px-4 py-3 text-sm focus:ring-0 focus:border-line',
      placeholder: '跳转页面或搜索项目…  ↑↓ 选择 · Enter 打开 · Esc 关闭',
    })
    inputEl.oninput = () => {
      sel = 0
      render()
    }
    inputEl.onkeydown = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        sel = Math.min(sel + 1, entries.length - 1)
        highlight()
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        sel = Math.max(sel - 1, 0)
        highlight()
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const hit = entries[sel]
        if (hit) run(hit)
      } else if (e.key === 'Escape') {
        e.preventDefault()
        close()
      }
    }
    listEl = el('div', { class: 'max-h-80 overflow-auto py-1' })
    panel.append(head, inputEl, listEl)
    overlay.appendChild(panel)
    overlay.onclick = (e) => {
      if (e.target === overlay) close()
    }
    document.body.appendChild(overlay)
    // P2-2：命令面板补对话框语义（role=dialog / aria-modal / Esc / 焦点陷阱）
    setupDialogAccessibility({ overlay, titleEl, onClose: close, initialFocus: inputEl })
    render()
    inputEl.focus()
  }

  function onGlobalKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault()
      open()
    } else if (e.key === 'Escape' && overlay) {
      close()
    }
  }
  window.addEventListener('keydown', onGlobalKey)

  return {
    open,
    destroy() {
      window.removeEventListener('keydown', onGlobalKey)
      close()
    },
  }
}
