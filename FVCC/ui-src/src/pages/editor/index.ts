// C-02：剪辑页入口（路由 #/editor/:projectId）+ 项目选择页（#/editor）
// C-03：接入 editorStore（状态机 + 撤销栈 + 2s 防抖保存），保存实现由本文件注入
// 三段布局由 layout.ts 提供；素材库/预览器/时间线的具体实现由 C-04/C-05/C-07 接入
import { el, svgIcon, toast, emptyState, formatTime, confirmDialog, skeletonRows } from '../../ui'
import { api, ApiError } from '../../api'
import { store } from '../../store'
import type { Project, ProjectSummary } from '../../types'
import { buildEditorLayout, type EditorLayout } from './layout'
import { createEditorStore, type EditorState, type EditorStore } from './editorStore'
import { buildAssetsPanel, type AssetsPanel } from './assets'
import { buildPreview, type PreviewController } from './preview'
import { buildTimeline, type TimelinePanel } from './timeline'
import { bindEditorShortcuts } from './shortcuts'
import { addWholeClip as addWholeClipOp, type ClipSource } from './clipOps'
import { buildTaskDrawer, type TaskDrawer } from './taskDrawer'
import { openRenderDialog, type RenderDialogHandle } from './dialog'
import { DEFAULT_PRESET_KEY, RENDER_PRESETS } from './preset'
import { formatMs } from './format'

function defaultTimeline(): Project['timeline'] {
  return { width: 1920, height: 1080, fps: 30, sampleRate: 48000, audio: true }
}

/** 各面板接入前的占位说明（后续任务替换为真实面板） */
function placeholder(text: string, icon: 'folder' | 'play' | 'film' = 'film'): HTMLElement {
  return el(
    'div',
    { class: 'h-full flex flex-col items-center justify-center text-ink-muted text-xs px-4 text-center gap-2' },
    [svgIcon(icon, 24) as unknown as Node, el('span', {}, [text])]
  )
}

export async function renderProjectPicker(root: HTMLElement) {
  root.innerHTML = ''
  const wrap = el('div', { class: 'p-4 sm:p-6 max-w-6xl mx-auto' })

  // ===== 动效改进（2026-09-20）：视图切换器（详细信息 / 卡片式，滑块左右滑动）=====
  const VIEW_KEY = 'wve.picker.view.v1'
  type PickerView = 'detail' | 'grid'
  function loadView(): PickerView {
    try {
      return localStorage.getItem(VIEW_KEY) === 'grid' ? 'grid' : 'detail'
    } catch {
      return 'detail'
    }
  }
  function saveView(v: PickerView) {
    try {
      localStorage.setItem(VIEW_KEY, v)
    } catch {
      /* 隐私模式或配额不足时静默降级 */
    }
  }
  const VIEW_BTN_W = 36 // w-9

  let viewMode: PickerView = loadView()

  const viewToggle = el('div', {
    class: 'relative flex items-center rounded-lg bg-surface-hover p-0.5 shrink-0',
    role: 'group',
    'aria-label': '项目列表视图',
  })
  const viewThumb = el('div', {
    class: 'absolute top-0.5 bottom-0.5 w-9 rounded-md bg-surface pointer-events-none transition-transform duration-300 ease-out-strong',
  })
  viewToggle.append(viewThumb)
  const viewDefs: { key: PickerView; icon: 'list' | 'grid'; label: string }[] = [
    { key: 'detail', icon: 'list', label: '详细信息' },
    { key: 'grid', icon: 'grid', label: '卡片式' },
  ]
  const viewButtons: HTMLButtonElement[] = []
  for (const v of viewDefs) {
    const b = el('button', {
      class: 'relative w-9 h-8 flex items-center justify-center rounded-md text-ink-muted hover:text-ink transition-colors',
      type: 'button',
      title: v.label,
      'aria-label': v.label,
    }) as HTMLButtonElement
    b.append(svgIcon(v.icon, 16) as unknown as Node)
    b.onclick = () => {
      if (viewMode === v.key) return
      viewMode = v.key
      saveView(viewMode)
      syncViewToggle()
      load()
    }
    viewButtons.push(b)
    viewToggle.append(b)
  }
  function syncViewToggle() {
    viewThumb.style.transform = `translateX(${(viewMode === 'detail' ? 0 : 1) * VIEW_BTN_W}px)`
    viewButtons.forEach((b, i) => {
      const active = (viewMode === 'detail' ? 0 : 1) === i
      b.classList.toggle('text-primary', active)
      b.setAttribute('aria-pressed', String(active))
    })
  }
  syncViewToggle()

  const head = el('div', { class: 'flex items-center justify-between mb-4 gap-2 flex-wrap' })
  head.append(
    el('h2', { class: 'text-lg font-semibold flex items-center gap-2' }, [
      svgIcon('scissors', 18) as unknown as Node,
      '剪辑项目',
    ]),
    el('div', { class: 'flex items-center gap-2 flex-wrap' }, [viewToggle, buildCreateForm()])
  )

  function buildCreateForm(): HTMLElement {
    // 动效改进：新建按钮去文字改圆形 +（尺寸匹配输入框 h-9）；输入框聚焦时由窄展开变长
    const form = el('div', { class: 'flex items-center gap-2' })
    const nameInput = el('input', {
      class:
        'h-9 w-32 focus:w-72 max-w-[60vw] px-3 text-sm rounded border border-line ' +
        'bg-surface text-ink placeholder-ink-muted ' +
        'focus:outline-none focus:ring-2 focus:ring-primary/40 focus:border-primary ' +
        'transition-[width] duration-300 ease-out-strong',
      placeholder: '新项目名称',
      maxlength: '64',
    }) as HTMLInputElement
    const createBtn = el('button', {
      class:
        'btn btn-primary !px-0 w-9 h-9 rounded-full shrink-0 ' +
        'transition-transform duration-200 ease-out-strong hover:scale-105 active:scale-95',
      title: '新建项目',
      'aria-label': '新建项目',
    })
    createBtn.append(svgIcon('plus', 18) as unknown as Node)
    form.append(nameInput, createBtn)
    createBtn.onclick = async () => {
      const name = nameInput.value.trim()
      if (!name) {
        toast('请输入项目名称', 'error')
        nameInput.focus()
        return
      }
      createBtn.setAttribute('disabled', 'true')
      try {
        const p = await api.createProject({ name, timeline: defaultTimeline() })
        nameInput.value = ''
        toast('项目已创建', 'success')
        openProject(p.id)
      } catch (e) {
        toast('创建失败：' + errText(e), 'error')
      } finally {
        createBtn.removeAttribute('disabled')
      }
    }
    nameInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') createBtn.click()
    })
    return form
  }

  wrap.append(head)

  const listBox = el('div', { class: 'grid gap-2' })
  wrap.append(listBox)
  root.append(wrap)

  async function load() {
    listBox.innerHTML = ''
    // 视图切换：卡片式用自适应栏位网格（auto-fill + minmax），详细信息保持纵向行卡片
    listBox.className =
      viewMode === 'grid'
        ? 'grid gap-4 grid-cols-[repeat(auto-fill,minmax(11rem,1fr))]'
        : 'grid gap-2'
    try {
      const r = await api.listProjects()
      // 服务端主字段为 items，projects 为兼容别名（2026-09-16 修复历史项目不显示）
      const list = r.projects ?? r.items ?? []
      if (!list.length) {
        listBox.append(emptyState('还没有剪辑项目，先在上方新建一个', 'film'))
        return
      }
      list.forEach((p, i) => {
        const card = viewMode === 'grid' ? gridCard(p) : detailCard(p)
        if (viewMode === 'grid') {
          // 卡片式：进入动画逐张错开（动效改进）
          card.classList.add('animate-fade-in-up')
          card.style.animationDelay = Math.min(i, 8) * 40 + 'ms'
        }
        listBox.append(card)
      })
    } catch (e) {
      listBox.append(emptyState('项目列表加载失败：' + errText(e), 'warning'))
    }
  }

  async function deleteProject(p: ProjectSummary) {
    try {
      await api.deleteProject(p.id)
      toast('项目已删除', 'success')
      load()
    } catch (e) {
      toast('删除失败：' + errText(e), 'error')
    }
  }

  function detailCard(p: ProjectSummary): HTMLElement {
    const card = el('div', { class: 'card flex items-center gap-3 px-3 py-2' })
    const main = el('div', { class: 'flex-1 min-w-0 cursor-pointer' })
    main.append(
      el('div', { class: 'font-medium text-sm truncate' }, [p.name]),
      el('div', { class: 'text-xs text-ink-muted' }, [
        `${p.clipCount} 个片段 · rev ${p.rev} · ${formatTime(p.updatedAt)}`,
      ])
    )
    main.onclick = () => openProject(p.id)

    const openBtn = el('button', { class: 'btn btn-sm' }, ['打开'])
    openBtn.onclick = () => openProject(p.id)

    const delBtn = el('button', { class: 'btn btn-sm flex items-center', title: '删除项目' })
    delBtn.append(svgIcon('trash', 14) as unknown as Node)
    delBtn.onclick = () => void deleteProject(p)

    card.append(main, openBtn, delBtn)
    return card
  }

  // 卡片式：缩略图（复用 /thumb 抽帧，素材面板同源能力）+ 标题，自适应栏位
  function gridCard(p: ProjectSummary): HTMLElement {
    const card = el('div', {
      class:
        'card overflow-hidden border border-line-subtle cursor-pointer group ' +
        'transition-all duration-200 ease-out-strong hover:border-primary/50 hover:shadow-lg hover:-translate-y-0.5',
    })
    const media = el('div', {
      class: 'relative aspect-video bg-surface-alt flex items-center justify-center overflow-hidden',
    })
    const hasPoster = p.clipCount > 0 && !!p.posterFile
    if (hasPoster) {
      const img = el('img', {
        class: 'w-full h-full object-cover transition-transform duration-300 ease-out-strong group-hover:scale-105',
        loading: 'lazy',
        alt: p.name,
      }) as HTMLImageElement
      // 复用已被提取的缩略图能力：/thumb 按首片段 inMs 抽帧（与素材面板同一端点）
      img.src = api.thumbUrl(p.posterFile as string, Math.max(0, p.posterMs ?? 0))
      img.onerror = () => {
        img.remove()
        media.append(placeholderIcon())
      }
      media.append(img)
    } else {
      media.append(placeholderIcon())
    }

    const body = el('div', { class: 'p-3' }, [
      el('div', { class: 'font-medium text-sm truncate', title: p.name }, [p.name]),
      el('div', { class: 'text-xs text-ink-muted mt-0.5' }, [
        `${p.clipCount} 个片段 · ${formatTime(p.updatedAt)}`,
      ]),
    ])

    const delBtn = el('button', {
      class:
        'absolute top-2 right-2 w-7 h-7 flex items-center justify-center rounded-full ' +
        'bg-black/50 text-white opacity-0 group-hover:opacity-100 transition-opacity duration-200',
      title: '删除项目',
      'aria-label': '删除项目',
    })
    delBtn.append(svgIcon('trash', 13) as unknown as Node)
    delBtn.onclick = (e) => {
      e.stopPropagation()
      void deleteProject(p)
    }

    card.append(media, body, delBtn)
    card.onclick = () => openProject(p.id)
    return card
  }

  function placeholderIcon(): HTMLElement {
    return el('div', { class: 'text-ink-subtle' }, [svgIcon('film', 32) as unknown as Node])
  }

  await load()
}

function errText(e: unknown): string {
  if (e instanceof ApiError) return e.code && e.code !== 'E_UNKNOWN' ? `${e.message}（${e.code}）` : e.message
  return e instanceof Error ? e.message : String(e)
}

function openProject(id: string) {
  location.hash = '#/editor/' + encodeURIComponent(id)
}

export async function renderEditor(root: HTMLElement, projectId: string): Promise<() => void> {
  root.innerHTML = ''
  // P2-1：项目加载改用骨架屏（替代"项目加载中…"文本）
  root.append(el('div', { class: 'p-4 space-y-3' }, skeletonRows(5, '3rem')))

  let project: Project
  try {
    project = await api.getProject(projectId)
  } catch (e) {
    root.innerHTML = ''
    const box = el('div', { class: 'p-6 max-w-lg mx-auto' }, [
      emptyState('项目打开失败：' + errText(e), 'warning'),
    ])
    const back = el('button', { class: 'btn mx-auto flex' }, ['返回项目列表'])
    back.onclick = () => {
      location.hash = '#/editor'
    }
    box.append(back)
    root.append(box)
    return () => {}
  }

  store.setCurrentProjectId(projectId)
  root.innerHTML = ''

  // ===== C-03：状态机 + 保存实现注入（02 §5.4）=====
  const edlStore: EditorStore = createEditorStore(projectId, {
    save: async (s) => {
      if (!s.projectId) throw new Error('projectId 缺失')
      const r = await api.updateProject(s.projectId, {
        rev: s.rev,
        name: s.name,
        timeline: s.timeline,
        clips: s.clips,
      })
      return { rev: r.rev, savedAt: r.savedAt }
    },
  })
  edlStore.dispatch({
    type: 'load',
    payload: {
      rev: project.rev,
      name: project.name,
      timeline: project.timeline,
      clips: project.clips,
    },
  })

  // C-10：剪辑快捷键统一由 shortcuts.ts 接管（旧版仅 Ctrl+S/Z 的局部绑定已移除，装配见下方预览器/时间线之后）
  let unbindShortcuts: () => void = () => {}

  const layout: EditorLayout = buildEditorLayout({
    projectName: project.name,
    projectId: project.id,
    onBack: () => {
      location.hash = '#/editor'
    },
    // C-09：顶栏入口 → 右侧任务中心抽屉（01 §4.5），不再整页跳转 #/tasks
    onOpenTasks: () => drawer.toggle(),
    // C-08：顶栏「渲染导出」与工具条同入口
    onRender: () => void handleRender(),
  })

  const statsEl = el('div', { class: 'text-xs text-ink-muted leading-tight text-right shrink-0 whitespace-nowrap' }, ['—'])
  layout.toolbar.append(statsEl)

  // ===== 需求1：顶栏「节点」显示真实链接状态（原实现仅初始化『节点 —』，setNodeStatus 从未被调用）=====
  function applyNodeStatus() {
    const servers = store.servers
    if (!servers.length) {
      layout.setNodeStatus('节点 未配置', 'muted')
      return
    }
    const online = servers.filter((s) => s.status === 'online').length
    if (online > 0) {
      layout.setNodeStatus(`节点 在线 ${online}/${servers.length}`, 'ok')
    } else {
      layout.setNodeStatus(`节点 离线 ${servers.length}`, 'bad')
    }
  }
  const unsubNode = store.subscribe(applyNodeStatus)
  applyNodeStatus()
  void store.loadServers().then(applyNodeStatus).catch(() => {})

  const unsub = edlStore.subscribe((s, changed) => {
    if (changed.includes('save')) renderSaveBadge(layout, s)
    if (changed.includes('clips')) renderStats(statsEl, edlStore)
    if (changed.includes('save') && s.save === 'conflict') void handleConflict()
  })
  renderSaveBadge(layout, edlStore.getState())
  renderStats(statsEl, edlStore)

  // ===== C-04/C-05/C-09：素材面板装配 =====
  // 修复①：读取全部授权目录（不再只取 accessiblePaths[0]），面板内以下拉切换
  // 修复①：预览器实例提前声明，消除 TDZ（下方 onSettingsChanged 同步回调会在预览器装配前访问 preview）
  let preview: PreviewController | null = null

  let roots: string[] = []
  // 修复⑤：预览器初始静音态由 Settings.playerMuted 决定（缺省 true，保持既有行为）
  let playerMuted = true
  try {
    const settings = await api.getSettings()
    roots = settings.settings?.accessiblePaths || []
    playerMuted = settings.settings?.playerMuted !== false
    // 同步到全局 store，其它页面可复用（settings.ts 保存时也会推送）
    store.applySettings(settings.settings)
  } catch {
    roots = []
  }

  let assetsPanel: AssetsPanel | null = null
  if (roots.length) {
    assetsPanel = buildAssetsPanel({
      edlStore,
      roots,
      // C-05：单击素材 → 装配预览器并 seek 到 0（/stream 票据由预览器内部申请）
      onPick: (asset) => preview?.reveal(asset.file, 0, { assetId: asset.assetId, durationMs: asset.durationMs }),
      onAddWhole: (asset) => addWholeClip(edlStore, asset),
      // C-09：代理任务由素材面板提交（04 §3.2）；此处仅联动任务中心刷新
      onRequestProxy: () => void store.loadTasks(),
    })
    layout.assets.append(assetsPanel.root)
  } else {
    layout.assets.append(placeholder('未配置可访问目录，请先在「设置」中配置', 'folder'))
  }
  // 修复⑥：设置保存后（settings.ts → store.applySettings）实时刷新授权目录，无需重进页面
  const unsubSettings = store.onSettingsChanged((s) => {
    // 修复⑤：设置页切换「预览器默认静音」后，已打开的预览器立即生效
    preview?.setMuted(s?.playerMuted !== false)
    const next = s?.accessiblePaths || []
    if (!next.length) return
    if (!assetsPanel) {
      assetsPanel = buildAssetsPanel({
        edlStore,
        roots: next,
        onPick: (asset) => preview?.reveal(asset.file, 0, { assetId: asset.assetId, durationMs: asset.durationMs }),
        onAddWhole: (asset) => addWholeClip(edlStore, asset),
        onRequestProxy: () => void store.loadTasks(),
      })
      layout.assets.innerHTML = ''
      layout.assets.append(assetsPanel.root)
    } else {
      assetsPanel.setRoots(next)
    }
  })

  // 装配期共享状态：预览器实例已在函数前部声明（素材单击 / 时间线定位 / 快捷键共用）
  // C-08：渲染弹窗句柄（单例；关闭后由 onClosed 置空，避免重复弹出）
  let renderDlg: RenderDialogHandle | null = null
  // 渲染方案选择态（时间线工具条下拉与渲染弹窗共用同一 presetKey，C-08）
  let presetKey = DEFAULT_PRESET_KEY

  // ===== C-05/C-06：预览器与打点装配 =====
  preview = buildPreview({
    edlStore,
    // 修复⑤：默认静音态不再硬编码，取设置页配置（缺省 true 保持既有行为）
    initialMuted: playerMuted,
    // 打点 → 时间线：片段由 preview 侧按 in/out 构造，这里只做装配；上限由 store 的 addClip 守卫 + 落地复核兜底
    onAddClip: (clip) => {
      edlStore.dispatch({ type: 'addClip', clip })
      if (!edlStore.getState().clips.some((c) => c.clipId === clip.clipId)) {
        toast('添加失败：片段数已达上限或入出点非法（01 §5.1）', 'error')
      }
    },
  })
  layout.preview.append(preview.root)

  // ===== C-07：时间线装配 =====
  const timeline: TimelinePanel = buildTimeline({
    edlStore,
    presets: RENDER_PRESETS,
    selectedPreset: presetKey,
    onSelectPreset: (key) => {
      presetKey = key
    },
    // 时间线/ruler 定位 → 预览器 seek（切素材由 reveal 内部处理，02 §6.4）
    onLocate: (file, ms, hint) => preview?.reveal(file, ms, hint),
    // 素材拖入轨道 → 整段添加（含 atIndex）
    onDropAsset: (asset, atIndex) => addWholeClip(edlStore, asset, atIndex),
    // 工具条 [+ 添加当前素材]：按打点区间或整段添加
    onAddCurrent: () => preview?.addMarksToTimeline(),
    onRender: () => void handleRender(),
    // 拖拽校验 + 「素材已变更」检测（02 §7.3）
    lookupAsset: (assetId) => assetsPanel?.list().find((a) => a.assetId === assetId),
  })
  layout.toolbar.append(timeline.toolbar)
  layout.timeline.append(timeline.root)

  // 功能1（时间线位）：预览器播放时间 → 时间线播放头/指示条（preview.onTime → timeline.setTime → updatePlayhead）
  // 缺失该连接时 lastFile 恒为 null，updatePlayhead 找不到当前片段，竖线 playhead 与片段覆盖层 playBar 全部隐藏
  const unsubTime = preview?.onTime((ms, file) => timeline.setTime(ms, file))

  // ===== C-09：任务中心抽屉（顶栏入口 → onOpenTasks；关闭不销毁列表）=====
  const drawer: TaskDrawer = buildTaskDrawer('任务中心')
  layout.root.append(drawer.root)

  // ===== C-10：剪辑快捷键（Space 播放/暂停、I/O 打点、Delete 删段、Ctrl+Z/S…）=====
  unbindShortcuts = bindEditorShortcuts({
    edlStore,
    preview,
    onRender: () => void handleRender(),
  })

  root.append(layout.root)

  let conflictPending = false

  /** 版本冲突（03 §4.3）：覆盖云端 / 载入云端，由用户选择 */
  async function handleConflict() {
    if (conflictPending) return
    conflictPending = true
    try {
      const overwrite = await confirmDialog(
        '项目已在其他页面被修改。确定 = 用本地版本覆盖云端；取消 = 载入云端最新版本（放弃本地改动）。',
        '版本冲突',
        true
      )
      const latest = await api.getProject(projectId)
      if (overwrite) {
        const local = edlStore.getState()
        const r = await api.updateProject(projectId, {
          rev: latest.rev,
          name: local.name,
          timeline: local.timeline,
          clips: local.clips,
        })
        edlStore.dispatch({ type: 'saveOk', rev: r.rev, at: r.savedAt })
        toast('已用本地版本覆盖云端', 'success')
      } else {
        edlStore.dispatch({
          type: 'load',
          payload: {
            rev: latest.rev,
            name: latest.name,
            timeline: latest.timeline,
            clips: latest.clips,
          },
        })
        toast('已载入云端最新版本', 'info')
      }
    } catch (e) {
      toast('版本冲突处理失败：' + errText(e), 'error')
    } finally {
      conflictPending = false
    }
  }

  /** C-08：渲染弹窗入口（顶栏 / 时间线工具条 / Ctrl+Enter 共用） */
  function handleRender() {
    if (renderDlg) return
    renderDlg = openRenderDialog({
      projectId,
      projectName: project.name,
      // 02 §5.4：提交前强制 flushSave；保存未完成则阻止提交（按钮态由 dialog 提示）
      beforeSubmit: async () => {
        await edlStore.flushSave()
        return edlStore.getState().save === 'saved'
      },
      onSubmitted: (r) => {
        toast(`渲染任务已提交，节点 ${r.serverId || '待分配'}`, 'success')
        // 打开任务中心抽屉，用户可立刻看到新任务（01 §4.5）
        drawer.open()
        void store.loadTasks()
      },
      onClosed: () => {
        renderDlg = null
      },
    })
  }

  return () => {
    unbindShortcuts()
    unsubTime?.()
    unsub()
    unsubNode()
    unsubSettings()
    renderDlg?.close()
    drawer.destroy()
    timeline.destroy()
    preview?.destroy()
    assetsPanel?.destroy()
    edlStore.destroy()
    layout.destroy()
    store.setCurrentProjectId(null)
  }
}

/** 整段添加（素材双击 / 拖拽 / 时间线工具条共用，01 §4.2/§4.4）；上限校验与 toast 复用 clipOps */
export function addWholeClip(edlStore: EditorStore, asset: ClipSource, atIndex?: number): boolean {
  // addClip 分支已内置选中新片段，预览器由 'selection' 切片自动 seek（C-05/C-06）
  return addWholeClipOp(edlStore, asset, atIndex).ok
}

/** 顶栏保存状态（01 §4.1：已保存 / 未保存 / 保存中 / 冲突 / 失败） */
function renderSaveBadge(layout: EditorLayout, s: EditorState) {
  switch (s.save) {
    case 'saved':
      layout.setSaveState(`已同步 rev ${s.rev}`, 'ok')
      break
    case 'dirty':
      layout.setSaveState('未保存', 'busy')
      break
    case 'saving':
      layout.setSaveState('保存中…', 'busy')
      break
    case 'conflict':
      layout.setSaveState('版本冲突', 'warn')
      break
    case 'error':
      layout.setSaveState('保存失败', 'warn')
      break
  }
}

function renderStats(target: HTMLElement, edlStore: EditorStore) {
  const d = edlStore.derive()
  target.innerHTML = ''
  // 2026-09-20 UI 精简：总时长 / 片段数拆为两行显示（原为一行「·」拼接）
  target.append(el('div', {}, [`总时长 ${formatMs(d.totalMs)}`]), el('div', {}, [`${d.clipCount} 个片段`]))
}
