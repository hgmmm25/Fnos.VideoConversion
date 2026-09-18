import { store } from '../store'
import { api } from '../api'
import { el, toast, formatTime, emptyState, svgIcon } from '../ui'
import { type TaskStatus, STATUS_LABEL, STATUS_CLASS } from '../types'
import { crudActions } from '../lib/crudActions'
import { useListPage } from '../lib/useListPage'

// 排序状态
type SortKey = 'fileName' | 'status' | 'serverId' | 'createdAt' | 'updatedAt'
let sortKey: SortKey = 'createdAt'
let sortDir: 'asc' | 'desc' = 'desc'

// 分页状态
const pageSize = 20
let currentPage = 1
let totalItems = 0

export function renderHistory(container: HTMLElement) {
  const wrap = el('div', { class: 'flex flex-col h-full' })

  const toolbar = el('div', {
    class: 'flex flex-wrap items-center gap-2 sm:gap-3 px-3 sm:px-4 py-2 border-b border-line bg-surface',
  })
  const refresh = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
  refresh.append(svgIcon('refresh', 14) as unknown as Node, el('span', {}, ['刷新']))
  refresh.onclick = () => { currentPage = 1; loadHistoryPage() }
  const filterSel = el('select', { class: 'input w-28 sm:w-32 text-sm' }) as HTMLSelectElement
  for (const [v, l] of [['all', '全部'], ['completed', '完成'], ['error', '错误'], ['cancelled', '取消']]) {
    filterSel.appendChild(el('option', { value: v }, [l]))
  }
  const stats = el('span', { class: 'ml-auto text-xs text-ink-muted' }, [])
  toolbar.append(refresh, filterSel, stats)

  const listWrap = el('div', { class: 'flex-1 overflow-auto' })
  const paginationWrap = el('div', { class: 'flex justify-center items-center gap-2 px-4 py-2 border-t border-line bg-surface-alt' })
  wrap.append(toolbar, listWrap, paginationWrap)
  container.appendChild(wrap)

  const render = () => {
    listWrap.innerHTML = ''
    let list = store.history
    const f = filterSel.value
    if (f !== 'all') list = list.filter((t) => t.status.toLowerCase() === f)

    // 排序
    const sorted = [...list].sort((a, b) => {
      let av: string | number = ''
      let bv: string | number = ''
      switch (sortKey) {
        case 'fileName': av = a.fileName; bv = b.fileName; break
        case 'status': av = a.status; bv = b.status; break
        case 'serverId': av = a.serverId; bv = b.serverId; break
        case 'createdAt': av = a.createdAt; bv = b.createdAt; break
        case 'updatedAt': av = a.updatedAt; bv = b.updatedAt; break
      }
      if (av < bv) return sortDir === 'asc' ? -1 : 1
      if (av > bv) return sortDir === 'asc' ? 1 : -1
      return 0
    })

    // 分页
    const start = (currentPage - 1) * pageSize
    const end = start + pageSize
    const paginated = sorted.slice(start, end)
    totalItems = sorted.length

    stats.textContent = `共 ${totalItems} 条历史记录`
    renderPagination()

    if (sorted.length === 0) {
      // P2-1：空状态承载"下一步动作"
      listWrap.appendChild(
        emptyState('暂无历史任务', 'history', {
          label: '去任务清单页',
          onClick: () => {
            location.hash = '#/tasks'
          },
        })
      )
      return
    }

    const tbl = el('table', { class: 'w-full text-sm min-w-[720px] whitespace-nowrap' })
    const thead = el('thead', { class: 'sticky top-0 bg-surface-alt text-ink-muted' })
    const hr = el('tr', { class: 'border-b border-line' })

    // 列定义
    const columns: { key: SortKey | ''; label: string }[] = [
      { key: 'fileName', label: '文件名' },
      { key: 'status', label: '状态' },
      { key: 'serverId', label: '服务器' },
      { key: 'createdAt', label: '创建时间' },
      { key: 'updatedAt', label: '完成时间' },
      { key: '', label: '错误' },
      { key: '', label: '操作' },
    ]

    for (const col of columns) {
      const th = el('th', { class: 'text-left px-4 py-2 font-medium' })
      const isSortable = col.key !== ''
      const isActive = sortKey === col.key
      if (isSortable) {
        th.style.cursor = 'pointer'
        th.onclick = () => {
          if (sortKey === col.key) {
            sortDir = sortDir === 'asc' ? 'desc' : 'asc'
          } else {
            sortKey = col.key as SortKey
            sortDir = 'asc'
          }
          render()
        }
      }
      th.appendChild(el('span', {}, [col.label]))
      if (isSortable && isActive) {
        th.appendChild(el('span', { class: 'text-xs ml-1' }, [sortDir === 'asc' ? '▲' : '▼']))
      }
      hr.appendChild(th)
    }
    thead.appendChild(hr)
    tbl.appendChild(thead)

    const tbody = el('tbody')
    for (const t of paginated) {
      const tr = el('tr', { class: 'border-b border-line-subtle hover:bg-surface-hover' })
      tr.appendChild(el('td', { class: 'px-4 py-2 truncate max-w-xs' }, [t.fileName]))
      tr.appendChild(el('td', { class: 'px-4 py-2' }, [el('span', { class: `badge ${STATUS_CLASS[t.status as TaskStatus]}` }, [STATUS_LABEL[t.status as TaskStatus]])]))
      const sv = store.servers.find((s) => s.id === t.serverId)
      tr.appendChild(el('td', { class: 'px-4 py-2 whitespace-nowrap' }, [sv?.name ?? t.serverId]))
      tr.appendChild(el('td', { class: 'px-4 py-2 whitespace-nowrap text-xs' }, [formatTime(t.createdAt)]))
      tr.appendChild(el('td', { class: 'px-4 py-2 whitespace-nowrap text-xs' }, [formatTime(t.updatedAt)]))
      tr.appendChild(el('td', { class: 'px-4 py-2 truncate max-w-xs text-xs text-danger' }, [t.errorMsg || '-']))
      const del = el('button', { class: 'btn btn-sm btn-danger' }, ['删除'])
      del.onclick = () => {
        historyActions.remove(t.id)
      }
      tr.appendChild(el('td', { class: 'px-4 py-2' }, [del]))
      tbody.appendChild(tr)
    }
    tbl.appendChild(tbody)
    listWrap.appendChild(tbl)
  }

  function renderPagination() {
    paginationWrap.innerHTML = ''
    const totalPages = Math.ceil(totalItems / pageSize)
    if (totalPages <= 1) return

    const mkBtn = (label: string, disabled: boolean, onClick: () => void): HTMLButtonElement => {
      const attrs: Record<string, string> = { class: 'btn btn-sm' }
      if (disabled) attrs.disabled = 'true'
      const btn = el('button', attrs, [label]) as HTMLButtonElement
      if (!disabled) btn.onclick = onClick
      return btn
    }

    const firstBtn = mkBtn('<<', currentPage === 1, () => { currentPage = 1; render() })
    const prevBtn = mkBtn('<', currentPage === 1, () => { currentPage--; render() })
    const pageInfo = el('span', { class: 'text-xs text-ink-muted min-w-[80px] text-center' }, [
      `第 ${currentPage} / ${totalPages} 页`
    ])
    const nextBtn = mkBtn('>', currentPage >= totalPages, () => { currentPage++; render() })
    const lastBtn = mkBtn('>>', currentPage >= totalPages, () => { currentPage = totalPages; render() })

    paginationWrap.append(firstBtn, prevBtn, pageInfo, nextBtn, lastBtn)
  }

  async function loadHistoryPage() {
    try {
      const r = await api.listHistory()
      store.history = r.tasks
      totalItems = r.total || r.tasks.length
      store.notify()
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  // P2-2：删除历史记录走公共 CRUD 四件套；成功后重置分页并刷新
  const historyActions = crudActions<string>({
    confirmTitle: () => '确定删除该历史记录？',
    danger: false,
    apiCall: (_action, id) => api.deleteHistory(id),
    reload: async () => {
      currentPage = 1
      await loadHistoryPage()
    },
    successMsg: () => '已删除',
  })

  // P2-2：store 通知直接重渲染本地已加载数据（不触发重新请求，避免分页加载内 notify 造成刷新循环）；首次加载/刷新走 useListPage
  const unsubStore = store.subscribe(render)
  const page = useListPage({
    load: async () => {
      await loadHistoryPage()
      return []
    },
    render: () => render(),
    errorLabel: '历史记录',
  })
  page.mount()
  filterSel.onchange = () => { currentPage = 1; render() }
  render()
  return () => { unsubStore(); page.dispose() }
}
