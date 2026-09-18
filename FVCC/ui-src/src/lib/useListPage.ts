// P2-2：列表页公共流程（订阅 store → 拉数据 → 渲染 → 错误处理 → refresh）
// 只依赖新契约 ApiError（api.ts 已兜底转换旧格式），禁止在公共层出现 body.error 判断
import { toast } from '../ui'
import { ApiError } from '../api'

export interface UseListPageOptions<T> {
  load: () => Promise<T[]>
  render: (data: T[]) => void
  /** 数据变更订阅（store.subscribe），返回取消函数 */
  subscribe?: (cb: () => void) => () => void
  /** 首次加载错误提示文案 */
  errorLabel?: string
}

export function useListPage<T>(opts: UseListPageOptions<T>) {
  let unsub: (() => void) | null = null
  let disposed = false

  async function refresh() {
    try {
      const data = await opts.load()
      if (!disposed) opts.render(data)
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e)
      toast(`${opts.errorLabel ?? '加载'}失败：${msg}`, 'error')
    }
  }

  function mount() {
    if (opts.subscribe) unsub = opts.subscribe(() => void refresh())
    void refresh()
  }
  function dispose() {
    disposed = true
    unsub?.()
    unsub = null
  }
  return { mount, dispose, refresh }
}
