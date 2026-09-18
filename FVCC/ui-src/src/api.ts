import type {
  Task,
  Server,
  Profile,
  VideoInfo,
  Metrics,
  AppInfo,
  BrowseResult,
  Settings,
  Project,
  TrashItem,
  ProjectSummary,
  ProjectCreateRequest,
  ProjectUpdateRequest,
  ProjectUpdateResult,
  RenderRequest,
  RenderSubmitResult,
  StreamTicket,
  StreamTicketRequest,
  ProxyRequest,
  ProxyRequestResult,
  MediaRoot,
} from './types'

const BASE = '/app/fvcc/api'

/** FVCC 统一错误契约（03 §4.1 / 02 §8.1）：{ ok:false, code, msg, detail? }；旧接口沿用 { error } */
export class ApiError extends Error {
  code: string
  status: number
  detail?: unknown

  constructor(code: string, message: string, detail?: unknown, status?: number) {
    super(message)
    this.name = 'ApiError'
    this.code = code || 'E_UNKNOWN'
    this.detail = detail
    this.status = status ?? 0
  }
}

async function request<T>(path: string, opts?: RequestInit): Promise<T> {
  const resp = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  })
  if (!resp.ok) {
    let msg = resp.statusText
    let code = ''
    let detail: unknown
    try {
      const body = await resp.json()
      msg = body.msg || body.error || msg
      code = body.code || ''
      detail = body.detail
    } catch {
      /* ignore */
    }
    throw new ApiError(code, msg, detail, resp.status)
  }
  return resp.json() as Promise<T>
}

export const api = {
  info: () => request<AppInfo>('/info'),
  metrics: () => request<Metrics>('/metrics'),

  // 视频
  scanDirectory: (path: string) =>
    request<{ videos: VideoInfo[]; total: number }>('/video/scan', {
      method: 'POST',
      body: JSON.stringify({ path }),
    }),
  scanDirectoryStream: (
    path: string,
    onProgress: (videos: VideoInfo[]) => void,
    onError?: (error: string) => void,
    recursive = true,
  ) => {
    let eventSource: EventSource | null = new EventSource(
      BASE +
        '/video/scan-stream?path=' +
        encodeURIComponent(path) +
        '&recursive=' +
        (recursive ? 'true' : 'false'),
    )
    const promise = new Promise<void>((resolve, reject) => {
      if (!eventSource) return
      eventSource.addEventListener('message', (e) => {
        try {
          const data = JSON.parse(e.data)
          if (data.type === 'progress') {
            onProgress(data.videos || [])
          } else if (data.type === 'done') {
            eventSource?.close()
            eventSource = null
            resolve()
          } else if (data.type === 'error') {
            eventSource?.close()
            eventSource = null
            // P2-4：统一后 SSE 错误为 {type:'error', code, msg}；兼容旧 {error}
            const errMsg = data.msg || data.error
            if (onError) onError(errMsg)
            else reject(new Error(errMsg))
          }
        } catch (err) {
          eventSource?.close()
          eventSource = null
          reject(err)
        }
      })
      eventSource.onerror = () => {
        eventSource?.close()
        eventSource = null
        reject(new Error('连接错误'))
      }
    })
    return {
      promise,
      cancel: () => {
        if (eventSource) {
          eventSource.close()
          eventSource = null
        }
      },
    }
  },
  probeVideo: (path: string) =>
    request<VideoInfo>('/video/probe', { method: 'POST', body: JSON.stringify({ path }) }),
  getPreviewUrl: (path: string) => '/app/fvcc/api/video/preview/' + encodeURIComponent(path),
  renameVideo: (path: string, newName: string) =>
    request<{ ok: boolean; newPath: string }>('/video/rename', { 
      method: 'POST', 
      body: JSON.stringify({ path, newName }) 
    }),
  moveVideo: (path: string, destDir: string) =>
    request<{ ok: boolean; newPath: string }>('/video/move', { 
      method: 'POST', 
      body: JSON.stringify({ path, destDir }) 
    }),
  deleteVideo: (path: string) =>
    request<{ ok: boolean; trashPath: string }>('/video/delete', { method: 'POST', body: JSON.stringify({ path }) }),

  // 回收站（P2-5：删除回收站化）
  listTrash: () => request<{ ok: boolean; items: TrashItem[]; count: number }>('/trash'),
  restoreTrash: (path: string) =>
    request<{ ok: boolean; path: string }>('/trash/restore', { method: 'POST', body: JSON.stringify({ path }) }),
  emptyTrash: () => request<{ ok: boolean; removed: number }>('/trash/empty', { method: 'POST' }),

  // 目录浏览
  browseDirs: (path?: string) =>
    request<BrowseResult>('/dirs' + (path ? '?path=' + encodeURIComponent(path) : '')),

  // 服务器
  listServers: () => request<{ servers: Server[] }>('/servers'),
  createServer: (sv: Partial<Server>) =>
    request<Server>('/servers', { method: 'POST', body: JSON.stringify(sv) }),
  updateServer: (id: string, sv: Partial<Server>) =>
    request<Server>(`/servers/${id}`, { method: 'PUT', body: JSON.stringify(sv) }),
  deleteServer: (id: string) => request<{ ok: boolean }>(`/servers/${id}`, { method: 'DELETE' }),
  testServer: (id: string) =>
    request<{ ok: boolean; status?: string; msg?: string; error?: string }>(`/servers/${id}/test`, {
      method: 'POST',
    }),

  // 转码方案
  listProfiles: () => request<{ profiles: Profile[] }>('/profiles'),
  createProfile: (p: Partial<Profile>) =>
    request<Profile>('/profiles', { method: 'POST', body: JSON.stringify(p) }),
  updateProfile: (id: string, p: Partial<Profile>) =>
    request<Profile>(`/profiles/${id}`, { method: 'PUT', body: JSON.stringify(p) }),
  deleteProfile: (id: string) =>
    request<{ ok: boolean }>(`/profiles/${id}`, { method: 'DELETE' }),

  // 任务
  listTasks: () => request<{ tasks: Task[]; total: number }>('/tasks'),
  createTask: (body: { sourceFile: string; outputFile: string; serverId: string; profileId: string }) =>
    request<Task>('/tasks', { method: 'POST', body: JSON.stringify(body) }),
  reorderTasks: (taskIds: string[]) =>
    request<{ ok: boolean }>('/tasks/reorder', { method: 'POST', body: JSON.stringify({ taskIds }) }),
  pauseTask: (id: string) => request<Task>(`/tasks/${id}/pause`, { method: 'POST' }),
  resumeTask: (id: string) => request<Task>(`/tasks/${id}/resume`, { method: 'POST' }),
  cancelTask: (id: string) => request<Task>(`/tasks/${id}/cancel`, { method: 'POST' }),
  retryTask: (id: string) => request<Task>(`/tasks/${id}/retry`, { method: 'POST' }),
  deleteTask: (id: string) => request<{ ok: boolean }>(`/tasks/${id}`, { method: 'DELETE' }),

  // 历史
  listHistory: () => request<{ tasks: Task[]; total: number }>('/history'),
  deleteHistory: (id: string) =>
    request<{ ok: boolean }>(`/history/${id}`, { method: 'DELETE' }),

  // 设置
  getSettings: () => request<{ settings: Settings }>('/settings'),
  saveSettings: (s: Settings) =>
    request<{ settings: Settings }>('/settings', { method: 'PUT', body: JSON.stringify(s) }),
  getLog: () => request<{ size: number; content: string; exists: boolean }>('/log'),
  clearLog: () => request<{ ok: boolean }>('/log', { method: 'DELETE' }),
  getVideoCacheInfo: () => request<{ count: number; size: number }>('/video-cache'),
  clearVideoCache: () => request<{ ok: boolean }>('/video-cache', { method: 'DELETE' }),

  // ===== C-01：EDL 项目（03 §4）=====
  // 列表响应同时含 items（服务端 03 §4.2 契约）与 projects/total 别名，任取其一
  listProjects: () =>
    request<{ items?: ProjectSummary[]; projects?: ProjectSummary[]; total?: number }>(
      '/edl/projects'
    ),
  createProject: (body: ProjectCreateRequest) =>
    request<Project>('/edl/projects', { method: 'POST', body: JSON.stringify(body) }),
  getProject: (id: string) => request<Project>('/edl/projects/' + encodeURIComponent(id)),
  updateProject: (id: string, body: ProjectUpdateRequest) =>
    request<ProjectUpdateResult>('/edl/projects/' + encodeURIComponent(id), {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteProject: (id: string) =>
    request<{ ok: boolean }>('/edl/projects/' + encodeURIComponent(id), { method: 'DELETE' }),
  renderProject: (id: string, body: RenderRequest) =>
    request<RenderSubmitResult>('/edl/projects/' + encodeURIComponent(id) + '/render', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  // ===== C-01：预览网关（04 §2.2~§2.4、§3.2）=====
  /** 申请预览票据；非一次性，绑定 path + 来源 IP，有效期 300s */
  createStreamTicket: (body: StreamTicketRequest) =>
    request<StreamTicket>('/stream/ticket', { method: 'POST', body: JSON.stringify(body) }),
  /** 预览流地址（video.src / audio.src，支持 Range 拖拽） */
  streamUrl: (path: string, ticket: string, root: MediaRoot = 'src') =>
    BASE +
    '/stream?path=' +
    encodeURIComponent(path) +
    '&ticket=' +
    encodeURIComponent(ticket) +
    '&root=' +
    root,
  /**
   * 抽帧缩略图地址（04 §2.4）：t 为**毫秒**
   * @param tMs 抽帧时间点（毫秒）
   * @param root 取帧来源：src 源文件 | proxy 代理 | dest 产物
   */
  thumbUrl: (path: string, tMs: number, root: MediaRoot = 'src', ticket?: string) =>
    BASE +
    '/thumb?path=' +
    encodeURIComponent(path) +
    '&t=' +
    Math.max(0, Math.round(tMs)) +
    '&root=' +
    root +
    (ticket ? '&ticket=' + encodeURIComponent(ticket) : ''),
  /** 下发代理生成任务（taskType=GEN_PROXY），完成后由 WS proxy_ready 通知 */
  createProxy: (body: ProxyRequest) =>
    request<ProxyRequestResult>('/proxy', { method: 'POST', body: JSON.stringify(body) }),
}
