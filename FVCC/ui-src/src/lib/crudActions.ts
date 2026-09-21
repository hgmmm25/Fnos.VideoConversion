// P2-2：CRUD 四件套统一封装（confirmDialog → api 调用 → toast → reload）
// 只依赖新契约 ApiError（api.ts 已兜底转换旧格式），禁止在公共层出现 body.error 判断
import { confirmDialog, toast } from '../ui'
import { ApiError } from '../api'

export interface CrudActionsOptions<T> {
  /** 删除确认弹窗文案；返回 false 则不弹窗直接取消 */
  confirmTitle: (item: T) => string
  /** 危险操作（删除/不可逆）为 true 时弹窗走 danger 样式 */
  danger?: boolean
  /** 是否弹确认框：默认仅 delete 弹；cancel/stop 等需要确认的操作可强制开启 */
  confirmFor?: (action: 'create' | 'update' | 'delete') => boolean
  /** 实际 API 调用：action 为 'create'|'update'|'delete' */
  apiCall: (action: 'create' | 'update' | 'delete', item: T) => Promise<unknown>
  /** 成功后的数据刷新（一般传 store.loadXxx） */
  reload: () => void | Promise<void>
  /** 成功文案模板，默认 `${action === 'delete' ? '删除' : action === 'create' ? '创建' : '更新'}成功` */
  successMsg?: (action: 'create' | 'update' | 'delete', item: T) => string
}

function defaultMsg(action: 'create' | 'update' | 'delete'): string {
  if (action === 'delete') return '删除成功'
  return action === 'create' ? '创建成功' : '更新成功'
}

export function crudActions<T>(opts: CrudActionsOptions<T>) {
  const shouldConfirm = opts.confirmFor ?? ((action: 'create' | 'update' | 'delete') => action === 'delete')

  const run = async (action: 'create' | 'update' | 'delete', item: T) => {
    if (shouldConfirm(action)) {
      const ok = await confirmDialog(opts.confirmTitle(item), '确认操作', opts.danger ?? true)
      if (!ok) return
    }
    try {
      await opts.apiCall(action, item)
      toast(opts.successMsg ? opts.successMsg(action, item) : defaultMsg(action), 'success')
      await opts.reload()
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e)
      toast(msg, 'error')
    }
  }
  return {
    create: (item: T) => run('create', item),
    update: (item: T) => run('update', item),
    remove: (item: T) => run('delete', item),
  }
}
