import { store } from '../store'
import { api } from '../api'
import { el, toast, confirmDialog, emptyState, formatTime, svgIcon } from '../ui'
import { type Server } from '../types'

export function renderServers(container: HTMLElement) {
  const wrap = el('div', { class: 'flex flex-col h-full p-4 gap-3' })

  // 视图状态：list=服务器清单，edit=编辑/创建
  let view: 'list' | 'edit' = 'list'
  let currentId: string | null = null

  const render = () => {
    wrap.innerHTML = ''
    if (view === 'list') {
      wrap.appendChild(renderList())
    } else {
      const s = store.servers.find((x) => x.id === currentId)
      wrap.appendChild(renderEdit(s))
    }
  }

  function renderList(): HTMLElement {
    const addBtn = el('button', { class: 'btn btn-primary ml-auto flex items-center gap-1.5' }, [])
    addBtn.append(svgIcon('plus', 16) as unknown as Node, el('span', {}, ['新增服务器']))
    const header = el('div', { class: 'flex items-center' }, [
      el('h2', { class: 'text-lg font-semibold' }, ['转码服务器']),
      addBtn,
    ])
    addBtn.onclick = () => {
      currentId = null
      view = 'edit'
      render()
    }

    const list = el('div', { class: 'flex-1 overflow-auto' })
    // 过滤掉 isLocal 服务器（fnNAS 自转码由创建转码任务时自动提供，无需在此管理）
    const remoteServers = store.servers.filter(s => !s.isLocal)
    if (remoteServers.length === 0) {
      // P2-1：空状态承载"下一步动作"
      list.appendChild(
        emptyState('暂无服务器，请点击右上角「新增服务器」添加', 'server', {
          label: '新增服务器',
          onClick: () => {
            addBtn.click()
          },
        })
      )
    } else {
      const grid = el('div', { class: 'grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3' })
      for (const s of remoteServers) {
        grid.appendChild(serverCard(s))
      }
      list.appendChild(grid)
    }

    return el('div', { class: 'flex flex-col flex-1 gap-3 overflow-hidden' }, [header, list])
  }

  function serverCard(s: Server): HTMLElement {
    const card = el('div', {
      class: 'card p-4 cursor-pointer hover:ring-2 hover:ring-primary/30 transition flex flex-col gap-3',
    })
    const head = el('div', { class: 'flex items-center justify-between' }, [
      el('div', { class: 'flex items-center gap-2' }, [
        el('span', {
          class: `inline-block w-2.5 h-2.5 rounded-full ${s.status === 'online' ? 'bg-success' : 'bg-neutral'}`,
        }),
        el('div', { class: 'font-medium' }, [s.name]),
      ]),
      el('div', { class: 'flex items-center gap-1' }, [
        (() => {
          const editBtn = el('button', { class: 'btn btn-sm' }, [])
          editBtn.append(svgIcon('edit', 14) as unknown as Node)
          editBtn.title = '编辑'
          editBtn.onclick = (e) => {
            e.stopPropagation()
            currentId = s.id
            view = 'edit'
            render()
          }
          return editBtn
        })(),
        (() => {
          const delBtn = el('button', { class: 'btn btn-sm btn-danger' }, [])
          delBtn.append(svgIcon('trash', 14) as unknown as Node)
          delBtn.title = '删除'
          delBtn.onclick = async (e) => {
            e.stopPropagation()
            if (!(await confirmDialog(`确定删除服务器「${s.name}」？`))) return
            try {
              await api.deleteServer(s.id)
              await store.loadServers()
              toast('已删除', 'success')
              render()
            } catch (err) {
              toast((err as Error).message, 'error')
            }
          }
          return delBtn
        })(),
      ]),
    ])
    const info = el('div', { class: 'text-xs text-ink-muted space-y-1' }, [
      el('div', {}, [`地址: ${s.ip}:${s.port}`]),
      el('div', {}, [`状态: ${s.status === 'online' ? '在线' : '离线'}`]),
    ])
    if (s.keyExpireAt) {
      info.appendChild(el('div', { class: 'text-warning' }, [`密钥过期: ${formatTime(s.keyExpireAt)}`]))
    }
    const testBtn = el('button', { class: 'btn btn-sm flex items-center gap-1.5 self-start' }, [])
    testBtn.append(svgIcon('circle', 14) as unknown as Node, el('span', {}, ['测试连接']))
    testBtn.onclick = async (e) => {
      e.stopPropagation()
      testBtn.disabled = true
      try {
        const r = await api.testServer(s.id)
        if (r.ok) toast('连接成功', 'success')
        else toast(`连接失败: ${r.error}`, 'error')
        await store.loadServers()
        render()
      } catch (err) {
        toast((err as Error).message, 'error')
      }
      testBtn.disabled = false
    }
    card.append(head, info, testBtn)
    card.onclick = () => {
      currentId = s.id
      view = 'edit'
      render()
    }
    return card
  }

  function renderEdit(s: Server | undefined): HTMLElement {
    const goBack = () => { view = 'list'; render() }
    const backBtn = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
    backBtn.append(svgIcon('back', 14) as unknown as Node, el('span', {}, ['返回清单']))
    backBtn.onclick = goBack
    const header = el('div', { class: 'flex items-center gap-2' }, [
      backBtn,
      el('h2', { class: 'text-lg font-semibold' }, [s ? '编辑服务器' : '新增服务器']),
    ])
    const body = el('div', { class: 'flex-1 overflow-auto' }, [editForm(s, goBack)])
    return el('div', { class: 'flex flex-col flex-1 gap-3 overflow-hidden' }, [header, body])
  }

  store.subscribe(render)
  render()
  refreshServerStatus()
  container.appendChild(wrap)
}

async function refreshServerStatus() {
  for (const s of store.servers) {
    if (s.isLocal) continue
    try {
      await api.testServer(s.id)
    } catch {
      /* ignore */
    }
  }
  await store.loadServers()
}

function editForm(s: Server | undefined, onBack: () => void): HTMLElement {
  const form = el('div', { class: 'card p-4 max-w-3xl mx-auto w-full' })

  const nameInput = el('input', { class: 'input', placeholder: '服务器名称', value: s?.name ?? '' }) as HTMLInputElement
  const ipInput = el('input', { class: 'input', placeholder: 'IP 地址，如 192.168.1.100', value: s?.ip ?? '' }) as HTMLInputElement
  const portInput = el('input', { type: 'number', class: 'input', placeholder: '端口', value: s ? String(s.port) : '8080' }) as HTMLInputElement
  const keyInput = el('input', { type: 'password', class: 'input', placeholder: '已保存，留空则不修改', value: '' }) as HTMLInputElement
  const lockInput = el('input', { type: 'number', class: 'input', placeholder: '锁自动过期(秒)', value: s ? String(s.lockExpireSec || 120) : '120' }) as HTMLInputElement

  const field = (label: string, control: HTMLElement, description?: string) => {
    const children: (string | Node)[] = [el('label', { class: 'block text-sm mb-1' }, [label]), control]
    if (description) {
      children.push(el('div', { class: 'text-xs text-ink-muted mt-1' }, [description]))
    }
    return el('div', { class: 'mb-3' }, children)
  }

  const row = (a: HTMLElement, b: HTMLElement) =>
    el('div', { class: 'grid grid-cols-1 sm:grid-cols-2 gap-3' }, [a, b])

  const connectionSection = el('div', { class: 'border-t border-line my-3 pt-3' }, [
    el('div', { class: 'text-sm font-medium mb-2' }, ['连接信息']),
    row(field('IP 地址 *', ipInput), field('端口', portInput)),
  ])

  const authSection = el('div', { class: 'border-t border-line my-3 pt-3' }, [
    el('div', { class: 'text-sm font-medium mb-2' }, ['认证与锁']),
    field('API 密钥', keyInput),
    field('锁自动过期（秒）', lockInput),
  ])

  form.append(
    field('服务器名称 *', nameInput),
    connectionSection,
    authSection,
  )

  const btns = el('div', { class: 'flex justify-end gap-2 mt-4 pt-3 border-t border-line' })
  if (s) {
    const del = el('button', { class: 'btn btn-danger mr-auto flex items-center gap-1.5' }, [])
    del.append(svgIcon('trash', 14) as unknown as Node, el('span', {}, ['删除']))
    del.onclick = async () => {
      if (!(await confirmDialog(`确定删除服务器「${s.name}」？`))) return
      try {
        await api.deleteServer(s.id)
        await store.loadServers()
        toast('已删除', 'success')
        onBack()
      } catch (e) {
        toast((e as Error).message, 'error')
      }
    }
    btns.append(del)
  }
  const cancel = el('button', { class: 'btn' }, ['取消'])
  cancel.onclick = onBack
  const save = el('button', { class: 'btn btn-primary flex items-center gap-1.5' }, [])
  save.append(svgIcon('save', 16) as unknown as Node, el('span', {}, ['保存']))
  save.onclick = async () => {
    const body = {
      name: nameInput.value.trim(),
      ip: ipInput.value.trim(),
      port: Number(portInput.value) || 8080,
      authKey: keyInput.value.trim(),
      lockExpireSec: Number(lockInput.value) || 120,
      isLocal: false,
    }
    if (!body.name) {
      toast('请填写服务器名称')
      return
    }
    if (!body.ip) {
      toast('请填写 IP 地址')
      return
    }
    try {
      if (s) await api.updateServer(s.id, { ...s, ...body })
      else await api.createServer(body)
      await store.loadServers()
      toast('保存成功', 'success')
      onBack()
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }
  btns.append(cancel, save)
  form.appendChild(btns)
  return form
}
