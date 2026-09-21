// P2-2：表单 builder——生成 label+input 行、校验规则、取值/回填
// 先服务 settings.ts / profiles.ts 两个表单最重页面
// 无障碍（07 §3）：checkbox 以 label 包裹实现关联；普通字段 label 紧随 control，保持与页面原 field() 一致的语义层级
import { el } from '../ui'

export interface FieldDef<T> {
  key: keyof T & string
  label: string
  type?: 'text' | 'number' | 'password' | 'select' | 'checkbox'
  options?: { label: string; value: string }[]
  placeholder?: string
  required?: boolean
  /** 数字输入可选约束 */
  min?: string | number
  max?: string | number
  step?: string | number
  /** 校验函数，返回错误文案；空串表示通过 */
  validate?: (value: string, all: T) => string
  help?: string
}

export function formBuilder<T extends Record<string, unknown>>(fields: FieldDef<T>[]) {
  const inputs = new Map<keyof T, HTMLElement>()
  const rows = new Map<keyof T, HTMLElement>()

  function makeRow(fd: FieldDef<T>): HTMLElement {
    const baseAttrs: Record<string, string> = { class: 'input' }
    if (fd.placeholder) baseAttrs.placeholder = fd.placeholder

    if (fd.type === 'checkbox') {
      const cb = el('input', { type: 'checkbox' }) as HTMLInputElement
      inputs.set(fd.key, cb)
      const rowEl = el('div', { class: 'mb-3' }, [
        el('label', { class: 'flex items-center gap-2 cursor-pointer' }, [cb, el('span', {}, [fd.label])]),
      ])
      if (fd.help) rowEl.appendChild(el('div', { class: 'text-xs text-ink-muted mt-1' }, [fd.help]))
      rows.set(fd.key, rowEl)
      return rowEl
    }

    let control: HTMLElement
    if (fd.type === 'select') {
      const sel = el('select', baseAttrs) as HTMLSelectElement
      for (const o of fd.options ?? []) {
        sel.appendChild(el('option', { value: o.value }, [o.label]))
      }
      control = sel
    } else {
      const type = fd.type === 'password' ? 'password' : fd.type === 'number' ? 'number' : 'text'
      const inp = el('input', { ...baseAttrs, type }) as HTMLInputElement
      if (fd.type === 'number') {
        if (fd.min !== undefined) inp.min = String(fd.min)
        if (fd.max !== undefined) inp.max = String(fd.max)
        if (fd.step !== undefined) inp.step = String(fd.step)
      }
      control = inp
    }
    inputs.set(fd.key, control)

    const children: (string | Node)[] = [el('label', { class: 'block text-sm mb-1' }, [fd.label]), control]
    if (fd.help) children.push(el('div', { class: 'text-xs text-ink-muted mt-1' }, [fd.help]))
    const rowEl = el('div', { class: 'mb-3' }, children)
    rows.set(fd.key, rowEl)
    return rowEl
  }

  for (const fd of fields) makeRow(fd)

  /** 返回字段行容器；传 key 返回单字段行，不传返回全部字段行 */
  function elOf(key?: keyof T & string): HTMLElement {
    if (key !== undefined) {
      const r = rows.get(key)
      if (r) return r
    }
    const wrap = el('div', {})
    for (const fd of fields) {
      wrap.appendChild(rows.get(fd.key)!)
    }
    return wrap
  }

  function collect(): Partial<T> {
    const out: Record<string, unknown> = {}
    for (const fd of fields) {
      const c = inputs.get(fd.key)!
      if (fd.type === 'checkbox') {
        out[fd.key] = (c as HTMLInputElement).checked
      } else if (fd.type === 'number') {
        const v = (c as HTMLInputElement).value
        out[fd.key] = v === '' ? undefined : Number(v)
      } else {
        out[fd.key] = (c as HTMLInputElement).value
      }
    }
    return out as Partial<T>
  }

  function fill(data: Partial<T>) {
    for (const fd of fields) {
      const c = inputs.get(fd.key)!
      const v = data[fd.key]
      if (fd.type === 'checkbox') {
        ;(c as HTMLInputElement).checked = Boolean(v)
      } else {
        ;(c as HTMLInputElement).value = v === undefined || v === null ? '' : String(v)
      }
    }
  }

  function validate(): string[] {
    const errs: string[] = []
    for (const fd of fields) {
      const c = inputs.get(fd.key)!
      const v = (c as HTMLInputElement).value
      if (fd.required && !v.trim()) errs.push(`${fd.label} 为必填项`)
      if (fd.validate) {
        const r = fd.validate(v, collect() as T)
        if (r) errs.push(r)
      }
    }
    return errs
  }

  /** 按 key 取内部控件引用（浏览按钮、联动等场景需要外部写入 value） */
  function get(key: keyof T): HTMLElement | undefined {
    return inputs.get(key)
  }

  return { el: elOf, collect, fill, validate, get }
}
