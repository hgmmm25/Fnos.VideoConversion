import type { Task, Server, Profile, Metrics, AppInfo, Settings } from './types'
import { api } from './api'
import { ws } from './ws'

// 全局响应式状态：单例 + 订阅通知
type Listener = () => void

/** WS proxy_ready 载荷（03 §6 / 04 §3.4）；assetId 为提交 POST /proxy 时前端生成并回传的素材 id */
export interface ProxyReadyInfo {
  assetId: string
  proxyFile: string
  durationMs: number
}

class Store {
  tasks: Task[] = []
  history: Task[] = []
  servers: Server[] = []
  profiles: Profile[] = []
  metrics: Metrics | null = null
  appInfo: AppInfo | null = null
  /** 当前打开的 EDL 项目 id（01 §5.1：由路由 #/editor/:id 驱动，editorStore 读取） */
  currentProjectId: string | null = null
  /** 最近一次加载/保存的应用设置（2026-09-16 新增：供剪辑页素材面板随设置刷新授权目录） */
  settings: Settings | null = null

  private listeners = new Set<Listener>()
  /** WS proxy_ready 订阅者（素材面板/剪辑页；2026-09-16 修复：此前 WS 事件无人消费） */
  private proxyReadyListeners = new Set<(p: ProxyReadyInfo) => void>()
  /** 设置变更订阅者（settings.ts 保存成功后推送，剪辑页据此刷新 accessiblePaths） */
  private settingsListeners = new Set<(s: Settings) => void>()

  subscribe(l: Listener) {
    this.listeners.add(l)
    return () => this.listeners.delete(l)
  }
  notify() {
    this.listeners.forEach((l) => l())
  }

  /** 订阅设置变更（返回取消订阅函数）；settings.ts 保存时统一调用 applySettings */
  onSettingsChanged(fn: (s: Settings) => void): () => void {
    this.settingsListeners.add(fn)
    // 修复②：已有设置时同步回放一次——订阅方可能尚未完成内部装配，
    // 回调异常不得向上冒泡（否则会中断调用方渲染流程，表现为整页空白）
    if (this.settings) {
      try {
        fn(this.settings)
      } catch (e) {
        console.warn('[store] settings handler (immediate replay)', e)
      }
    }
    return () => {
      this.settingsListeners.delete(fn)
    }
  }
  applySettings(s: Settings | null | undefined) {
    if (!s) return
    this.settings = s
    this.settingsListeners.forEach((fn) => {
      try {
        fn(s)
      } catch (e) {
        console.warn('[store] settings handler', e)
      }
    })
  }

  /** 订阅代理就绪事件；返回取消订阅函数（调用方必须在销毁时调用，避免面板泄漏） */
  onProxyReady(fn: (p: ProxyReadyInfo) => void): () => void {
    this.proxyReadyListeners.add(fn)
    return () => {
      this.proxyReadyListeners.delete(fn)
    }
  }
  private emitProxyReady(p: ProxyReadyInfo) {
    this.proxyReadyListeners.forEach((fn) => {
      try {
        fn(p)
      } catch (e) {
        console.warn('[store] proxy_ready handler', e)
      }
    })
  }

  setCurrentProjectId(id: string | null) {
    if (this.currentProjectId === id) return
    this.currentProjectId = id
    this.notify()
  }

  // ===== 加载 =====
  // 数据去重保护（2026-09-19 修复）：load* 完成后若数据与当前完全一致则跳过 notify。
  // 此前 load* 无条件 notify，而 useListPage 的 subscribe 回调会 refresh→load→notify，
  // 形成「加载→通知→再加载」无限循环：列表页被持续全量重建、表单输入被吞、按钮点击失效，
  // 同时向后端狂发 GET /api/{tasks|servers|profiles} 请求。
  private sameList<T>(a: T[], b: T[]): boolean {
    if (a.length !== b.length) return false
    return JSON.stringify(a) === JSON.stringify(b)
  }
  private sameJson(a: unknown, b: unknown): boolean {
    return JSON.stringify(a) === JSON.stringify(b)
  }

  async loadAll() {
    await Promise.all([this.loadTasks(), this.loadServers(), this.loadProfiles(), this.loadMetrics()])
  }
  async loadInfo() {
    try {
      this.appInfo = await api.info()
      this.notify()
    } catch (e) {
      console.warn('[store] load info', e)
    }
  }
  async loadTasks() {
    const r = await api.listTasks()
    if (this.sameList(this.tasks, r.tasks)) return
    this.tasks = r.tasks
    this.notify()
  }
  async loadHistory() {
    const r = await api.listHistory()
    if (this.sameList(this.history, r.tasks)) return
    this.history = r.tasks
    this.notify()
  }
  async loadServers() {
    const r = await api.listServers()
    if (this.sameList(this.servers, r.servers)) return
    this.servers = r.servers
    this.notify()
  }
  async loadProfiles() {
    const r = await api.listProfiles()
    if (this.sameList(this.profiles, r.profiles)) return
    this.profiles = r.profiles
    this.notify()
  }
  async loadMetrics() {
    try {
      const m = await api.metrics()
      if (this.sameJson(this.metrics, m)) return
      this.metrics = m
      this.notify()
    } catch (e) {
      console.warn('[store] load metrics', e)
    }
  }

  // 应用 WS 推送
  private applyTaskUpdate(msg: any) {
    if (msg.type !== 'task_update' || !msg.taskId) return
    const idx = this.tasks.findIndex((t) => t.id === msg.taskId)
    if (idx >= 0) {
      this.tasks[idx] = {
        ...this.tasks[idx],
        status: msg.status,
        progress: msg.progress ?? this.tasks[idx].progress,
        errorMsg: msg.message ?? this.tasks[idx].errorMsg,
      }
      // 完成态或错误态延迟移入历史
      if (msg.status === 'COMPLETED' || msg.status === 'CANCELLED' || msg.status === 'ERROR') {
        setTimeout(() => this.loadTasks().then(() => this.loadHistory()), 800)
      }
      this.notify()
    } else {
      // 新任务或未在列表中，重新加载
      this.loadTasks()
    }
  }

  startWS() {
    ws.connect()
    ws.on((msg) => {
      if (msg.type === 'task_update') this.applyTaskUpdate(msg)
      else if (msg.type === 'proxy_ready') {
        // 04 §3.4：代理生成完成后切预览源（此前该事件未订阅，代理状态永远停在"待生成"）
        this.emitProxyReady({
          assetId: String(msg.assetId || ''),
          proxyFile: String(msg.proxyFile || ''),
          durationMs: Number(msg.durationMs) || 0,
        })
        // 任务抽屉/历史里的 GEN_PROXY 任务状态同步刷新
        void this.loadTasks()
      } else if (msg.type === 'node_status') {
        // 需求1：渲染节点状态变化（online↔offline / 健康分跨档）→ 刷新 servers 状态
        const sid = String(msg.serverId || '')
        if (sid) {
          const status = msg.status === 'online' ? 'online' : 'offline'
          const idx = this.servers.findIndex((s) => s.id === sid)
          if (idx >= 0) {
            if (this.servers[idx].status !== status) {
              this.servers[idx] = { ...this.servers[idx], status }
              this.notify()
            }
          } else {
            void this.loadServers()
          }
        }
      } else if (msg.type === 'snapshot') {
        const data = msg.data || []
        if (this.sameList(this.tasks, data)) return
        this.tasks = data
        this.notify()
      }
    })
  }
}

export const store = new Store()
