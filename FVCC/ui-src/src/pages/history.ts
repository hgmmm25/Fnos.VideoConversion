import { store } from '../store'
import { api } from '../api'
import { el, toast, formatTime, emptyState, svgIcon } from '../ui'
import { type Task, type TaskStatus, STATUS_LABEL, STATUS_CLASS } from '../types'
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

  // §7 阶段B试点二：历史趋势图（lightweight-charts 动态 import，仅进页签时加载）
  const trend = makeTrendCard()

  const listWrap = el('div', { class: 'flex-1 overflow-auto' })
  const paginationWrap = el('div', { class: 'flex justify-center items-center gap-2 px-4 py-2 border-t border-line bg-surface-alt' })
  wrap.append(toolbar, trend.card, listWrap, paginationWrap)
  container.appendChild(wrap)

  const render = () => {
    trend.scheduleDraw()
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
  return () => { unsubStore(); page.dispose(); trend.dispose() }
}

// ===== §7 阶段B试点二：历史趋势图（lightweight-charts 动态 import；canvas 渲染；色板取自 token）=====
type TrendRange = 7 | 30 | 90 | 0
interface TrendDay { ts: number; label: string; done: number; err: number }
interface TrendChartHandle { remove(): void; applyOptions(o: Record<string, unknown>): void }

function tokenColor(name: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(`--c-${name}`).trim()
  const parts = v.split(/\s+/).filter(Boolean)
  return parts.length === 3 ? `rgb(${parts.join(' ')})` : (v || 'transparent')
}

function buildTrendDays(hist: Task[], range: TrendRange): TrendDay[] {
  const byDay = new Map<string, TrendDay>()
  const keyOf = (d: Date) => `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`
  for (const t of hist) {
    const ts = t.updatedAt ? new Date(t.updatedAt) : new Date()
    if (isNaN(ts.getTime())) continue
    const k = keyOf(ts)
    let day = byDay.get(k)
    if (!day) {
      day = { ts: new Date(ts.getFullYear(), ts.getMonth(), ts.getDate()).getTime(), label: `${ts.getMonth() + 1}/${ts.getDate()}`, done: 0, err: 0 }
      byDay.set(k, day)
    }
    if (t.status === 'COMPLETED') day.done++
    else if (t.status === 'ERROR') day.err++
  }
  if (range > 0) {
    const out: TrendDay[] = []
    const start = new Date()
    start.setHours(0, 0, 0, 0)
    start.setDate(start.getDate() - (range - 1))
    for (let i = 0; i < range; i++) {
      const d = new Date(start)
      d.setDate(start.getDate() + i)
      const hit = byDay.get(keyOf(d))
      out.push(hit ?? { ts: d.getTime(), label: `${d.getMonth() + 1}/${d.getDate()}`, done: 0, err: 0 })
    }
    return out
  }
  return [...byDay.values()].sort((a, b) => a.ts - b.ts)
}

function makeTrendCard() {
  let range: TrendRange = 30
  let collapsed = false
  let chart: TrendChartHandle | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let disposed = false

  const legend = el('span', { class: 'flex items-center gap-3 text-xs text-ink-muted' }, [])
  const rangeSel = el('select', { class: 'input text-sm h-9', 'aria-label': '趋势时间范围' }) as HTMLSelectElement
  for (const [v, l] of [[7, '近7天'], [30, '近30天'], [90, '近90天'], [0, '全部']] as const) {
    rangeSel.appendChild(el('option', { value: String(v) }, [l]))
  }
  rangeSel.value = '30'
  const foldBtn = el('button', { class: 'btn btn-sm w-11 h-11 flex items-center justify-center shrink-0', 'aria-label': '折叠/展开趋势图' }, [el('span', { class: 'text-xs' }, ['▲'])]) as HTMLButtonElement
  const body = el('div', { class: 'h-[220px] px-3 sm:px-4 pb-3' })

  const header = el('div', { class: 'flex flex-wrap items-center gap-2 px-3 sm:px-4 py-1.5 border-b border-line-subtle bg-surface' }, [
    el('span', { class: 'text-sm font-medium flex items-center gap-1.5' }, [svgIcon('history', 15) as unknown as Node, el('span', {}, ['历史趋势'])]),
    legend,
    el('span', { class: 'ml-auto flex items-center gap-2' }, [rangeSel, foldBtn]),
  ])
  const card = el('div', { class: 'border-b border-line bg-surface' }, [header, body])

  const renderLegend = (done: number, err: number) => {
    const total = done + err
    const rate = total > 0 ? ((done / total) * 100).toFixed(1) : '0.0'
    legend.innerHTML = ''
    legend.append(
      el('span', { class: 'flex items-center gap-1' }, [el('i', { class: 'inline-block w-2 h-2 rounded-sm', style: `background:${tokenColor('success')}` }), el('span', {}, [`完成 ${done}`])]),
      el('span', { class: 'flex items-center gap-1' }, [el('i', { class: 'inline-block w-2 h-2 rounded-sm', style: `background:${tokenColor('danger')}` }), el('span', {}, [`错误 ${err}`])]),
      el('span', { class: 'flex items-center gap-1' }, [el('i', { class: 'inline-block w-2 h-2 rounded-sm', style: `background:${tokenColor('signal')}` }), el('span', {}, [`成功率 ${rate}%`])])
    )
  }

  const scheduleDraw = () => {
    if (disposed || collapsed) return
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => { void draw() }, 200)
  }

  const draw = async () => {
    if (disposed || collapsed) return
    const days = buildTrendDays(store.history, range)
    const hasData = days.some((d) => d.done > 0 || d.err > 0)
    const totalDone = days.reduce((s, d) => s + d.done, 0)
    const totalErr = days.reduce((s, d) => s + d.err, 0)
    renderLegend(totalDone, totalErr)
    if (chart) { try { chart.remove() } catch { /* noop */ } chart = null }
    body.innerHTML = ''
    if (!hasData) {
      body.appendChild(el('div', { class: 'flex items-center justify-center h-full text-sm text-ink-muted py-10' }, ['暂无历史数据，完成转码后这里会展示趋势']))
      return
    }
    try {
      const lwc = await import('lightweight-charts')
      const w = Math.max(body.clientWidth || 600, 320)
      const h = body.clientHeight || 220
      const inst = lwc.createChart(body, {
        width: w,
        height: h,
        layout: { background: { type: lwc.ColorType.Solid, color: 'transparent' }, textColor: tokenColor('ink-muted'), fontSize: 11 },
        grid: { vertLines: { color: tokenColor('line-subtle') }, horzLines: { color: tokenColor('line-subtle') } },
        rightPriceScale: { borderColor: tokenColor('line-subtle') },
        leftPriceScale: { borderColor: tokenColor('line-subtle') },
        timeScale: { borderColor: tokenColor('line-subtle'), timeVisible: false, rightOffset: 2 },
        crosshair: { mode: 0 },
      })
      chart = inst as unknown as TrendChartHandle
      const times = days.map((d) => { const dt = new Date(d.ts); return { year: dt.getFullYear(), month: dt.getMonth() + 1, day: dt.getDate() } })
      const doneSeries = inst.addSeries(lwc.HistogramSeries, {
        priceFormat: { type: 'volume' },
        priceScaleId: 'right',
        color: tokenColor('success'),
      })
      doneSeries.setData(days.map((d, i) => ({ time: times[i], value: d.done })))
      const errSeries = inst.addSeries(lwc.LineSeries, {
        color: tokenColor('danger'),
        lineWidth: 2,
        priceScaleId: 'right',
      })
      errSeries.setData(days.map((d, i) => ({ time: times[i], value: d.err })))
      const rateSeries = inst.addSeries(lwc.LineSeries, {
        color: tokenColor('signal'),
        lineWidth: 2,
        priceScaleId: 'left',
        lineStyle: lwc.LineStyle.Dashed,
        autoscaleInfoProvider: () => ({ priceRange: { minValue: 0, maxValue: 100 } }),
      })
      rateSeries.setData(days.map((d, i) => ({ time: times[i], value: d.done + d.err > 0 ? Math.round((d.done / (d.done + d.err)) * 1000) / 10 : 0 })))
      inst.timeScale().fitContent()
    } catch (e) {
      body.appendChild(el('div', { class: 'flex items-center justify-center h-full text-sm text-danger py-10' }, [`趋势图加载失败：${(e as Error).message}`]))
    }
  }

  foldBtn.onclick = () => {
    collapsed = !collapsed
    body.style.display = collapsed ? 'none' : ''
    foldBtn.firstChild!.textContent = collapsed ? '▼' : '▲'
    if (!collapsed) scheduleDraw()
  }
  rangeSel.onchange = () => { range = Number(rangeSel.value) as TrendRange; scheduleDraw() }

  const dispose = () => {
    disposed = true
    if (timer) clearTimeout(timer)
    if (chart) { try { chart.remove() } catch { /* noop */ } chart = null }
  }

  return { card, scheduleDraw, dispose }
}
