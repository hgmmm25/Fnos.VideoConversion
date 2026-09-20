import { store } from '../store'
import { api } from '../api'
import { el, toast, formatSize, formatDuration, emptyState, svgIcon, skeletonRows, setupDialogAccessibility, showBatchResult } from '../ui'
import { type VideoInfo, type StreamInfo } from '../types'
import { useListPage } from '../lib/useListPage'

// 格式化码率：ffprobe 返回 bps 字符串，转换为人类可读
function formatBitrate(bps: string | number): string {
  const n = typeof bps === 'string' ? parseInt(bps, 10) : bps
  if (!n || isNaN(n) || n <= 0) return '-'
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + ' Mbps'
  if (n >= 1_000) return (n / 1_000).toFixed(0) + ' kbps'
  return n + ' bps'
}

// 格式化 FPS：将 "25/1" "24000/1001" 等分数转换为小数
function formatFps(fps: string): string {
  if (!fps) return '-'
  if (fps.includes('/')) {
    const [num, den] = fps.split('/')
    const n = parseFloat(num)
    const d = parseFloat(den)
    if (d && !isNaN(n) && !isNaN(d)) {
      const v = n / d
      return v >= 100 ? v.toFixed(0) : v.toFixed(2).replace(/\.?0+$/, '')
    }
  }
  const v = parseFloat(fps)
  if (!isNaN(v)) return v >= 100 ? v.toFixed(0) : String(v)
  return fps
}

// 格式化采样率：将 "44100" 转换为 "44.1 kHz"
function formatSampleRate(sr: string | number): string {
  const n = typeof sr === 'string' ? parseInt(sr, 10) : sr
  if (!n || isNaN(n) || n <= 0) return '-'
  if (n >= 1000) return (n / 1000).toFixed(n % 1000 === 0 ? 0 : 1) + ' kHz'
  return n + ' Hz'
}

// 格式化声道数
function formatChannels(ch: number): string {
  if (!ch || ch <= 0) return '-'
  const names: Record<number, string> = { 1: '单声道', 2: '立体声', 6: '5.1', 8: '7.1' }
  return names[ch] || String(ch) + ' 声道'
}

const selectedVideos = new Set<string>()
let scannedVideos: VideoInfo[] = []
let lastScanRoot = ''

// 扫描代数：每次新扫描递增，用于忽略过期扫描的回调
let scanGeneration = 0
let currentScanCancel: (() => void) | null = null

// 列可见性
type ColKey = 'fileName' | 'filePath' | 'format' | 'size' | 'duration' | 'resolution' | 'codec' | 'bitrate' | 'fps' | 'audioCodec' | 'audioBitrate' | 'sampleRate' | 'channels' | 'streamCount'
const colLabels: Record<ColKey, string> = {
  fileName: '文件名',
  filePath: '文件路径',
  format: '格式',
  size: '大小',
  duration: '时长',
  resolution: '分辨率',
  codec: '视频编码',
  bitrate: '视频码率',
  fps: '帧率',
  audioCodec: '音频编码',
  audioBitrate: '音频码率',
  sampleRate: '采样率',
  channels: '声道数',
  streamCount: '流数量',
}
const defaultVisibleCols = ['fileName', 'size', 'duration', 'resolution', 'codec', 'audioCodec'] as ColKey[]
const savedCols = localStorage.getItem('fvcc_visible_cols')
const visibleCols: Set<ColKey> = new Set(savedCols ? JSON.parse(savedCols) : defaultVisibleCols)

// C-阶段（7.3 scanner 行）：文件类型/大小分布 —— 简单占比条，CSS 自绘零依赖，颜色走 token 色板
const DIST_FMT_COLORS = ['bg-primary', 'bg-signal', 'bg-success', 'bg-warning', 'bg-neutral']
const SIZE_BUCKETS: { label: string; test: (s: number) => boolean; cls: string }[] = [
  { label: '<100MB', test: (s) => s < 100 * 1024 * 1024, cls: 'bg-primary/50' },
  { label: '100MB-1GB', test: (s) => s < 1024 * 1024 * 1024, cls: 'bg-primary/70' },
  { label: '1-4GB', test: (s) => s < 4 * 1024 * 1024 * 1024, cls: 'bg-signal/70' },
  { label: '>4GB', test: () => true, cls: 'bg-signal' },
]

export function renderScanner(container: HTMLElement) {
  const wrap = el('div', { class: 'flex flex-col h-full' })

  let scanRoot = lastScanRoot

  const toolbar = el('div', {
    class: 'flex flex-wrap items-center justify-between gap-2 px-3 sm:px-4 py-2 border-b border-line bg-surface',
  })
  const pathDisplay = el('div', { class: 'text-xs text-ink-muted truncate max-w-[200px] sm:max-w-md' }, [''])
  const refreshBtn = el('button', { class: 'btn btn-sm flex items-center gap-1.5 opacity-0 pointer-events-none transition-opacity bg-transparent hover:bg-surface-hover text-ink-muted hover:text-primary' }, [])
  refreshBtn.append(svgIcon('refresh', 16) as unknown as Node)
  const browseBtn = el('button', { class: 'btn btn-primary flex items-center gap-1.5' }, [])
  browseBtn.append(svgIcon('folder-open', 16) as unknown as Node, el('span', {}, ['浏览目录']))
  const taskBtn = el('button', { class: 'btn btn-primary flex items-center gap-1.5', disabled: 'true' }, [])
  taskBtn.append(svgIcon('plus', 16) as unknown as Node, el('span', {}, ['创建转码任务']))
  const trashBtn = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
  trashBtn.append(svgIcon('trash', 16) as unknown as Node, el('span', {}, ['回收站']))
  trashBtn.onclick = () => openTrashManager()

  // 文件清单搜索框
  const searchInput = el('input', {
    type: 'text',
    placeholder: '搜索文件...',
    class: 'input h-8 w-40 text-sm',
  }) as HTMLInputElement
  let searchQuery = ''
  searchInput.oninput = () => {
    searchQuery = searchInput.value.trim().toLowerCase()
    currentPage = 1
    render()
  }

  // 列选择按钮
  const colBtn = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
  colBtn.append(svgIcon('list', 16) as unknown as Node)

  const colDropdown = el('div', { class: 'absolute z-50 mt-1 right-0 card p-3 shadow-lg min-w-[160px] hidden' })
  const colLabel = el('div', { class: 'text-xs font-medium mb-2 text-ink-muted' }, ['显示列'])
  colDropdown.appendChild(colLabel)
  const colKeys = Object.keys(colLabels) as ColKey[]
  for (const key of colKeys) {
    const cb = el('input', { type: 'checkbox', class: 'rounded mr-1.5' }) as HTMLInputElement
    cb.checked = visibleCols.has(key)
    cb.onchange = () => {
      if (cb.checked) visibleCols.add(key)
      else if (visibleCols.size > 1) visibleCols.delete(key)
      else { cb.checked = true; return }
      localStorage.setItem('fvcc_visible_cols', JSON.stringify([...visibleCols]))
      render()
    }
    colDropdown.appendChild(el('label', { class: 'flex items-center gap-1 text-sm py-1 cursor-pointer' }, [cb, el('span', {}, [colLabels[key]])]))
  }
  const colWrap = el('div', { class: 'relative' }, [colBtn, colDropdown])
  colBtn.onclick = (e) => {
    e.stopPropagation()
    colDropdown.classList.toggle('hidden')
  }
  document.addEventListener('click', function closeCol(e) {
    if (!colWrap.contains(e.target as Node)) colDropdown.classList.add('hidden')
  })

  const leftGroup = el('div', { class: 'flex items-center gap-2' }, [browseBtn, pathDisplay, refreshBtn])
  const rightGroup = el('div', { class: 'flex items-center gap-2' }, [searchInput, trashBtn, taskBtn, colWrap])
  toolbar.append(leftGroup, rightGroup)

  // 列表
  const tableWrap = el('div', { class: 'flex-1 overflow-auto' })

  // D-阶段（7.4 阶段 D）：大列表分页 —— 禁止整表全量重建（§7.5），与 history 分页一致
  const PAGE_SIZE = 100
  let currentPage = 1

  // 统计栏（放到最下面）
  const statBar = el('div', {
    class: 'flex items-center justify-between gap-4 px-4 py-1.5 text-xs text-ink-muted bg-surface-alt border-t border-line',
  })
  const statTotal = el('span', {}, ['共 0 个文件'])
  const statSelected = el('span', {}, ['已选 0 个'])
  // D-阶段：分页控件（左：统计 / 右：分页 + 详情）
  const paginationWrap = el('div', { class: 'flex items-center gap-1' })
  const prevBtn = el('button', { class: 'btn btn-sm', disabled: 'true' }, ['上一页'])
  const pageInfo = el('span', { class: 'font-mono tabular-nums whitespace-nowrap' }, ['1 / 1'])
  const nextBtn = el('button', { class: 'btn btn-sm', disabled: 'true' }, ['下一页'])
  const updatePagination = (total: number) => {
    const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
    if (currentPage > totalPages) currentPage = totalPages
    if (currentPage < 1) currentPage = 1
    pageInfo.textContent = `${currentPage} / ${totalPages}`
    prevBtn.disabled = currentPage <= 1
    nextBtn.disabled = currentPage >= totalPages
  }
  prevBtn.onclick = () => {
    if (currentPage > 1) {
      currentPage--
      render()
    }
  }
  nextBtn.onclick = () => {
    const totalPages = Math.max(1, Math.ceil((searchQuery ? scannedVideos.filter((v) => v.fileName.toLowerCase().includes(searchQuery) || v.path.toLowerCase().includes(searchQuery)).length : scannedVideos.length) / PAGE_SIZE))
    if (currentPage < totalPages) {
      currentPage++
      render()
    }
  }
  paginationWrap.append(prevBtn, pageInfo, nextBtn)
  const detailFileName = el('span', { class: 'text-xs font-medium text-primary truncate max-w-[200px] hidden', title: '' }, [''])
  
  const detailToggle = el('button', { 
    class: 'text-xs text-primary hover:text-primary-hover flex items-center gap-1 transition-colors',
    title: '展开文件详情'
  }, [el('span', {}, ['文件详情']), el('span', {}, ['▼'])])
  
  statBar.append(
    el('div', { class: 'flex items-center gap-4' }, [statTotal, statSelected]),
    el('div', { class: 'flex items-center gap-2' }, [paginationWrap, detailFileName, detailToggle])
  )

  // C-阶段（7.3 scanner 行）：格式/大小占比条（挂 toolbar 与列表之间，随扫描结果更新）
  const distBar = el('div', { class: 'hidden bg-surface-alt border-b border-line px-4 py-2 text-xs text-ink-muted space-y-1.5' })

  const detailPanel = el('div', { 
    class: 'hidden bg-surface border-t border-line overflow-auto max-h-[300px]' 
  })

  // 右键菜单
  const contextMenu = el('div', { 
    class: 'fixed z-50 hidden card p-1 shadow-lg min-w-[160px]' 
  })
  document.body.appendChild(contextMenu)
  
  const closeContextMenu = () => {
    contextMenu.classList.add('hidden')
  }
  
  document.addEventListener('click', closeContextMenu)
  document.addEventListener('contextmenu', closeContextMenu)

  const createContextMenu = (video: VideoInfo) => {
    contextMenu.innerHTML = ''
    
    const divider = { type: 'divider' as const }
    const menuItems: Array<{ type: 'divider' } | { label: string; action: () => void; danger?: boolean }> = [
      { label: '转码', action: () => handleTranscode(video) },
      { label: '刷新', action: () => handleRefresh() },
      divider,
      { label: '重命名', action: () => handleRename(video) },
      { label: '移动到', action: () => handleMove(video) },
      divider,
      { label: '删除', action: () => handleDelete(video), danger: true },
    ]
    
    for (const item of menuItems) {
      if ('type' in item && item.type === 'divider') {
        contextMenu.appendChild(el('div', { class: 'border-t border-line my-1' }))
      } else {
        const menuItem = item as { label: string; action: () => void; danger?: boolean }
        const btn = el('button', { 
          class: `w-full text-left px-3 py-1.5 text-sm hover:bg-surface-hover transition-colors ${menuItem.danger ? 'text-danger' : ''}`,
          style: 'background: none; border: none; cursor: pointer; outline: none;'
        }, [menuItem.label])
        btn.onclick = (e) => {
          e.stopPropagation()
          closeContextMenu()
          menuItem.action()
        }
        contextMenu.appendChild(btn)
      }
    }
  }
  
  // 转码功能：如果有选中的行直接调用创建转码任务，否则选中当前行再调用
  const handleTranscode = (video: VideoInfo) => {
    // 如果没有选中的行，选中当前行
    if (selectedVideos.size === 0) {
      selectedVideos.add(video.path)
    }
    // 调用创建转码任务
    openCreateTaskModal(scanRoot)
  }
  
  // 刷新功能：重新扫描当前目录
  const handleRefresh = () => {
    if (!scanRoot) return
    doScan(scanRoot, refreshBtn, () => { render(); updateStats() })
  }
  
  const handleRename = async (video: VideoInfo) => {
    // 记录重命名前的复选框状态和旧路径
    const oldPath = video.path
    const wasSelected = selectedVideos.has(oldPath)

    const overlay = el('div', { class: 'fixed inset-0 bg-black/40 z-50 flex items-center justify-center p-3' })
    const modal = el('div', { class: 'card w-full max-w-sm p-5' })
    const title = el('h3', { class: 'font-semibold text-base mb-3' }, ['重命名文件'])
    const label = el('label', { class: 'block text-sm mb-1' }, ['新文件名'])
    const input = el('input', {
      type: 'text',
      class: 'input mb-3',
      value: video.fileName,
      placeholder: '输入新文件名'
    }) as HTMLInputElement

    const btns = el('div', { class: 'flex justify-end gap-2' })
    const cancel = el('button', { class: 'btn' }, ['取消'])
    const ok = el('button', { class: 'btn btn-primary' }, ['确定'])

    cancel.onclick = () => overlay.remove()
    ok.onclick = async () => {
      const newName = input.value.trim()
      if (!newName) {
        toast('请输入新文件名')
        return
      }
      ok.disabled = true
      try {
        const r = await api.renameVideo(oldPath, newName)
        const videoIndex = scannedVideos.findIndex(v => v.path === oldPath)
        if (videoIndex !== -1) {
          scannedVideos[videoIndex].fileName = newName
          scannedVideos[videoIndex].path = r.newPath
        }
        // 恢复重命名前的复选框状态：先删除旧路径，再添加新路径
        // 注意：必须使用 oldPath 删除，因为 video.path 此时已被修改为新路径
        if (wasSelected) {
          selectedVideos.delete(oldPath)
          selectedVideos.add(r.newPath)
        } else {
          selectedVideos.delete(r.newPath)
        }
        toast('重命名成功', 'success')
        render()
        overlay.remove()
      } catch (e) {
        toast((e as Error).message, 'error')
        ok.disabled = false
      }
    }

    btns.append(cancel, ok)
    modal.append(title, label, input, btns)
    overlay.appendChild(modal)
    overlay.onclick = (e) => { if (e.target === overlay) overlay.remove() }
    document.body.appendChild(overlay)
    // P2-2：重命名弹窗补对话框语义（role=dialog / aria-modal / Esc / 焦点陷阱）
    setupDialogAccessibility({ overlay, titleEl: title, onClose: () => overlay.remove() })
    input.focus()
  }
  
  const handleMove = async (video: VideoInfo) => {
    // 获取要移动的文件列表：如果有选中的行，移动选中的行；否则移动当前行
    const filesToMove = selectedVideos.size > 0 
      ? [...selectedVideos]
      : [video.path]
    
    openDirBrowser(async (destDir) => {
      try {
        let movedCount = 0
        let outCount = 0
        for (const path of filesToMove) {
          const r = await api.moveVideo(path, destDir)
          const isInScanRoot = r.newPath.startsWith(scanRoot)
          if (isInScanRoot) {
            const videoIndex = scannedVideos.findIndex(v => v.path === path)
            if (videoIndex !== -1) {
              scannedVideos[videoIndex].path = r.newPath
            }
            // 如果原来的路径被选中，更新选中状态为新路径
            if (selectedVideos.has(path)) {
              selectedVideos.delete(path)
              selectedVideos.add(r.newPath)
            }
            movedCount++
          } else {
            scannedVideos = scannedVideos.filter(v => v.path !== path)
            selectedVideos.delete(path)
            outCount++
          }
        }
        if (movedCount > 0) {
          toast(`成功移动 ${movedCount} 个文件`, 'success')
        }
        if (outCount > 0) {
          toast(`${outCount} 个文件已移出当前目录`, 'info')
        }
        render()
      } catch (e) {
        toast((e as Error).message, 'error')
      }
    }, scanRoot)
  }
  
  const handleDelete = async (video: VideoInfo) => {
    // 获取要删除的文件列表：如果有选中的行，删除选中的行；否则删除当前行
    const filesToDelete = selectedVideos.size > 0 
      ? [...selectedVideos]
      : [video.path]
    
    const overlay = el('div', { class: 'fixed inset-0 bg-black/40 z-50 flex items-center justify-center p-3' })
    const modal = el('div', { class: 'card w-full max-w-sm p-5' })
    const title = el('h3', { class: 'font-semibold text-base mb-3' }, ['删除文件'])
    let warningText: string
    if (filesToDelete.length === 1) {
      const v = scannedVideos.find(sv => sv.path === filesToDelete[0])
      warningText = `确定要删除文件 "${v?.fileName || filesToDelete[0]}" 吗？`
    } else {
      warningText = `确定要删除选中的 ${filesToDelete.length} 个文件吗？`
    }
    
    const warning = el('div', { class: 'text-sm text-danger mb-4' }, [
      warningText,
      el('br'),
      '删除后文件将移入回收站，可在回收站中恢复。'
    ])
    
    const btns = el('div', { class: 'flex justify-end gap-2' })
    const cancel = el('button', { class: 'btn' }, ['取消'])
    const ok = el('button', { class: 'btn btn-danger' }, ['确定'])
    
    cancel.onclick = () => overlay.remove()
    ok.onclick = async () => {
      ok.disabled = true
      try {
        let deletedCount = 0
        for (const path of filesToDelete) {
          try {
            await api.deleteVideo(path)
            scannedVideos = scannedVideos.filter(v => v.path !== path)
            selectedVideos.delete(path)
            if (selectedDetailPath === path) {
              selectedDetailPath = null
            }
            deletedCount++
          } catch (e) {
            toast(`删除文件失败: ${(e as Error).message}`, 'error')
          }
        }
        if (deletedCount > 0) {
          toast(`已删除 ${deletedCount} 个文件（移入回收站）`, 'success')
        }
        render()
        overlay.remove()
      } catch (e) {
        toast((e as Error).message, 'error')
        ok.disabled = false
      }
    }
    
    btns.append(cancel, ok)
    modal.append(title, warning, btns)
    overlay.appendChild(modal)
    overlay.onclick = (e) => { if (e.target === overlay) overlay.remove() }
    document.body.appendChild(overlay)
    // P2-2：删除确认补对话框语义（role=dialog / aria-modal / Esc / 焦点陷阱）
    setupDialogAccessibility({ overlay, titleEl: title, onClose: () => overlay.remove() })
    ok.focus()
  }
  
  wrap.append(toolbar, distBar, tableWrap, detailPanel, statBar)
  container.appendChild(wrap)

  let detailExpanded = false
  let selectedDetailPath: string | null = null
  
  const toggleDetail = () => {
    detailExpanded = !detailExpanded
    detailToggle.innerHTML = ''
    detailToggle.append(
      el('span', {}, ['文件详情']), 
      el('span', {}, [detailExpanded ? '▲' : '▼'])
    )
    if (detailExpanded) {
      detailPanel.classList.remove('hidden')
      detailFileName.classList.remove('hidden')
      setTimeout(() => {
        detailPanel.classList.remove('animate-slide-up')
        detailPanel.classList.add('animate-slide-down')
      }, 10)
      renderDetailPanel()
    } else {
      detailPanel.classList.remove('animate-slide-down')
      detailPanel.classList.add('animate-slide-up')
      detailFileName.classList.add('hidden')
      setTimeout(() => {
        detailPanel.classList.add('hidden')
      }, 200)
    }
  }

  detailToggle.onclick = () => {
    toggleDetail()
  }

  const codecNames: Record<string, string> = {
    'h264': 'H.264',
    'h265': 'H.265',
    'hevc': 'H.265 (HEVC)',
    'vp9': 'VP9',
    'vp8': 'VP8',
    'av1': 'AV1',
    'mpeg4': 'MPEG-4',
    'mpeg2video': 'MPEG-2',
    'vc1': 'VC-1',
    'wmv3': 'WMV3',
    'aac': 'AAC',
    'mp3': 'MP3',
    'ac3': 'AC-3',
    'eac3': 'E-AC-3',
    'dts': 'DTS',
    'dtshd': 'DTS-HD',
    'flac': 'FLAC',
    'opus': 'Opus',
    'vorbis': 'Vorbis',
    'pcm_s16le': 'PCM 16位',
    'pcm_s24le': 'PCM 24位',
    'pcm_f32le': 'PCM 浮点',
    'srt': 'SRT',
    'ass': 'ASS',
    'ssa': 'SSA',
    'subrip': 'SRT',
    'mov_text': 'MOV 文本',
    'hdmv_pgs_subtitle': 'PGS 蓝光字幕',
    'dvb_subtitle': 'DVB 字幕',
  }

  const languageCodes: Record<string, string> = {
    'chi': '中文',
    'zh': '中文',
    'zh-cn': '中文(简体)',
    'zh-tw': '中文(繁体)',
    'zh-hk': '中文(香港)',
    'eng': '英语',
    'en': '英语',
    'en-us': '英语(美国)',
    'en-gb': '英语(英国)',
    'jpn': '日语',
    'ja': '日语',
    'kor': '韩语',
    'ko': '韩语',
    'fre': '法语',
    'fr': '法语',
    'deu': '德语',
    'de': '德语',
    'spa': '西班牙语',
    'es': '西班牙语',
    'ita': '意大利语',
    'it': '意大利语',
    'rus': '俄语',
    'ru': '俄语',
    'por': '葡萄牙语',
    'pt': '葡萄牙语',
    'nld': '荷兰语',
    'nl': '荷兰语',
    'swe': '瑞典语',
    'sv': '瑞典语',
    'nor': '挪威语',
    'no': '挪威语',
    'dan': '丹麦语',
    'da': '丹麦语',
    'fin': '芬兰语',
    'fi': '芬兰语',
    'pol': '波兰语',
    'pl': '波兰语',
    'hun': '匈牙利语',
    'hu': '匈牙利语',
    'cze': '捷克语',
    'cs': '捷克语',
    'slk': '斯洛伐克语',
    'sk': '斯洛伐克语',
    'srp': '塞尔维亚语',
    'sr': '塞尔维亚语',
    'hrv': '克罗地亚语',
    'hr': '克罗地亚语',
    'bg': '保加利亚语',
    'grc': '希腊语',
    'el': '希腊语',
    'heb': '希伯来语',
    'he': '希伯来语',
    'ara': '阿拉伯语',
    'ar': '阿拉伯语',
    'hin': '印地语',
    'hi': '印地语',
    'tha': '泰语',
    'th': '泰语',
    'vie': '越南语',
    'vi': '越南语',
    'ind': '印尼语',
    'id': '印尼语',
    'msa': '马来语',
    'ms': '马来语',
    'fil': '菲律宾语',
    'tl': '菲律宾语',
    'ukr': '乌克兰语',
    'uk': '乌克兰语',
    'bel': '白俄罗斯语',
    'be': '白俄罗斯语',
    'kaz': '哈萨克语',
    'kk': '哈萨克语',
    'uzb': '乌兹别克语',
    'uz': '乌兹别克语',
    'tur': '土耳其语',
    'tr': '土耳其语',
    'pers': '波斯语',
    'fa': '波斯语',
    'ur': '乌尔都语',
    'pas': '普什图语',
    'afr': '南非语',
    'af': '南非语',
    'amh': '阿姆哈拉语',
  }

  function formatCodec(codecName: string): string {
    return codecNames[codecName.toLowerCase()] || codecName
  }

  function formatLanguage(lang: string): string {
    return languageCodes[lang.toLowerCase()] || lang
  }

  function renderDetailPanel() {
    detailPanel.innerHTML = ''
    if (!selectedDetailPath) {
      detailPanel.appendChild(el('div', { class: 'p-4 text-sm text-ink-muted text-center' }, ['点击列表中的文件查看详情']))
      return
    }
    
    const video = scannedVideos.find(v => v.path === selectedDetailPath)
    if (!video || !video.streams || video.streams.length === 0) {
      detailPanel.appendChild(el('div', { class: 'p-4 text-sm text-ink-muted text-center' }, ['暂无流信息']))
      return
    }
    
    detailFileName.textContent = video.fileName
    detailFileName.title = video.fileName
    
    const detailGrid = el('div', { class: 'p-4' })
    
    const videoStreams = video.streams.filter(s => s.codecType === 'video')
    const audioStreams = video.streams.filter(s => s.codecType === 'audio')
    const subtitleStreams = video.streams.filter(s => s.codecType === 'subtitle')
    const dataStreams = video.streams.filter(s => s.codecType === 'data')
    const attachmentStreams = video.streams.filter(s => s.codecType === 'attachment')
    const otherStreams = video.streams.filter(s => 
      !['video', 'audio', 'subtitle', 'data', 'attachment'].includes(s.codecType)
    )

    const renderStreamCard = (s: StreamInfo, colorClass: string, index: number): HTMLElement => {
      const card = el('div', { 
        class: `card p-3 border-l-4 ${colorClass} animate-fade-in-up`,
        style: `animation-delay: ${index * 50}ms`,
      })
      const head = el('div', { class: 'flex items-center justify-between mb-2' }, [
        el('span', { class: 'text-xs font-medium truncate max-w-[80px]' }, [formatCodec(s.codecName) || '-']),
        el('span', { class: 'text-xs text-ink-muted' }, [`#${s.index}`]),
      ])
      card.appendChild(head)
      
      const info = el('div', { class: 'text-xs text-ink-muted space-y-0.5' })
      
      switch (s.codecType) {
        case 'video':
          if (s.width && s.height) info.appendChild(el('div', {}, [`分辨率: ${s.width}x${s.height}`]))
          if (s.rFrameRate) info.appendChild(el('div', {}, [`帧率: ${formatFps(s.rFrameRate)}`]))
          if (s.bitRate) info.appendChild(el('div', {}, [`码率: ${formatBitrate(s.bitRate)}`]))
          if (s.codecLongName) info.appendChild(el('div', {}, [`编码: ${s.codecLongName}`]))
          break
        case 'audio':
          if (s.codecLongName) info.appendChild(el('div', {}, [`编码: ${s.codecLongName}`]))
          if (s.sampleRate) info.appendChild(el('div', {}, [`采样率: ${formatSampleRate(s.sampleRate)}`]))
          if (s.channels) info.appendChild(el('div', {}, [`声道: ${formatChannels(s.channels)}`]))
          if (s.channelLayout) info.appendChild(el('div', {}, [`声道布局: ${s.channelLayout}`]))
          if (s.bitRate) info.appendChild(el('div', {}, [`码率: ${formatBitrate(s.bitRate)}`]))
          break
        case 'subtitle':
          if (s.codecLongName) info.appendChild(el('div', {}, [`格式: ${s.codecLongName}`]))
          if (s.tags && s.tags.language) info.appendChild(el('div', {}, [`语言: ${formatLanguage(s.tags.language)}`]))
          if (s.tags && s.tags.title) info.appendChild(el('div', {}, [`标题: ${s.tags.title}`]))
          break
        case 'data':
        case 'attachment':
          if (s.codecLongName) info.appendChild(el('div', {}, [`格式: ${s.codecLongName}`]))
          if (s.tags && s.tags.filename) info.appendChild(el('div', {}, [`文件名: ${s.tags.filename}`]))
          if (s.bitRate) info.appendChild(el('div', {}, [`大小: ${formatBitrate(s.bitRate)}`]))
          break
        default:
          if (s.codecLongName) info.appendChild(el('div', {}, [`编码: ${s.codecLongName}`]))
          if (s.bitRate) info.appendChild(el('div', {}, [`码率: ${formatBitrate(s.bitRate)}`]))
          break
      }
      
      card.appendChild(info)
      return card
    }

    const renderStreamSection = (title: string, streams: StreamInfo[], colorClass: string, baseIndex: number): HTMLElement | null => {
      if (streams.length === 0) return null
      const section = el('div', { class: 'mb-4' })
      section.appendChild(el('div', { class: 'text-xs font-medium text-ink-muted mb-2' }, [title]))
      const cards = el('div', { class: 'grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2' })
      for (let i = 0; i < streams.length; i++) {
        cards.appendChild(renderStreamCard(streams[i], colorClass, baseIndex + i))
      }
      section.appendChild(cards)
      return section
    }

    let totalIndex = 0

    if (videoStreams.length > 0 || audioStreams.length > 0) {
      const videoAudioRow = el('div', { class: 'grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4' })
      
      if (videoStreams.length > 0) {
        const videoSection = el('div', { class: '' })
        videoSection.appendChild(el('div', { class: 'text-xs font-medium text-ink-muted mb-2' }, ['视频流']))
        const videoCards = el('div', { class: 'grid grid-cols-1 sm:grid-cols-2 gap-2' })
        for (let i = 0; i < videoStreams.length; i++) {
          videoCards.appendChild(renderStreamCard(videoStreams[i], 'border-l-primary', totalIndex++))
        }
        videoSection.appendChild(videoCards)
        videoAudioRow.appendChild(videoSection)
      }
      
      if (audioStreams.length > 0) {
        const audioSection = el('div', { class: '' })
        audioSection.appendChild(el('div', { class: 'text-xs font-medium text-ink-muted mb-2' }, ['音频流']))
        const audioCards = el('div', { class: 'grid grid-cols-1 sm:grid-cols-2 gap-2' })
        for (let i = 0; i < audioStreams.length; i++) {
          audioCards.appendChild(renderStreamCard(audioStreams[i], 'border-l-success', totalIndex++))
        }
        audioSection.appendChild(audioCards)
        videoAudioRow.appendChild(audioSection)
      }
      
      detailGrid.appendChild(videoAudioRow)
    }

    const subtitleSection = renderStreamSection('字幕流', subtitleStreams, 'border-l-signal', totalIndex)
    totalIndex += subtitleStreams.length
    const dataSection = renderStreamSection('数据流', dataStreams, 'border-l-neutral', totalIndex)
    totalIndex += dataStreams.length
    const attachmentSection = renderStreamSection('附件流', attachmentStreams, 'border-l-warning', totalIndex)
    totalIndex += attachmentStreams.length
    const otherSection = renderStreamSection('其他流', otherStreams, 'border-l-ink-muted', totalIndex)
    
    if (subtitleSection) detailGrid.appendChild(subtitleSection)
    if (dataSection) detailGrid.appendChild(dataSection)
    if (attachmentSection) detailGrid.appendChild(attachmentSection)
    if (otherSection) detailGrid.appendChild(otherSection)

    detailPanel.appendChild(detailGrid)
  }

  taskBtn.onclick = () => openCreateTaskModal(scanRoot)
  browseBtn.onclick = () => openDirBrowser(async (selected) => {
    scanRoot = selected
    lastScanRoot = selected
    pathDisplay.textContent = selected
    refreshBtn.classList.remove('opacity-0', 'pointer-events-none')
    selectedVideos.clear()
    await doScan(selected, browseBtn, () => { render(); updateStats() })
  }, scanRoot)
  refreshBtn.onclick = async () => {
    if (!scanRoot) return
    selectedVideos.clear()
    await doScan(scanRoot, refreshBtn, () => { render(); updateStats() })
  }

  const updateStats = () => {
    statSelected.textContent = `已选 ${selectedVideos.size} 个`
    taskBtn.disabled = selectedVideos.size === 0
  }
  // C-阶段（7.3 scanner 行）：格式/大小占比条渲染（基于扫描全量，不随搜索词变化）
  const updateDist = () => {
    const vs = scannedVideos
    distBar.replaceChildren()
    if (vs.length === 0) {
      distBar.classList.add('hidden')
      return
    }
    distBar.classList.remove('hidden')
    const total = vs.length

    // 格式占比（top5 + 其他）
    const fmtMap = new Map<string, number>()
    for (const v of vs) {
      const f = (v.format || '未知').toLowerCase()
      fmtMap.set(f, (fmtMap.get(f) ?? 0) + 1)
    }
    const fmtArr = [...fmtMap.entries()].sort((a, b) => b[1] - a[1])
    const top = fmtArr.slice(0, 5)
    const restN = fmtArr.slice(5).reduce((a, [, n]) => a + n, 0)
    if (restN > 0) top.push(['其他', restN])
    const fmtRow = el('div', { class: 'flex items-center gap-3' }, [el('span', { class: 'shrink-0 w-10' }, ['格式'])])
    const fmtBar = el('div', { class: 'flex flex-1 min-w-0 h-1.5 rounded-full overflow-hidden bg-surface' })
    const fmtLabels: string[] = []
    top.forEach(([f, n], i) => {
      const seg = el('div', {
        class: `${DIST_FMT_COLORS[i % DIST_FMT_COLORS.length]} h-full transition-all`,
        style: `width:${(n / total) * 100}%`,
        title: `${f} ${n}`,
      })
      fmtBar.appendChild(seg)
      fmtLabels.push(`${f} ${n}`)
    })
    fmtRow.append(fmtBar, el('span', { class: 'shrink-0 font-mono tabular-nums max-w-[40%] truncate' }, [fmtLabels.join(' · ')]))

    // 大小分段占比
    const sizeCounts = SIZE_BUCKETS.map((b) => ({ label: b.label, cls: b.cls, n: 0 }))
    for (const v of vs) {
      const b = SIZE_BUCKETS.find((b) => b.test(v.size))
      if (b) {
        const hit = sizeCounts.find((x) => x.label === b.label)
        if (hit) hit.n++
      }
    }
    const sizeRow = el('div', { class: 'flex items-center gap-3' }, [el('span', { class: 'shrink-0 w-10' }, ['大小'])])
    const sizeBar = el('div', { class: 'flex flex-1 min-w-0 h-1.5 rounded-full overflow-hidden bg-surface' })
    const sizeLabels: string[] = []
    for (const b of sizeCounts) {
      if (b.n === 0) continue
      const seg = el('div', {
        class: `${b.cls} h-full transition-all`,
        style: `width:${(b.n / total) * 100}%`,
        title: `${b.label} ${b.n}`,
      })
      sizeBar.appendChild(seg)
      sizeLabels.push(`${b.label} ${b.n}`)
    }
    sizeRow.append(sizeBar, el('span', { class: 'shrink-0 font-mono tabular-nums max-w-[40%] truncate' }, [sizeLabels.join(' · ')]))

    distBar.append(fmtRow, sizeRow)
  }
  const render = () => {
    tableWrap.innerHTML = ''
    const filtered = searchQuery
      ? scannedVideos.filter((v) => v.fileName.toLowerCase().includes(searchQuery) || v.path.toLowerCase().includes(searchQuery))
      : scannedVideos
    statTotal.textContent = searchQuery
      ? `共 ${filtered.length} / ${scannedVideos.length} 个文件`
      : `共 ${scannedVideos.length} 个文件`
    updateStats()
    updateDist()
    if (scannedVideos.length === 0) {
      // P2-1：空状态承载"下一步动作"
      tableWrap.appendChild(
        emptyState('请点击「浏览目录」选择要扫描的视频目录', 'video', {
          label: '浏览目录',
          onClick: () => {
            browseBtn.click()
          },
        })
      )
      updatePagination(0)
      return
    }
    if (filtered.length === 0) {
      tableWrap.appendChild(emptyState('没有匹配的文件', 'search'))
      updatePagination(0)
      return
    }
    updatePagination(filtered.length)
    // D-阶段（7.4 阶段 D）：大列表分页 —— 排序在全量数据上执行，渲染仅取当前页切片，避免数千行全量重建
    tableWrap.appendChild(buildTable(updateStats, filtered))
  }

  if (lastScanRoot) {
    pathDisplay.textContent = lastScanRoot
    refreshBtn.classList.remove('opacity-0', 'pointer-events-none')
  }

  // 排序状态
  let sortKey: ColKey = 'fileName'
  let sortDir: 'asc' | 'desc' = 'asc'

  function parseFps(fps: string): number {
    if (!fps) return 0
    if (fps.includes('/')) {
      const [num, den] = fps.split('/')
      const n = parseFloat(num)
      const d = parseFloat(den)
      if (d && !isNaN(n) && !isNaN(d)) return n / d
    }
    const v = parseFloat(fps)
    return !isNaN(v) ? v : 0
  }

  function buildTable(updateBtns: () => void, sourceVideos: VideoInfo[]): HTMLElement {
  const sortedAll = [...sourceVideos].sort((a, b) => {
    let av: string | number = ''
    let bv: string | number = ''
    switch (sortKey) {
      case 'fileName': av = a.fileName; bv = b.fileName; break
      case 'filePath': av = a.path; bv = b.path; break
      case 'format': av = a.format; bv = b.format; break
      case 'size': av = a.size; bv = b.size; break
      case 'duration': av = a.probed ? a.duration : 0; bv = b.probed ? b.duration : 0; break
      case 'resolution': av = a.probed ? a.width * a.height : 0; bv = b.probed ? b.width * b.height : 0; break
      case 'codec': av = a.probed ? a.codec : ''; bv = b.probed ? b.codec : ''; break
      case 'bitrate': av = a.probed ? (parseInt(a.bitrate, 10) || 0) : 0; bv = b.probed ? (parseInt(b.bitrate, 10) || 0) : 0; break
      case 'fps': av = a.probed ? parseFps(a.fps) : 0; bv = b.probed ? parseFps(b.fps) : 0; break
      case 'audioCodec': av = a.probed ? a.audioCodec : ''; bv = b.probed ? b.audioCodec : ''; break
      case 'audioBitrate': av = a.probed ? (parseInt(a.audioBitrate, 10) || 0) : 0; bv = b.probed ? (parseInt(b.audioBitrate, 10) || 0) : 0; break
      case 'sampleRate': av = a.probed ? (parseInt(a.sampleRate, 10) || 0) : 0; bv = b.probed ? (parseInt(b.sampleRate, 10) || 0) : 0; break
      case 'channels': av = a.probed ? a.channels : 0; bv = b.probed ? b.channels : 0; break
      case 'streamCount': av = a.probed ? a.streamCount : 0; bv = b.probed ? b.streamCount : 0; break
    }
    if (av < bv) return sortDir === 'asc' ? -1 : 1
    if (av > bv) return sortDir === 'asc' ? 1 : -1
    return 0
  })

  // D-阶段（7.4 阶段 D）：大列表分页 —— 排序在全量 sortedAll 上，渲染仅取当前页切片
  const startIdx = (currentPage - 1) * PAGE_SIZE
  const sorted = sortedAll.slice(startIdx, startIdx + PAGE_SIZE)

  const table = el('table', { class: 'w-full text-sm whitespace-nowrap' })
  const thead = el('thead', { class: 'sticky top-0 bg-surface-alt text-ink-muted' })
  const headRow = el('tr', { class: 'border-b border-line' })

  // 可见列
  const cols = (Object.keys(colLabels) as ColKey[]).filter(k => visibleCols.has(k))

  // 全选复选框
  const allCb = el('input', { type: 'checkbox', class: 'rounded' }) as HTMLInputElement
  allCb.checked = sorted.length > 0 && sorted.every((v) => selectedVideos.has(v.path))
  allCb.indeterminate = !allCb.checked && sorted.some((v) => selectedVideos.has(v.path))
  allCb.onchange = () => {
    if (allCb.checked) {
      for (const v of sorted) selectedVideos.add(v.path)
    } else {
      for (const v of sorted) selectedVideos.delete(v.path)
    }
    updateBtns()
    table.querySelectorAll('input[data-path]').forEach((cb) => {
      (cb as HTMLInputElement).checked = allCb.checked
      const tr = cb.closest('tr')
      if (tr) tr.classList.toggle('bg-primary/5', allCb.checked)
    })
  }
  allCb.onclick = (e) => e.stopPropagation()

  for (const col of cols) {
    const th = el('th', { class: 'text-left px-4 py-2 font-medium' })
    const isActive = sortKey === col
    th.style.cursor = 'pointer'
    th.onclick = () => {
      if (sortKey === col) {
        sortDir = sortDir === 'asc' ? 'desc' : 'asc'
      } else {
        sortKey = col
        sortDir = 'asc'
      }
      render()
    }
    if (col === 'fileName') {
      const wrap = el('div', { class: 'flex items-center gap-2' }, [
        allCb,
        el('span', {}, [colLabels[col]]),
      ])
      if (isActive) {
        wrap.appendChild(el('span', { class: 'text-xs' }, [sortDir === 'asc' ? '▲' : '▼']))
      }
      th.appendChild(wrap)
    } else {
      const children: Node[] = [el('span', {}, [colLabels[col]])]
      if (isActive) {
        children.push(el('span', { class: 'text-xs ml-1' }, [sortDir === 'asc' ? '▲' : '▼']))
      }
      th.append(...children)
    }
    headRow.appendChild(th)
  }
  thead.appendChild(headRow)
  table.appendChild(thead)

  const tbody = el('tbody')
  for (const v of sorted) {
    const tr = el('tr', { class: 'border-b border-line-subtle hover:bg-surface-hover' })
    const selected = selectedVideos.has(v.path)
    if (selected) tr.classList.add('bg-primary/5')

    const cb = el('input', { type: 'checkbox', class: 'rounded' }) as HTMLInputElement
    cb.checked = selected
    cb.dataset.path = v.path
    cb.onchange = () => {
      if (cb.checked) selectedVideos.add(v.path)
      else selectedVideos.delete(v.path)
      tr.classList.toggle('bg-primary/5', cb.checked)
      allCb.checked = sorted.every((x) => selectedVideos.has(x.path))
      allCb.indeterminate = !allCb.checked && sorted.some((x) => selectedVideos.has(x.path))
      updateBtns()
    }
    cb.onclick = (e) => e.stopPropagation()

    tr.onclick = () => {
      selectedDetailPath = v.path
      if (detailExpanded) {
        renderDetailPanel()
      }
    }

    tr.oncontextmenu = (e) => {
      e.preventDefault()
      e.stopPropagation()
      createContextMenu(v)
      contextMenu.style.left = `${e.clientX}px`
      contextMenu.style.top = `${e.clientY}px`
      contextMenu.classList.remove('hidden')
    }

    for (const col of cols) {
      if (col === 'fileName') {
        const nameWrap = el('div', { class: 'flex items-center gap-2' }, [
          cb,
          el('span', { class: 'truncate max-w-[120px] sm:max-w-xs' }, [v.fileName]),
        ])
        tr.appendChild(el('td', { class: 'px-4 py-2' }, [nameWrap]))
      } else {
        let text = '-'
        if (v.probed) {
          switch (col) {
            case 'filePath': text = v.path || '-'; break
            case 'format': text = v.format || '-'; break
            case 'size': text = formatSize(v.size); break
            case 'duration': text = formatDuration(v.duration); break
            case 'resolution': text = v.resolution || '-'; break
            case 'codec': text = v.codec || '-'; break
            case 'bitrate': text = formatBitrate(v.bitrate); break
            case 'fps': text = formatFps(v.fps); break
            case 'audioCodec': text = v.audioCodec || '-'; break
            case 'audioBitrate': text = formatBitrate(v.audioBitrate); break
            case 'sampleRate': text = formatSampleRate(v.sampleRate); break
            case 'channels': text = formatChannels(v.channels); break
            case 'streamCount': text = v.streamCount ? String(v.streamCount) : '-'; break
          }
        } else {
          switch (col) {
            case 'filePath': text = v.path || '-'; break
            case 'format': text = v.format || '-'; break
            case 'size': text = formatSize(v.size); break
          }
        }
        const td = el('td', { class: 'px-4 py-2 whitespace-nowrap' }, [text])
        if (col === 'filePath') td.classList.add('max-w-xs', 'truncate')
        tr.appendChild(td)
      }
    }
    tbody.appendChild(tr)
  }
  table.appendChild(tbody)
  return table
}

async function doScan(path: string, scanBtn: HTMLButtonElement, onDone: () => void) {
  if (!path.trim()) {
    toast('请输入目录路径')
    return
  }

  // 取消前一次扫描（如果有）
  if (currentScanCancel) {
    currentScanCancel()
    currentScanCancel = null
  }

  // 递增代数，用于忽略过期扫描的回调
  const myGen = ++scanGeneration

  const originalChildren = Array.from(scanBtn.children)
  scanBtn.innerHTML = ''
  scanBtn.appendChild(el('span', {}, ['扫描中...']))
  scanBtn.disabled = true
  try {
    scannedVideos = []
    selectedVideos.clear()
    onDone()
    const scan = api.scanDirectoryStream(
      path.trim(),
      (videos) => {
        // 代数不匹配说明已有更新的扫描启动，忽略本次回调
        if (myGen !== scanGeneration) return
        scannedVideos.push(...videos)
        onDone()
      },
      (error) => {
        if (myGen !== scanGeneration) return
        toast(error, 'error')
      },
    )
    currentScanCancel = scan.cancel
    await scan.promise
    if (myGen !== scanGeneration) return
    toast(`扫描完成，共 ${scannedVideos.length} 个视频文件`, 'success')
    onDone()
  } catch (e) {
    if (myGen !== scanGeneration) return
    toast((e as Error).message, 'error')
  } finally {
    if (myGen === scanGeneration) {
      currentScanCancel = null
      scanBtn.innerHTML = ''
      for (const child of originalChildren) {
        scanBtn.appendChild(child)
      }
      scanBtn.disabled = false
    }
  }
}

  // P2-2：扫描列表走 useListPage 统一生命周期（load 幂等返回内存数据，不重复扫描）
  const page = useListPage({
    load: async () => [...scannedVideos],
    render: () => render(),
    subscribe: (cb) => store.subscribe(cb),
    errorLabel: '扫描结果',
  })
  page.mount()
  updateStats()
  return page.dispose
}

// isSMBMode 判断当前是否为SMB传输模式
async function isSMBMode(): Promise<boolean> {
  try {
    const r = await api.getSettings()
    return r.settings.transferMode === 'smb'
  } catch {
    return false
  }
}

export function openDirBrowser(onSelect: (path: string) => void, startPath?: string) {
  const overlay = el('div', { class: 'fixed inset-0 bg-black/40 z-50 flex items-center justify-center p-3' })
  const modal = el('div', { class: 'card w-full max-w-[42rem] max-h-[75vh] flex flex-col shadow-2xl' })
  const titleEl = el('h3', { class: 'font-semibold text-base' }, ['选择目录'])
  const header = el('div', { class: 'flex items-center justify-between mb-3 px-4 pt-4' }, [titleEl])
  const pathBar = el('div', { class: 'text-xs text-ink-muted mb-3 px-4' }, [
    skeletonRows(1, '0.75rem')[0],
  ])
  const listWrap = el('div', { class: 'flex-1 overflow-auto border border-line rounded mb-3 mx-4 min-h-[240px]' })
  const btns = el('div', { class: 'flex justify-end gap-2 px-4 pb-4' })
  const cancelBtn = el('button', { class: 'btn' }, ['取消'])
  const selectBtn = el('button', { class: 'btn btn-primary', disabled: 'true' }, ['选择此目录'])

  btns.append(cancelBtn, selectBtn)
  modal.append(header, pathBar, listWrap, btns)
  overlay.appendChild(modal)
  document.body.appendChild(overlay)

  let selectedPath = ''

  overlay.onclick = (e) => { if (e.target === overlay) overlay.remove() }
  cancelBtn.onclick = () => overlay.remove()
  selectBtn.onclick = () => {
    if (selectedPath) {
      overlay.remove()
      onSelect(selectedPath)
    }
  }
  // P2-2：目录浏览器补对话框语义（role=dialog / aria-modal / Esc / 焦点陷阱 / 返回焦点）
  setupDialogAccessibility({ overlay, titleEl, onClose: () => overlay.remove() })
  cancelBtn.focus()

  async function loadDirs(path: string) {
    selectedPath = path
    pathBar.textContent = path || '授权目录'
    listWrap.innerHTML = ''
    // P2-1：目录列表加载用骨架屏（替代纯文本"加载中..."）
    listWrap.appendChild(
      el('div', { class: 'p-3 space-y-2' }, skeletonRows(6, '2rem'))
    )
    selectBtn.disabled = !path

    try {
      const isSMB = await isSMBMode()
      const r = await api.browseDirs(path || undefined)
      listWrap.innerHTML = ''
      selectedPath = r.current
      pathBar.textContent = r.current || (isSMB ? '共享目录' : '授权目录')
      selectBtn.disabled = !r.current

      const dirs = r.dirs || []
      const files = r.files || []

      if (!r.current) {
        if (dirs.length === 0) {
          listWrap.appendChild(el('div', { class: 'p-4 text-sm text-ink-muted' }, [
            isSMB ? '未找到已共享的授权目录，请检查SMB用户名配置和fnOS共享设置' : '未找到授权目录，请在 fnOS 应用设置中为此应用授权共享文件夹',
          ]))
        } else {
          for (const d of dirs) {
            const row = dirRow(d.name, d.path)
            row.onclick = () => loadDirs(d.path)
            row.ondblclick = () => { overlay.remove(); onSelect(d.path) }
            listWrap.appendChild(row)
          }
        }
        return
      }

      const backLabel = r.parent === '' ? (isSMB ? '返回共享目录' : '返回授权目录') : '..'
      const backRow = el('div', {
        class: 'flex items-center gap-2 px-3 py-2 cursor-pointer hover:bg-surface-hover text-sm text-ink-muted',
      }, [svgIcon('back', 16) as unknown as Node, el('span', {}, [backLabel])])
      if (r.parent !== undefined) {
        backRow.onclick = () => loadDirs(r.parent!)
      } else {
        backRow.style.opacity = '0.4'
        backRow.style.cursor = 'default'
      }
      listWrap.appendChild(backRow)

      for (const d of dirs) {
        const row = dirRow(d.name, d.path)
        row.onclick = () => loadDirs(d.path)
        row.ondblclick = () => { overlay.remove(); onSelect(d.path) }
        listWrap.appendChild(row)
      }

      if (files.length > 0) {
        listWrap.appendChild(el('div', {
          class: 'px-3 py-1.5 text-xs text-ink-muted border-t border-line-subtle mt-1',
        }, [`视频文件（${files.length}）`]))
        for (const f of files) {
          const row = el('div', {
            class: 'flex items-center gap-2 px-3 py-1.5 text-sm text-ink-muted',
          }, [svgIcon('file-video', 16) as unknown as Node, el('span', { class: 'flex-1 truncate' }, [f.name]), el('span', { class: 'text-xs' }, [formatSize(f.size || 0)])])
          listWrap.appendChild(row)
        }
      }

      if (dirs.length === 0 && files.length === 0) {
        listWrap.appendChild(el('div', { class: 'p-4 text-sm text-ink-muted' }, ['空目录']))
      }
    } catch (e) {
      listWrap.innerHTML = ''
      listWrap.appendChild(el('div', { class: 'p-4 text-sm text-danger' }, [(e as Error).message]))
    }
  }

  function dirRow(name: string, path: string): HTMLElement {
    const row = el('div', {
      class: 'flex items-center gap-2 px-3 py-2 cursor-pointer hover:bg-surface-hover text-sm',
    }, [svgIcon('folder', 16) as unknown as Node, el('span', {}, [name])])
    row.title = path
    return row
  }

  loadDirs(startPath || '')
}

function openCreateTaskModal(scanRoot: string) {
  if (selectedVideos.size === 0) return
  if (store.servers.length === 0) {
    toast('请先添加转码服务器', 'error')
    return
  }
  if (store.profiles.length === 0) {
    toast('请先添加转码方案', 'error')
    return
  }

  const overlay = el('div', { class: 'fixed inset-0 bg-black/40 z-50 flex items-center justify-center p-3' })
  const modal = el('div', { class: 'card w-full max-w-md p-5' })
  const title = el('h3', { class: 'font-semibold text-base mb-3' }, [`创建转码任务（${selectedVideos.size} 个文件）`])
  const serverLabel = el('label', { class: 'block text-sm mb-1' }, ['转码服务器 *'])
  const serverSel = el('select', { class: 'input mb-3' }) as HTMLSelectElement
  
  const localServer = store.servers.find(s => s.isLocal)
  if (localServer) {
    serverSel.appendChild(el('option', { value: localServer.id }, [`${localServer.name} (fnNAS 自转码)`]))
  } else {
    serverSel.appendChild(el('option', { value: '_local_' }, ['fnNAS 自转码 (使用本地算力)']))
  }
  
  for (const s of store.servers) {
    if (s.isLocal) continue
    let display = `${s.name} (${s.ip}:${s.port})`
    display += ` ${s.status === 'online' ? '● 在线' : '○ 离线'}`
    serverSel.appendChild(el('option', { value: s.id }, [display]))
  }

  const profileLabel = el('label', { class: 'block text-sm mb-1' }, ['转码方案 *'])
  const profileSel = el('select', { class: 'input mb-3' }) as HTMLSelectElement
  for (const p of store.profiles) {
    profileSel.appendChild(el('option', { value: p.id }, [p.name]))
  }

  const outLabel = el('label', { class: 'block text-sm mb-1' }, ['输出后缀'])
  const outInput = el('input', { class: 'input mb-3', value: '_trans.mp4', placeholder: '_trans.mp4' }) as HTMLInputElement

  profileSel.onchange = () => {
    const p = store.profiles.find(pr => pr.id === profileSel.value)
    if (p) {
      outInput.value = p.outputSuffix || '_trans.mp4'
    }
  }

  const defaultProfile = store.profiles[0]
  if (defaultProfile) {
    outInput.value = defaultProfile.outputSuffix || '_trans.mp4'
  }

  const btns = el('div', { class: 'flex justify-end gap-2 mt-4' })
  const cancel = el('button', { class: 'btn' }, ['取消'])
  const ok = el('button', { class: 'btn btn-primary' }, ['创建'])
  cancel.onclick = () => overlay.remove()
  ok.onclick = async () => {
    const serverId = serverSel.value
    const profileId = profileSel.value
    const suffix = outInput.value || '_trans.mp4'
    if (!serverId || !profileId) {
      toast('请选择服务器和方案')
      return
    }
    const profile = store.profiles.find(p => p.id === profileId)
    ok.disabled = true
    let created = 0
    const failures: { name: string; reason: string }[] = []
    const files = [...selectedVideos]
    for (let i = 0; i < files.length; i++) {
      const path = files[i]
      let outputFile: string
      if (profile && profile.outputPath) {
        const sep = path.includes('/') ? '/' : '\\'
        let relPath = scanRoot && path.startsWith(scanRoot)
          ? path.substring(scanRoot.length)
          : path.substring(path.lastIndexOf(sep) + 1)
        while (relPath.startsWith(sep)) relPath = relPath.substring(1)
        const relNoExt = relPath.replace(/\.[^.]+$/, '')
        outputFile = profile.outputPath.endsWith(sep) ? profile.outputPath : profile.outputPath + sep
        outputFile = outputFile + relNoExt + suffix
      } else {
        const baseName = String(path).replace(/\.[^.]+$/, '')
        outputFile = baseName + suffix
      }
      const fileName = String(path).split(/[\\/]/).pop() || path
      try {
        const r = await api.createTask({ sourceFile: path, outputFile, serverId, profileId })
        if (r.status === 'PAUSED') {
          toast(`任务已创建（服务器离线，任务已暂停）: ${r.fileName}`, 'info')
        }
        created++
      } catch (e) {
        failures.push({ name: fileName, reason: (e as Error).message })
      }
      if (i < files.length - 1) {
        await new Promise(r => setTimeout(r, 100))
      }
    }
    overlay.remove()
    if (created > 0) {
      selectedVideos.clear()
      await store.loadTasks()
    }
    // P2-1：批量创建提供逐项结果（成功 N、失败 M 及原因）
    showBatchResult('创建任务', created, failures)
    ok.disabled = false
  }
  btns.append(cancel, ok)
  modal.append(title, serverLabel, serverSel, profileLabel, profileSel, outLabel, outInput, btns)
  overlay.appendChild(modal)
  overlay.onclick = (e) => { if (e.target === overlay) overlay.remove() }
  document.body.appendChild(overlay)
  // P2-2：创建任务弹窗补对话框语义（role=dialog / aria-modal / Esc / 焦点陷阱）
  setupDialogAccessibility({ overlay, titleEl: title, onClose: () => overlay.remove() })
  serverSel.focus()
}

// openTrashManager 回收站管理弹窗（P2-5）：列表 / 恢复 / 清空
function openTrashManager() {
  const overlay = el('div', { class: 'fixed inset-0 bg-black/40 z-50 flex items-center justify-center p-3' })
  const modal = el('div', { class: 'card w-full max-w-2xl p-5 max-h-[80vh] flex flex-col' })
  const title = el('h3', { class: 'font-semibold text-base mb-3' }, ['回收站'])
  const listBox = el('div', { class: 'flex-1 overflow-auto border border-line rounded-lg mb-3 min-h-[200px]' })
  const emptyHint = el('div', { class: 'text-sm text-ink-muted p-4 text-center' }, ['回收站为空'])
  const btns = el('div', { class: 'flex justify-end gap-2' })
  const cancel = el('button', { class: 'btn' }, ['关闭'])
  const emptyBtn = el('button', { class: 'btn btn-danger' }, ['清空回收站'])

  async function load() {
    try {
      const r = await api.listTrash()
      listBox.innerHTML = ''
      if (!r.items || r.items.length === 0) {
        listBox.appendChild(emptyHint)
        emptyBtn.disabled = true
        return
      }
      emptyBtn.disabled = false
      for (const item of r.items) {
        const row = el('div', { class: 'flex items-center justify-between gap-2 px-3 py-2 border-b border-line last:border-0 text-sm' })
        const info = el('div', { class: 'flex-1 min-w-0' }, [
          el('div', { class: 'truncate font-medium' }, [item.name]),
          el('div', { class: 'text-xs text-ink-muted truncate' }, [item.origPath]),
        ])
        const restoreBtn = el('button', { class: 'btn btn-sm btn-primary shrink-0' }, ['恢复'])
        restoreBtn.onclick = async () => {
          try {
            await api.restoreTrash(item.path)
            toast(`已恢复: ${item.name}`, 'success')
            load()
          } catch (e) {
            toast((e as Error).message, 'error')
          }
        }
        row.append(info, restoreBtn)
        listBox.appendChild(row)
      }
    } catch (e) {
      listBox.innerHTML = ''
      listBox.appendChild(el('div', { class: 'text-sm text-danger p-4 text-center' }, [`加载失败: ${(e as Error).message}`]))
    }
  }

  cancel.onclick = () => overlay.remove()
  emptyBtn.onclick = async () => {
    emptyBtn.disabled = true
    try {
      const r = await api.emptyTrash()
      toast(`已清空回收站（${r.removed} 项）`, 'success')
      load()
    } catch (e) {
      toast((e as Error).message, 'error')
      emptyBtn.disabled = false
    }
  }

  btns.append(emptyBtn, cancel)
  modal.append(title, listBox, btns)
  overlay.appendChild(modal)
  overlay.onclick = (e) => { if (e.target === overlay) overlay.remove() }
  document.body.appendChild(overlay)
  setupDialogAccessibility({ overlay, titleEl: title, onClose: () => overlay.remove() })
  load()
}
