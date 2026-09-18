import './style.css'
// P1-3：IBM Plex 西文字体本地打包（NAS 离线可用；中文回退系统字体栈）
import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/500.css'
import '@fontsource/ibm-plex-sans/600.css'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import { store } from './store'
import { ws } from './ws'
import { initCommandPalette } from './command'
import { el, svgIcon, type IconName } from './ui'
import { initTheme } from './theme'
import { renderTasks } from './pages/tasks'
import { renderScanner } from './pages/scanner'
import { renderServers } from './pages/servers'
import { renderProfiles } from './pages/profiles'
import { renderHistory } from './pages/history'
import { renderSettings } from './pages/settings'
import { renderEditor, renderProjectPicker } from './pages/editor'

type PageId = 'editor' | 'tasks' | 'scanner' | 'servers' | 'profiles' | 'history' | 'settings'

const PAGE_ITEMS: { id: PageId; label: string; icon: IconName }[] = [
  { id: 'editor', label: '剪辑', icon: 'scissors' },
  { id: 'tasks', label: '任务清单', icon: 'list' },
  { id: 'scanner', label: '视频文件', icon: 'video' },
  { id: 'servers', label: '转码服务器', icon: 'server' },
  { id: 'profiles', label: '转码方案', icon: 'edit' },
  { id: 'history', label: '历史任务', icon: 'history' },
  { id: 'settings', label: '设置', icon: 'settings' },
]

// P1-4：导航分组（仅外壳展示层；hash 路由与 data-page 高亮机制保持兼容）
const NAV_GROUPS: { label: string; items: (typeof PAGE_ITEMS)[number][] }[] = [
  { label: '编辑工作区', items: [PAGE_ITEMS[0]] },
  { label: '资源', items: [PAGE_ITEMS[2], PAGE_ITEMS[4]] },
  { label: '运行', items: [PAGE_ITEMS[1], PAGE_ITEMS[5], PAGE_ITEMS[3]] },
  { label: '系统', items: [PAGE_ITEMS[6]] },
]
const PAGES = PAGE_ITEMS

/** 计入全局「队列长度」的活动任务状态（排队/传输/转码/冷却均视为在队列中） */
const ACTIVE_TASK_STATUS = new Set([
  'QUEUE',
  'UPLOADING',
  'WAITING_TRANS',
  'TRANSCODING',
  'WAITING_DOWN',
  'DOWNLOADING',
  'COOLDOWN',
])

// ===== C-02：hash 路由（#/editor/:projectId，其余为各页签）=====
interface Route {
  page: PageId
  projectId?: string
}

function parseRoute(): Route {
  const raw = location.hash.replace(/^#\/?/, '')
  if (!raw) return { page: 'tasks' }
  const [head, ...rest] = raw.split('/')
  if (head === 'editor') {
    return { page: 'editor', projectId: rest[0] ? decodeURIComponent(rest[0]) : undefined }
  }
  const hit = PAGES.find((p) => p.id === head)
  return { page: hit ? hit.id : 'tasks' }
}

let route: Route = { page: 'tasks' }
/** 编辑器离开时的清理回调（释放 DOM 与 store.currentProjectId） */
let editorCleanup: (() => void) | null = null
/** 异步渲染令牌：防止慢请求回来后覆盖已切换的新路由 */
let renderToken = 0

function syncTabHighlight() {
  document.querySelectorAll('[data-page]').forEach((b) => {
    const btn = b as HTMLButtonElement
    const active = btn.dataset.page === route.page
    const isMobile = btn.dataset.mobile === '1'
    const baseClass = isMobile
      // P0-4：移动端导航可点区域 ≥44px（min-h-11）
      ? 'px-3 py-2 min-h-11 text-sm rounded transition-colors flex items-center gap-2 '
      : 'px-2.5 py-1.5 text-sm rounded transition-colors flex items-center gap-1.5 whitespace-nowrap '
    btn.className =
      baseClass + (active ? 'bg-primary text-white' : 'text-ink-muted hover:bg-surface-hover')
  })
}

function renderShell(): HTMLElement {
  const app = document.getElementById('app')!
  app.innerHTML = ''
  const shell = el('div', { class: 'h-screen flex flex-col min-w-[320px]' })

  // P1-4：全局命令面板（Ctrl/Cmd+K 或顶栏命令框）
  const palette = initCommandPalette({
    pages: PAGE_ITEMS.map((p) => ({ id: p.id, label: p.label, icon: p.icon })),
    onNavigate: (id) => navigate(id as PageId),
  })

  // 顶部导航
  const nav = el('nav', {
    class:
      'flex items-center gap-1 px-2 sm:px-4 h-12 bg-surface border-b border-line shrink-0',
  })
  const brand = el(
    'div',
    { class: 'font-bold text-primary mr-2 sm:mr-4 flex items-center gap-2 shrink-0' },
    [svgIcon('film', 20) as unknown as Node, el('span', { class: 'hidden sm:inline' }, ['视频转码'])]
  )
  nav.appendChild(brand)

  // 移动端菜单按钮（小屏显示）
  const menuBtn = el('button', {
    class: 'md:hidden btn btn-sm flex items-center',
    'aria-label': '菜单',
  })
  menuBtn.append(svgIcon('list', 20) as unknown as Node)
  nav.appendChild(menuBtn)

  // 桌面端分组 tabs（md 及以上显示）
  const tabs = el('div', { class: 'hidden md:flex items-center gap-0.5 overflow-x-auto' })
  for (const g of NAV_GROUPS) {
    tabs.appendChild(
      el(
        'span',
        {
          class:
            'px-2 text-xs font-semibold uppercase tracking-wider text-ink-muted/70 select-none whitespace-nowrap',
        },
        [g.label]
      )
    )
    for (const p of g.items) {
      const btn = el('button', {
        class:
          'px-2.5 py-1.5 text-sm rounded transition-colors flex items-center gap-1.5 whitespace-nowrap ' +
          (p.id === route.page
            ? 'bg-primary text-white'
            : 'text-ink-muted hover:bg-surface-hover'),
        'data-page': p.id,
      })
      btn.append(svgIcon(p.icon, 16) as unknown as Node, el('span', {}, [p.label]))
      btn.onclick = () => navigate(p.id)
      tabs.appendChild(btn)
    }
  }
  nav.appendChild(tabs)

  // P1-4：全局命令入口（桌面端，顶栏右侧）
  const cmdBtn = el('button', {
    class:
      'hidden md:flex ml-auto items-center gap-1.5 px-2.5 py-1.5 text-xs rounded transition-colors border border-line bg-surface-alt text-ink-muted hover:bg-surface-hover',
    'aria-label': '全局命令 (Ctrl+K)',
    title: '全局命令 (Ctrl+K)',
  })
  cmdBtn.append(
    svgIcon('search', 14) as unknown as Node,
    el('span', { class: 'text-ink-muted' }, ['跳转 / 搜索']),
    el('kbd', { class: 'text-xs px-1 py-0.5 rounded bg-surface-hover text-ink-muted' }, ['⌘K'])
  )
  cmdBtn.onclick = () => palette.open()
  nav.appendChild(cmdBtn)

  // 移动端分组下拉菜单容器（默认隐藏，点击菜单按钮切换）
  const mobileMenu = el('div', {
    class: 'hidden md:hidden flex-col gap-1 px-2 py-2 bg-surface border-b border-line',
  })
  // P1-4：移动端命令入口（无快捷键，放菜单首位）
  const mCmd = el('button', {
    class:
      'px-3 py-2 min-h-11 text-sm rounded transition-colors flex items-center gap-2 text-ink-muted hover:bg-surface-hover',
  })
  mCmd.append(
    svgIcon('search', 16) as unknown as Node,
    el('span', {}, ['跳转页面 / 搜索项目'])
  )
  mCmd.onclick = () => {
    mobileMenu.classList.add('hidden')
    mobileMenu.classList.remove('flex')
    palette.open()
  }
  mobileMenu.appendChild(mCmd)
  for (const g of NAV_GROUPS) {
    mobileMenu.appendChild(
      el(
        'div',
        {
          class:
            'px-3 pt-2 pb-1 text-xs font-semibold uppercase tracking-wider text-ink-muted/70 select-none',
        },
        [g.label]
      )
    )
    for (const p of g.items) {
      const btn = el('button', {
        // P0-4：移动端下拉菜单项可点区域 ≥44px（min-h-11）
        class:
          'px-3 py-2 min-h-11 text-sm rounded transition-colors flex items-center gap-2 ' +
          (p.id === route.page
            ? 'bg-primary text-white'
            : 'text-ink-muted hover:bg-surface-hover'),
        'data-page': p.id,
        'data-mobile': '1',
      })
      btn.append(svgIcon(p.icon, 16) as unknown as Node, el('span', {}, [p.label]))
      btn.onclick = () => {
        navigate(p.id)
        mobileMenu.classList.add('hidden')
        mobileMenu.classList.remove('flex')
      }
      mobileMenu.appendChild(btn)
    }
  }
  let mobileOpen = false
  menuBtn.onclick = () => {
    mobileOpen = !mobileOpen
    if (mobileOpen) {
      mobileMenu.classList.remove('hidden')
      mobileMenu.classList.add('flex')
    } else {
      mobileMenu.classList.add('hidden')
      mobileMenu.classList.remove('flex')
    }
  }

  // 内容区
  const content = el('div', { class: 'flex-1 overflow-auto', id: 'page-content' })

  // ===== P1-4：全局状态条（外壳底部细条，所有页面可见；数据来自 store/ws 真实状态）=====
  const statusBar = el('div', {
    class:
      'flex items-center gap-3 sm:gap-5 px-3 h-7 text-xs shrink-0 bg-surface border-t border-line text-ink-muted overflow-x-auto',
  })
  const nodeDot = el('span', { class: 'w-1.5 h-1.5 rounded-full inline-block shrink-0 bg-ink-muted/40' })
  const nodeEl = el('span', {})
  const nodeWrap = el('div', { class: 'flex items-center gap-1.5 shrink-0' })
  nodeWrap.append(nodeDot, nodeEl)
  const queueDot = el('span', { class: 'w-1.5 h-1.5 rounded-full inline-block shrink-0 bg-ink-muted/40' })
  const queueEl = el('span', {})
  const queueWrap = el('div', { class: 'flex items-center gap-1.5 shrink-0' })
  queueWrap.append(queueDot, queueEl)
  const statusSpacer = el('div', { class: 'flex-1 min-w-2' })
  const wsDot = el('span', { class: 'w-1.5 h-1.5 rounded-full inline-block shrink-0 bg-ink-muted/40' })
  const wsEl = el('span', {})
  const wsWrap = el('div', { class: 'flex items-center gap-1.5 shrink-0' })
  wsWrap.append(wsDot, wsEl)
  statusBar.append(nodeWrap, queueWrap, statusSpacer, wsWrap)

  function updateStatusBar() {
    const online = store.servers.filter((s) => s.status === 'online').length
    const total = store.servers.length
    if (total === 0) {
      nodeEl.textContent = '节点 未配置'
      nodeDot.className = 'w-1.5 h-1.5 rounded-full inline-block shrink-0 bg-ink-muted/40'
    } else {
      nodeEl.textContent = `节点 ${online}/${total} 在线`
      nodeDot.className =
        'w-1.5 h-1.5 rounded-full inline-block shrink-0 ' + (online > 0 ? 'bg-success' : 'bg-danger')
    }
    const queueLen = store.tasks.filter((t) => ACTIVE_TASK_STATUS.has(t.status)).length
    queueEl.textContent = `队列 ${queueLen}`
    queueDot.className =
      'w-1.5 h-1.5 rounded-full inline-block shrink-0 ' +
      (queueLen > 0 ? 'bg-signal' : 'bg-ink-muted/40')
    const connected = ws.isConnected()
    wsEl.textContent = connected ? 'WS 已连接' : 'WS 已断开'
    wsDot.className =
      'w-1.5 h-1.5 rounded-full inline-block shrink-0 ' + (connected ? 'bg-success' : 'bg-danger')
  }
  // 状态条随 store（servers/tasks）与 WS 连接状态刷新
  store.subscribe(updateStatusBar)
  ws.onStatusChange(updateStatusBar)
  updateStatusBar()

  shell.append(nav, mobileMenu, content, statusBar)
  app.appendChild(shell)

  return content
}

function navigate(page: PageId) {
  const target = '#/' + page
  if (location.hash === target) {
    applyRoute()
  } else {
    location.hash = target
  }
}

/** 路由变化 → 清理旧页 → 渲染新页 */
function applyRoute() {
  route = parseRoute()
  syncTabHighlight()
  renderPage()
}

/** 修复③：页面渲染失败时给出可见错误提示，避免主区域静默空白 */
function renderPageError(content: HTMLElement, pageLabel: string, err: unknown) {
  const msg = err instanceof Error ? err.message : String(err)
  content.innerHTML = ''
  const box = el('div', { class: 'p-6 max-w-2xl mx-auto' })
  box.append(
    el('div', {
      class: 'bg-surface rounded-lg p-5 flex flex-col gap-3',
    }, [
      el('div', { class: 'text-danger font-bold text-base' }, [`${pageLabel}加载失败`]),
      el('div', { class: 'text-ink-muted text-sm' }, ['页面渲染时发生异常，可尝试刷新或返回其他页签。']),
      el('pre', {
        class:
          'text-xs bg-surface-hover rounded p-3 overflow-auto whitespace-pre-wrap break-all text-ink-muted',
      }, [msg]),
    ])
  )
  const retry = el('button', { class: 'btn btn-sm self-start' }, ['重新加载'])
  retry.onclick = () => renderPage()
  box.append(retry)
  content.appendChild(box)
}

function renderPage() {
  const content = document.getElementById('page-content')!
  const token = ++renderToken
  if (editorCleanup) {
    editorCleanup()
    editorCleanup = null
  }
  content.innerHTML = ''
  switch (route.page) {
    case 'editor':
      if (route.projectId) {
        const pid = route.projectId
        renderEditor(content, pid)
          .then((cleanup) => {
            if (token !== renderToken) {
              cleanup()
              return
            }
            editorCleanup = cleanup
          })
          .catch((e) => {
            console.error('[main] render editor failed', e)
            if (token === renderToken) renderPageError(content, '剪辑页面', e)
          })
      } else {
        renderProjectPicker(content).catch((e) => {
          console.error('[main] render picker failed', e)
          renderPageError(content, '项目列表', e)
        })
      }
      break
    case 'tasks':
      renderTasks(content)
      break
    case 'scanner':
      renderScanner(content)
      break
    case 'servers':
      renderServers(content)
      break
    case 'profiles':
      renderProfiles(content)
      break
    case 'history':
      renderHistory(content)
      break
    case 'settings':
      renderSettings(content)
      break
  }
}

async function main() {
  initTheme()
  route = parseRoute()
  renderShell()
  window.addEventListener('hashchange', applyRoute)
  await store.loadInfo()
  await store.loadAll()
  store.startWS()
  renderPage()
}

main().catch((e) => {
  console.error('[main] init failed', e)
  const content = document.getElementById('page-content')
  if (content) renderPageError(content as HTMLElement, '应用', e)
})
