// C-04：素材面板（01 §4.2 / US-01）
// 目录扫描 + 列表（缩略图/时长/分辨率/编码/需代理角标）+ 排序 + 本地搜索 + 双击或拖拽添加整段
// 2026-09-16 修复：①多授权目录 + 目录下拉/浏览；②分批扫描结果累加（不再逐批覆盖）；
//                  ③代理状态落地（queued/ready/invalid）+ 订阅 WS proxy_ready；
//                  ④缩略图按时间段分格（时间线侧，见 timeline.ts）；⑤deduplicated 字段消费；
//                  ⑥订阅/缓存清理、toRelative 兜底收敛、sourceRoot 随设置刷新。
import { el, toast } from '../../ui'
import { api } from '../../api'
import { store, type ProxyReadyInfo } from '../../store'
import type { AssetRef, AssetVisibility, ProxyState, VideoInfo } from '../../types'
import { EDL_LIMITS, formatMs, newAssetId } from './format'
import type { EditorStore } from './editorStore'
import { openDirBrowser } from '../scanner'

export interface AssetsPanelOptions {
  edlStore: EditorStore
  /** 素材根候选（= Settings.accessiblePaths，01 §4.2 多目录授权）；首项为默认根 */
  roots?: string[]
  /** 单根简写（等价 roots: [sourceRoot]，保留旧调用签名） */
  sourceRoot?: string
  /** 首次扫描目录，默认取首个授权根 */
  initialDir?: string
  /** 切换授权根后回调（上层可持久化偏好或联动其它面板） */
  onRootChange?(root: string): void
  /** 单击 = 装配到预览器并 seek 到起点（C-05 装配点；不传则仅支持双击添加） */
  onPick?(asset: AssetRef): void
  /** 双击 / 拖拽 = 添加整段（P0 仅支持整段，01 §4.2） */
  onAddWhole(asset: AssetRef): void
  /** 生成代理任务已提交后的回调（上层刷新任务中心等；代理状态由面板自行维护） */
  onRequestProxy?(asset: AssetRef): void
}

export interface AssetsPanel {
  root: HTMLElement
  /** 供时间线拖拽校验：当前已扫描素材 */
  list(): AssetRef[]
  /** 当前生效的授权根（素材相对路径基准） */
  currentRoot(): string
  /** Settings 变更后刷新授权根（修复 sourceRoot 不随设置刷新） */
  setRoots(roots: string[]): void
  refresh(dir?: string): void
  destroy(): void
}

/** 浏览器可直读的编码 / 封装白名单（01 §5.2 判定表：codec ∈ {h264, hevc} 且 format ∈ {mp4, mov, m4v}） */
const DIRECT_CODECS = new Set(['h264', 'avc1', 'hevc', 'hvc1'])
const DIRECT_CONTAINERS = ['mp4', 'mov', 'm4v']
/** 码率阈值：超过则判为需代理（01 §5.2"bitrate ≤ 20 Mbps"） */
const DIRECT_MAX_BITRATE = 20_000_000

function parseBitrate(bps: string): number {
  const m = /([\d.]+)\s*([kmg]?)bps/i.exec(bps || '')
  if (!m) return 0
  const n = parseFloat(m[1])
  const unit = m[2].toLowerCase()
  const mul = unit === 'k' ? 1e3 : unit === 'm' ? 1e6 : unit === 'g' ? 1e9 : 1
  return Number.isFinite(n) ? Math.round(n * mul) : 0
}

/** 可见性判定：direct / need_proxy（01 §5.2 判定表）
 *  偏差登记（待回写 10 文档）：后端 VideoInfo 未暴露 "moov 是否前置" 标记，
 *  P0 仅按 编码 + 封装 + 码率 三项判定，非前置 moov 素材可能被误判为 direct。
 */
export function judgeVisibility(v: Pick<VideoInfo, 'codec' | 'format' | 'bitrate'>): AssetVisibility {
  const codec = (v.codec || '').toLowerCase().split(/[\s/]/)[0]
  // Format = 文件扩展名（models.go:250），非 ffprobe container 串
  const container = (v.format || '').toLowerCase().replace(/^\./, '')
  const containersOk = DIRECT_CONTAINERS.some((c) => container === c)
  if (!containersOk) return 'need_proxy'
  if (!DIRECT_CODECS.has(codec)) return 'need_proxy'
  if (parseBitrate(v.bitrate) > DIRECT_MAX_BITRATE) return 'need_proxy'
  return 'direct'
}

const normPath = (p: string) => (p || '').replace(/\\/g, '/').replace(/\/+$/, '')

/** 规范化授权根列表（去重 + 去尾斜杠；兼容单根 sourceRoot 入参） */
export function normalizeRoots(roots?: string[], single?: string): string[] {
  const raw = roots && roots.length ? roots : single ? [single] : []
  const out: string[] = []
  for (const r of raw) {
    const n = normPath(r)
    if (n && !out.some((x) => x.toLowerCase() === n.toLowerCase())) out.push(n)
  }
  return out
}

/** 绝对路径 → 相对 sourceRoot 的 POSIX 路径（03 §2.1：file 必须是相对 SourceRoot 的路径） */
export function toRelative(absPath: string, sourceRoot: string): string {
  const p = normPath(absPath)
  const root = normPath(sourceRoot)
  const pl = p.toLowerCase()
  const rl = root.toLowerCase()
  // 大小写不敏感比较（Windows 盘符/目录名常有大小写差异）
  if (root && (pl === rl || pl.startsWith(rl + '/'))) {
    return p.length > root.length ? p.slice(root.length + 1) : ''
  }
  // 兜底（2026-09-16 收敛）：跨根 / 盘符差异时退化为「去盘符 + 去前导斜杠」，
  // 交由服务端 PathValidator 裁决；不再返回带盘符的绝对路径（后端必然拒绝且定位困难）。
  const m = /^[A-Za-z]:\/(.*)$/.exec(p)
  return (m ? m[1] : p).replace(/^\/+/, '')
}

/** 代理相对路径 → 源相对路径（demo/a_01.proxy.mp4 → demo/a_01.mp4；与后端 proxyRelOf 互逆） */
export function fileFromProxyRel(proxyRel: string): string {
  const rel = normPath(proxyRel)
  const i = rel.lastIndexOf('/')
  const dir = i >= 0 ? rel.slice(0, i + 1) : ''
  const base = i >= 0 ? rel.slice(i + 1) : rel
  const dot = base.lastIndexOf('.')
  const stem = dot > 0 ? base.slice(0, dot) : base
  const ext = dot > 0 ? base.slice(dot) : '.mp4'
  return dir + stem.replace(/\.proxy$/, '') + ext
}

/** /video/scan 结果 → AssetRef（素材面板与时间线共用的最小装配） */
export function toAssetRef(v: VideoInfo, sourceRoot: string, assetId?: string): AssetRef {
  return {
    assetId: assetId || newAssetId(),
    file: toRelative(v.path, sourceRoot),
    durationMs: Math.max(0, Math.round((v.duration || 0) * 1000)),
    width: v.width || 0,
    height: v.height || 0,
    fps: Number.parseInt(v.fps || '0', 10) || 0,
    videoCodec: v.codec || '',
    bitrate: parseBitrate(v.bitrate),
    visibility: judgeVisibility(v),
    proxyState: 'none',
  }
}

type SortKey = 'name' | 'duration' | 'size'

interface ProxyRecord {
  state: ProxyState
  proxyFile?: string
  durationMs?: number
}

export function buildAssetsPanel(opts: AssetsPanelOptions): AssetsPanel {
  const { edlStore } = opts
  let roots = normalizeRoots(opts.roots, opts.sourceRoot)
  let activeRoot = roots[0] || ''
  let currentDir = opts.initialDir || activeRoot
  if (currentDir) activeRoot = rootForPath(currentDir) || activeRoot
  let assets: AssetRef[] = []
  /** 缩略图需要的时长/文件名等展示字段（AssetRef 不含，单独保留）；key = 相对路径（file） */
  const meta = new Map<string, VideoInfo>()
  /** 代理状态登记表（key = 相对路径）；跨扫描保留，避免重扫后状态丢失 */
  const proxyByFile = new Map<string, ProxyRecord>()
  /** file → assetId 稳定映射（同一素材重扫/分批到达时保持同一 assetId，供 WS proxy_ready 匹配） */
  const idByFile = new Map<string, string>()
  let keyword = ''
  let sortKey: SortKey = 'name'
  // 需求2：名称/时长/大小 支持正序/倒序（1 正序 / -1 倒序）
  let sortDir: 1 | -1 = 1
  // 需求3：文件夹式阅览——非递归扫描，子目录由前端据视频相对路径推导后在此列展示
  const recursive = false
  interface DirEntry {
    name: string
    path: string
  }
  let dirs: DirEntry[] = []
  let scan: { cancel(): void } | null = null
  let destroyed = false

  // 修复①：授权目录下拉（多根切换）+ 浏览对话框（browseDirs 受后端授权校验约束）
  // 2026-09-20 UI 精简：删除「路径手填 + 扫描」对话框（所有目录已由授权下拉/浏览覆盖）
  const rootSel = el('select', {
    class: 'input text-xs flex-1 min-w-0',
    title: '切换授权素材目录',
  }) as HTMLSelectElement
  rootSel.onchange = () => {
    const next = rootSel.value
    if (next) setActiveRoot(next, true)
  }
  const browseBtn = el('button', { class: 'btn btn-primary shrink-0 text-xs px-2 py-1' }, ['浏览…'])
  browseBtn.onclick = () => openDirBrowser((dir) => startScan(dir), currentDir || activeRoot)
  syncRootOptions()

  const searchInput = el('input', {
    class: 'input text-xs',
    placeholder: '搜索文件名（本地过滤）',
  }) as HTMLInputElement
  searchInput.oninput = () => {
    keyword = searchInput.value.trim().toLowerCase()
    renderList()
  }

  const sortSel = el('select', { class: 'input text-xs w-20 shrink-0', title: '排序字段' }, [
    el('option', { value: 'name' }, ['名称']),
    el('option', { value: 'duration' }, ['时长']),
    // 扫描接口未返回 mtime，P0 以"大小"替代"修改时间"（偏差已登记）
    el('option', { value: 'size' }, ['大小']),
  ]) as HTMLSelectElement
  sortSel.onchange = () => {
    sortKey = sortSel.value as SortKey
    renderList()
  }
  // 需求2：正序/倒序切换按钮（名称/时长/大小 三字段共用）
  const dirBtn = el('button', {
    class: 'btn shrink-0 text-xs px-1.5 py-1 w-8',
    title: sortDir === 1 ? '当前正序，点击切换为倒序' : '当前倒序，点击切换为正序',
  }, [sortDir === 1 ? '↑' : '↓']) as HTMLButtonElement
  dirBtn.onclick = () => {
    sortDir = sortDir === 1 ? -1 : 1
    dirBtn.textContent = sortDir === 1 ? '↑' : '↓'
    dirBtn.title = sortDir === 1 ? '当前正序，点击切换为倒序' : '当前倒序，点击切换为正序'
    renderList()
  }

  const breadcrumb = el('div', { class: 'flex items-center gap-1 text-xs text-ink-muted' }, [])
  const listBox = el('div', { class: 'flex-1 overflow-y-auto divide-y divide-line' }, [])
  const statusEl = el('div', { class: 'px-1 text-xs text-ink-muted' }, ['就绪'])

  const root = el('div', { class: 'flex flex-col h-full min-h-0 gap-2' }, [
    el('div', { class: 'flex items-center gap-1' }, [rootSel, browseBtn]),
    el('div', { class: 'flex items-center gap-1' }, [searchInput, sortSel, dirBtn]),
    breadcrumb,
    listBox,
    statusEl,
  ])

  /** 授权根下拉同步（修复①：不再只认 accessiblePaths[0]） */
  function syncRootOptions(): void {
    rootSel.innerHTML = ''
    if (!roots.length) {
      rootSel.append(el('option', { value: '' }, ['无授权目录']))
      rootSel.disabled = true
      return
    }
    rootSel.disabled = false
    for (const r of roots) rootSel.append(el('option', { value: r }, [shortRoot(r)]))
    rootSel.value = roots.includes(activeRoot) ? activeRoot : roots[0]
  }

  /** 目录所属授权根（最长前缀匹配，大小写不敏感） */
  function rootForPath(dir: string): string {
    const d = normPath(dir).toLowerCase()
    let best = ''
    for (const r of roots) {
      const rl = r.toLowerCase()
      if (d === rl || d.startsWith(rl + '/')) {
        if (r.length > best.length) best = r
      }
    }
    return best
  }

  function shortRoot(r: string): string {
    const seg = normPath(r).split('/').filter(Boolean)
    return seg.length ? seg[seg.length - 1] : r
  }

  /** 切换授权根（root 必为授权列表项）；rescan=true 时清空列表重新扫描 */
  function setActiveRoot(next: string, rescan: boolean): void {
    if (!next) return
    activeRoot = next
    syncRootOptions()
    opts.onRootChange?.(next)
    if (rescan) startScan(next)
  }

  // ===== 扫描 =====
  function startScan(dir: string) {
    scan?.cancel()
    // 目录可能属于其它授权根（手填/浏览）→ 同步切换根，保证相对路径基准正确
    const owner = rootForPath(dir)
    if (owner && owner !== activeRoot) setActiveRoot(owner, false)
    // 修复②：新扫描先清空（同一批次内改为累加，不再逐批覆盖）
    assets = []
    dirs = []
    pruneMeta()
    renderList()
    setStatus('扫描中…')
    const task = api.scanDirectoryStream(
      dir,
      (videos) => {
        if (destroyed) return
        appendAssets(videos, dir)
      },
      (err) => {
        if (destroyed) return
        renderError(err)
      },
      recursive
    )
    scan = task
    task.promise
      .then(() => {
        if (destroyed) return
        setStatus(`共 ${assets.length} 个视频`)
      })
      .catch((e: unknown) => {
        if (destroyed) return
        renderError(e instanceof Error ? e.message : String(e))
      })
  }

  /** 扫描结果 → AssetRef（稳定 assetId + 代理状态回填） */
  function toRef(v: VideoInfo): AssetRef {
    const ref = toAssetRef(v, activeRoot)
    let id = idByFile.get(ref.file)
    if (!id) {
      id = ref.assetId
      idByFile.set(ref.file, id)
    }
    ref.assetId = id
    const p = proxyByFile.get(ref.file)
    if (p) {
      ref.proxyState = p.state
      ref.proxyFile = p.proxyFile
      ref.proxyDurationMs = p.durationMs
    }
    return ref
  }

  /** 修复②：增量推批累加 + 按 file 去重（后端 handlers.go 每批 batchSize=10，逐批下发不同素材） */
  function appendAssets(videos: VideoInfo[], dir: string) {
    currentDir = dir
    const seen = new Set(assets.map((a) => a.file))
    for (const v of videos) {
      const ref = toRef(v)
      meta.set(ref.file, v)
      if (seen.has(ref.file)) {
        const i = assets.findIndex((a) => a.file === ref.file)
        if (i >= 0) assets[i] = ref
        continue
      }
      seen.add(ref.file)
      assets.push(ref)
      // 需求3：非递归扫描时按视频相对路径推导其所在子目录，供文件夹式阅览
      if (!recursive) ensureSubDir(v.path, dir)
    }
    renderBreadcrumb(dir)
    renderList()
    setStatus(`扫描中… ${assets.length} 个视频`)
  }

  /** 需求3：把视频文件所在的一级子目录登记到 dirs（去重；保留目录 _xxx 不展示） */
  function ensureSubDir(filePath: string, scanDir: string): void {
    const fp = normPath(filePath)
    const sd = normPath(scanDir)
    if (!fp.startsWith(sd + '/')) return
    const rel = fp.slice(sd.length + 1)
    const segs = rel.split('/')
    if (segs.length < 2) return
    const name = segs[0]
    if (name.startsWith('_')) return
    const dpath = sd + '/' + name
    if (!dirs.some((d) => d.path === dpath)) dirs.push({ name, path: dpath })
  }

  /** 修复⑥：清理已不在列表中的展示缓存（原实现按 assetId 建 key，每次扫描都会增长泄漏） */
  function pruneMeta() {
    const keep = new Set(assets.map((a) => a.file))
    for (const k of [...meta.keys()]) if (!keep.has(k)) meta.delete(k)
  }

  function renderError(msg: string) {
    const hint = /授权|authorized|permission|denied/i.test(msg)
      ? '该目录不在授权访问范围内'
      : '扫描失败：' + msg
    listBox.innerHTML = ''
    listBox.append(el('div', { class: 'p-3 text-xs text-danger' }, [hint]))
    setStatus('异常')
  }

  function setStatus(t: string) {
    statusEl.textContent = t
  }

  function renderBreadcrumb(dir: string) {
    breadcrumb.innerHTML = ''
    const normalized = dir.replace(/\\/g, '/')
    const rootNorm = normPath(activeRoot)
    const isUnderRoot = !!rootNorm && normalized.toLowerCase().startsWith(rootNorm.toLowerCase())
    breadcrumb.append(el('span', { class: 'shrink-0' }, ['位置：']))
    if (isUnderRoot) {
      const rel = normalized.slice(rootNorm.length).replace(/^\/+/, '')
      const parts = rel ? rel.split('/') : []
      // 2026-09-20 UI 精简：「根」返回入口由授权目录下拉承担，面包屑不再渲染根段
      if (!parts.length) {
        breadcrumb.append(el('span', { class: 'shrink-0 truncate max-w-[140px]' }, ['根']))
        return
      }
      const segs: { label: string; path: string }[] = []
      let acc = rootNorm
      for (const p of parts) {
        acc = acc + '/' + p
        segs.push({ label: p, path: acc })
      }
      segs.forEach((s, i) => {
        if (i) breadcrumb.append(el('span', { class: 'shrink-0' }, ['/']))
        const a = el('button', { class: 'hover:text-signal truncate max-w-[140px]' }, [s.label])
        a.onclick = () => startScan(s.path)
        breadcrumb.append(a)
      })
    } else {
      breadcrumb.append(el('span', { class: 'truncate' }, [normalized]))
    }
  }

  function visibleAssets(): AssetRef[] {
    const filtered = keyword
      ? assets.filter((a) => fileNameOf(a).toLowerCase().includes(keyword))
      : assets.slice()
    const byName = (a: AssetRef) => fileNameOf(a).toLowerCase()
    filtered.sort((a, b) => {
      let cmp: number
      if (sortKey === 'duration') {
        cmp = a.durationMs - b.durationMs || byName(a).localeCompare(byName(b))
      } else if (sortKey === 'size') {
        cmp = (meta.get(a.file)?.size || 0) - (meta.get(b.file)?.size || 0)
      } else {
        cmp = byName(a).localeCompare(byName(b))
      }
      // 需求2：正序/倒序
      return cmp * sortDir
    })
    return filtered
  }

  function fileNameOf(a: AssetRef): string {
    return a.file.split('/').pop() || a.file
  }

  // ===== 列表 =====
  function renderList() {
    listBox.innerHTML = ''
    if (!assets.length && !dirs.length) {
      listBox.append(
        el('div', { class: 'p-4 text-xs text-ink-muted text-center' }, ['该目录下未找到视频文件'])
      )
      return
    }
    // 需求3：文件夹式阅览——非授权根时提供「上级」返回入口
    const isAtRoot =
      !activeRoot || normPath(currentDir).toLowerCase() === normPath(activeRoot).toLowerCase()
    if (!isAtRoot) {
      const parent = normPath(currentDir).split('/').slice(0, -1).join('/')
      const up = el('div', { class: 'flex items-center gap-2 px-2 py-1.5 cursor-pointer hover:bg-elevated' }, [
        el('span', { class: 'shrink-0 text-xs px-1 rounded bg-elevated' }, ['上级']),
        el('span', { class: 'truncate text-xs text-ink-muted' }, [parent]),
      ])
      up.onclick = () => startScan(parent)
      listBox.append(up)
    }
    // 需求3：子目录行（点击进入；空目录因扫描接口不返回目录列表而无法展示，为已知限制）
    const sortedDirs = dirs.slice().sort((a, b) => a.name.localeCompare(b.name))
    for (const d of sortedDirs) {
      const row = el('div', { class: 'flex items-center gap-2 px-2 py-1.5 cursor-pointer hover:bg-elevated' }, [
        el('span', { class: 'shrink-0 text-xs px-1 rounded bg-primary/15 text-primary' }, ['目录']),
        el('span', { class: 'truncate text-xs text-ink', title: d.path }, [d.name]),
      ])
      row.onclick = () => startScan(d.path)
      listBox.append(row)
    }
    if (!assets.length) return
    const rows = visibleAssets()
    if (!rows.length) {
      listBox.append(el('div', { class: 'p-4 text-xs text-ink-muted text-center' }, ['无匹配文件']))
      return
    }
    for (const a of rows) listBox.append(renderRow(a))
  }

  /** 代理状态角标（修复③：queued/ready/invalid 均有可感知状态） */
  function proxyBadge(state: ProxyState): HTMLElement | null {
    if (state === 'queued')
      return el('span', { class: 'shrink-0 text-xs px-1 rounded bg-primary/15 text-primary' }, ['代理生成中…'])
    if (state === 'ready')
      return el('span', { class: 'shrink-0 text-xs px-1 rounded bg-success/15 text-success' }, ['代理就绪'])
    if (state === 'invalid')
      return el('span', { class: 'shrink-0 text-xs px-1 rounded bg-danger/15 text-danger' }, ['代理失败'])
    return null
  }

  function renderRow(a: AssetRef): HTMLElement {
    const name = fileNameOf(a)
    const st: ProxyState = a.proxyState || 'none'
    const useProxy = st === 'ready' && !!a.proxyFile

    const thumb = el('img', {
      class: 'w-10 h-6 rounded object-cover bg-elevated shrink-0',
      loading: 'lazy',
      alt: name,
    }) as HTMLImageElement
    // 列表内素材：t = min(1000, durationMs/10)（04 §2.4）；代理就绪后取代理帧（体积小、拖动更顺）
    thumb.src = api.thumbUrl(
      useProxy ? (a.proxyFile as string) : a.file,
      Math.min(1000, Math.round(a.durationMs / 10)),
      useProxy ? 'proxy' : 'src'
    )
    thumb.onerror = () => {
      thumb.removeAttribute('src')
      thumb.classList.add('opacity-40')
    }

    const badges: HTMLElement[] = []
    if (st === 'none' && a.visibility === 'need_proxy') {
      badges.push(el('span', { class: 'shrink-0 text-xs px-1 rounded bg-warning/15 text-warning' }, ['需代理']))
    }
    const pb = proxyBadge(st)
    if (pb) badges.push(pb)

    const info = el('div', { class: 'min-w-0 flex-1' }, [
      el('div', { class: 'truncate text-xs text-ink', title: a.file }, [name]),
      el('div', { class: 'text-xs text-ink-muted truncate' }, [
        `${formatMs(a.durationMs)} · ${a.width}×${a.height} · ${a.videoCodec || '未知'}`,
      ]),
    ])

    const row = el(
      'div',
      {
        class: 'flex items-center gap-2 px-2 py-1.5 cursor-grab hover:bg-elevated',
        draggable: 'true',
        title: '双击添加到时间线 / 拖拽到时间线',
      },
      [thumb, info, ...badges]
    )

    // 动作按钮随状态变化（修复③：提交后按钮不再无反馈）
    const actionLabel =
      st === 'queued' ? '生成中…' : st === 'ready' ? '重建代理' : st === 'invalid' ? '重新生成' : '生成代理'
    const needAction = st === 'queued' || st === 'ready' || st === 'invalid' || a.visibility === 'need_proxy'
    if (needAction) {
      const proxyBtn = el('button', {
        class: 'btn shrink-0 text-xs px-1.5 py-0.5' + (st === 'queued' ? ' opacity-50' : ''),
      }, [actionLabel]) as HTMLButtonElement
      if (st === 'queued') proxyBtn.setAttribute('disabled', 'true')
      proxyBtn.onclick = (e) => {
        e.stopPropagation()
        void requestProxy(a)
      }
      row.append(proxyBtn)
    }

    row.onclick = () => opts.onPick?.(a)
    row.ondblclick = () => addWhole(a)
    row.ondragstart = (e) => {
      const dt = (e as DragEvent).dataTransfer
      if (!dt) return
      dt.effectAllowed = 'copy'
      dt.setData('application/x-wve-asset', JSON.stringify(a))
      dt.setData('text/plain', a.file)
    }
    return row
  }

  /** 双击 = 添加整段（01 §4.2） */
  function addWhole(a: AssetRef) {
    if (!a.durationMs) {
      toast('该素材时长未知，暂不能添加', 'error')
      return
    }
    opts.onAddWhole(a)
  }

  // ===== 代理（C-09 / 04 §3.2、§3.5）=====
  function setProxyState(file: string, rec: ProxyRecord) {
    proxyByFile.set(file, rec)
    const a = assets.find((x) => x.file === file)
    if (a) {
      a.proxyState = rec.state
      a.proxyFile = rec.proxyFile
      a.proxyDurationMs = rec.durationMs
    }
    renderList()
  }

  /** 修复③/⑤：提交代理任务并落地 queued 状态；deduplicated 命中活跃任务时提示去重 */
  async function requestProxy(a: AssetRef): Promise<void> {
    const cur = a.proxyState || 'none'
    if (cur === 'queued') {
      toast('该素材的代理任务正在生成中', 'info')
      return
    }
    setProxyState(a.file, { state: 'queued' })
    try {
      const r = await api.createProxy({ assetId: a.assetId, file: a.file })
      if (r.deduplicated) {
        toast('该素材已有代理任务在处理，已复用既有任务', 'info')
      } else {
        toast('代理任务已提交，完成后自动切到代理预览', 'success')
      }
      // 服务端可能返回既有任务（含已就绪场景）→ 记下代理文件名，待 WS proxy_ready 落地为 ready
      if (r.proxyFile) setProxyState(a.file, { state: 'queued', proxyFile: r.proxyFile })
      opts.onRequestProxy?.(a)
    } catch (e) {
      setProxyState(a.file, { state: 'invalid' })
      toast('代理任务提交失败：' + (e instanceof Error ? e.message : String(e)), 'error')
    }
  }

  /** 修复③：消费 WS proxy_ready（04 §3.5 完成广播），刷新角标/缩略图并提示 */
  function handleProxyReady(p: ProxyReadyInfo): void {
    if (destroyed) return
    const file = fileOfProxyEvent(p)
    if (!file) return
    setProxyState(file, { state: 'ready', proxyFile: p.proxyFile, durationMs: p.durationMs })
    const name = file.split('/').pop() || file
    toast(`代理已就绪：${name}`, 'success')
  }

  /** assetId 优先匹配；缺失时按 proxyFile ↔ 源文件互逆换算兜底（修复③：不再依赖单次会话 assetId） */
  function fileOfProxyEvent(p: ProxyReadyInfo): string {
    if (p.assetId) {
      for (const [file, id] of idByFile) if (id === p.assetId) return file
    }
    return p.proxyFile ? fileFromProxyRel(p.proxyFile) : ''
  }

  // 素材数超上限时提前提示（EDL_LIMITS 与后端 handlers_edl.go 同源）
  const unsubClips = edlStore.subscribe((s, changed) => {
    if (!changed.includes('clips')) return
    if (s.clips.length >= EDL_LIMITS.maxClips) setStatus(`已达上限 ${EDL_LIMITS.maxClips} 个片段`)
  })
  // 修复⑥：WS 订阅取消（原实现未取消，销毁后闭包泄漏）
  const unsubProxyReady = store.onProxyReady(handleProxyReady)

  if (currentDir) startScan(currentDir)
  else {
    renderList()
    setStatus('未配置授权目录，请先在「设置」中配置可访问目录')
  }

  return {
    root,
    list: () => assets,
    currentRoot: () => activeRoot,
    setRoots(next: string[]): void {
      // 修复⑥：sourceRoot 随设置刷新（Settings 保存后由上层调用）
      const cleaned = normalizeRoots(next)
      if (!cleaned.length) return
      const changed = cleaned.join('\u0000') !== roots.join('\u0000')
      roots = cleaned
      if (!roots.includes(activeRoot)) {
        activeRoot = roots[0]
        syncRootOptions()
        if (changed) startScan(activeRoot)
      } else {
        syncRootOptions()
      }
    },
    refresh: (dir?: string) => startScan(dir || currentDir),
    destroy() {
      destroyed = true
      scan?.cancel()
      unsubClips()
      unsubProxyReady()
    },
  }
}
