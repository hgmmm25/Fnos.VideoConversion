import { api } from '../api'
import { store } from '../store'
import { el, toast, svgIcon, confirmDialog, skeletonRows, setupDialogAccessibility } from '../ui'
import { type Settings, type AppInfo, LOG_LEVELS } from '../types'
import { themeOptions, getCurrentTheme, setTheme } from '../theme'

export function renderSettings(container: HTMLElement) {
  const wrap = el('div', { class: 'flex flex-col h-full p-4 gap-3' })
  const header = el('div', { class: 'flex items-center gap-2' }, [
    el('h2', { class: 'text-lg font-semibold' }, ['应用设置']),
  ])
  const body = el('div', { class: 'flex-1 overflow-auto' })
  wrap.append(header, body)
  container.appendChild(wrap)

  let settings: Settings | null = null
  let info: AppInfo | null = null

  const load = async () => {
    body.innerHTML = ''
    // P2-1：表单加载改用骨架屏（替代纯文本"加载中..."）
    body.appendChild(
      el('div', { class: 'card p-4 max-w-3xl mx-auto w-full space-y-4' }, skeletonRows(6, 'h-10'))
    )
    try {
      const [sr, ir] = await Promise.all([api.getSettings(), api.info()])
      settings = sr.settings
      info = ir
      // 2026-09-16 修复：设置加载后同步到全局 store，剪辑页据此刷新授权目录
      store.applySettings(settings)
      renderForm()
    } catch (e) {
      body.innerHTML = ''
      body.appendChild(el('div', { class: 'text-sm text-danger' }, [(e as Error).message]))
    }
  }

  function renderForm() {
    if (!settings) return
    body.innerHTML = ''
    const s = settings!

    const form = el('div', { class: 'card p-4 max-w-3xl mx-auto w-full' })

    const field = (label: string, control: HTMLElement, hint?: string) => {
      const children: Node[] = [el('label', { class: 'block text-sm mb-1' }, [label]), control]
      if (hint) children.push(el('div', { class: 'text-xs text-ink-muted mt-1' }, [hint]))
      return el('div', { class: 'mb-3' }, children)
    }

    const row = (a: HTMLElement, b: HTMLElement) =>
      el('div', { class: 'grid grid-cols-1 sm:grid-cols-2 gap-3' }, [a, b])

    // 外观（主题切换）
    const themeSel = el('select', { class: 'input' }) as HTMLSelectElement
    for (const t of themeOptions) {
      const opt = el('option', { value: t.id }, [t.name])
      if (t.id === getCurrentTheme()) (opt as HTMLOptionElement).selected = true
      themeSel.appendChild(opt)
    }
    themeSel.onchange = () => {
      setTheme(themeSel.value)
      toast('主题已切换', 'success')
    }

    // 调度与传输
    const intervalInput = el('input', {
      type: 'number', class: 'input', min: '1', value: String(s.schedulerIntervalSec),
    }) as HTMLInputElement
    const chunkInput = el('input', {
      type: 'number', class: 'input', min: '1', value: String(s.chunkSizeMB),
    }) as HTMLInputElement
    const retryInput = el('input', {
      type: 'number', class: 'input', min: '0', value: String(s.maxRetry),
    }) as HTMLInputElement
    const historyInput = el('input', {
      type: 'number', class: 'input', min: '100', value: String(s.historyLimit),
    }) as HTMLInputElement

    

    // 传输模式
    const transferModeGroup = el('div', { class: 'flex gap-4' })
    const httpModeBtn = el('label', { class: 'flex items-center gap-2 cursor-pointer' }, [
      el('input', { type: 'radio', name: 'transferMode', value: 'http', checked: s.transferMode === 'http' }) as HTMLInputElement,
      el('span', {}, ['转存 模式']),
    ])
    const smbModeBtn = el('label', { class: 'flex items-center gap-2 cursor-pointer' }, [
      el('input', { type: 'radio', name: 'transferMode', value: 'smb', checked: s.transferMode === 'smb' }) as HTMLInputElement,
      el('span', {}, ['SMB 模式']),
    ])
    transferModeGroup.append(httpModeBtn, smbModeBtn)

    // SMB设置（SMB模式直接读取fnOS用户共享，无需输入共享路径）
    const smbUserInput = el('input', {
      class: 'input', value: s.smbUser || '', placeholder: 'SMB用户名',
    }) as HTMLInputElement
    const smbPasswordInput = el('input', {
      type: 'password', class: 'input', value: '', placeholder: '已保存，留空则不修改',
    }) as HTMLInputElement

    const smbSettings = el('div', { class: 'mt-3 space-y-3' }, [
      field('SMB用户名', smbUserInput, 'fnOS服务器上具有共享文件夹访问权限的用户名'),
      field('SMB密码', smbPasswordInput, '对应fnOS用户的密码，用于读取共享文件夹配置'),
    ])

    if (s.transferMode !== 'smb') {
      smbSettings.style.display = 'none'
    }

    httpModeBtn.onclick = () => {
      smbSettings.style.display = 'none'
    }
    smbModeBtn.onclick = () => {
      smbSettings.style.display = 'block'
    }

    // 本地转码设置
    const maxLocalTranscodeInput = el('input', {
      type: 'number', class: 'input', min: '1', max: '10', value: String(s.maxLocalTranscodeCount || 1),
    }) as HTMLInputElement

    form.append(
      el('div', { class: 'bg-surface-alt rounded-lg p-3 my-3' }, [
        el('div', { class: 'text-sm font-medium mb-2' }, ['外观']),
        field('主题配色', themeSel, '选择界面的主题颜色方案'),
      ]),
      el('div', { class: 'bg-surface-alt rounded-lg p-3 my-3' }, [
        el('div', { class: 'text-sm font-medium mb-2' }, ['调度与传输']),
        row(
          field('调度轮询间隔（秒）', intervalInput, '调度器检查任务状态的频率，1-2 秒为宜'),
          field('上传分片大小（MB）', chunkInput, '断点续传分片大小，2-8 MB 为宜'),
        ),
        row(
          field('最大重试次数', retryInput, '任务失败后自动重试的最大次数'),
          field('最大并发转码数', maxLocalTranscodeInput, '同时进行本地转码的任务数量，建议不超过CPU核心数'),
          // field('历史记录保留条数', historyInput, '超过此条数时自动清理最早记录'),
        ),
      ]),
      // el('div', { class: 'border-t border-line my-3 pt-3' }, [
      //   el('div', { class: 'text-sm font-medium mb-2' }, ['本地转码设置']),
      //   field('最大并发转码数', maxLocalTranscodeInput, '同时进行本地转码的任务数量，建议不超过CPU核心数'),
      // ]),
      el('div', { class: 'bg-surface-alt rounded-lg p-3 my-3' }, [
        el('div', { class: 'text-sm font-medium mb-2' }, ['传输模式']),
        field('文件传输方式', transferModeGroup, '传输模式：上传文件到服务端硬盘处理；SMB模式：服务端直接读写fnOS共享目录，无需上传'),
        smbSettings,
      ]),
      
    )

    // 系统信息（只读展示）
    if (info) {
      const accessPathItems = info.accessPaths && info.accessPaths.length > 0
        ? info.accessPaths.map((p: string) => el('div', { class: 'pl-2' }, [p]))
        : [el('div', { class: 'pl-2' }, ['未授权（请在 fnOS 应用设置中授权共享文件夹）'])]
      form.append(
        el('div', { class: 'bg-surface-alt rounded-lg p-3 my-3' }, [
          el('div', { class: 'text-sm font-medium mb-2' }, ['系统信息（只读）']),
          el('div', { class: 'text-xs text-ink-muted space-y-1' }, [
            el('div', {}, [`应用版本: ${info.version}`]),
            el('div', {}, [`运行时: ${info.runtime}`]),
            el('div', {}, [`ffprobe: ${info.ffprobe ? '可用' : '不可用'}`]),
            el('div', {}, ['授权目录:']),
            ...accessPathItems,
          ]),
        ]),
      )
    }

    // 日志管理
    const logSizeEl = el('div', { class: 'text-xs text-ink-muted' }, [
      skeletonRows(1, '0.75rem')[0],
    ])
    const logViewBtn = el('button', { class: 'btn btn-sm mt-2' }, ['查看日志'])
    const logClearBtn = el('button', { class: 'btn btn-sm btn-danger mt-2 ml-2' }, ['清空日志'])
    const logLevelSel = el('select', { class: 'input' }) as HTMLSelectElement
    for (const l of LOG_LEVELS) {
      const opt = el('option', { value: l.value }, [l.label])
      if (l.value === s.logLevel) (opt as HTMLOptionElement).selected = true
      logLevelSel.appendChild(opt)
    }
    logViewBtn.onclick = async () => {
      try {
        const r = await api.getLog()
        const lines = (r.content || '').split('\n').filter(l => l.trim())
        const reversed = lines.reverse().join('\n')
        openLogViewer(reversed)
      } catch (e) {
        toast((e as Error).message, 'error')
      }
    }
    logClearBtn.onclick = async () => {
      if (!(await confirmDialog('确定清空日志？', '清空日志', true))) return
      try {
        await api.clearLog()
        toast('日志已清空', 'success')
        logSizeEl.textContent = '日志大小: 0 KB'
      } catch (e) {
        toast((e as Error).message, 'error')
      }
    }
    form.append(
      el('div', { class: 'bg-surface-alt rounded-lg p-3 my-3' }, [
        el('div', { class: 'text-sm font-medium mb-2' }, ['运行日志']),
        row(
          field('日志记录级别', logLevelSel, '选择记录的日志级别，低级别会包含高级别日志'),
          field('历史记录保留条数', historyInput, '超过此条数时自动清理最早记录'),
        ),
        logSizeEl,
        el('div', {}, [logViewBtn, logClearBtn]),
      ]),
    )

    // 视频缓存管理
    const cacheInfoEl = el('div', { class: 'text-xs text-ink-muted space-y-1' }, [
      ...skeletonRows(2, '0.75rem'),
    ])
    const cacheClearBtn = el('button', { class: 'btn btn-sm btn-danger mt-2' }, ['清空缓存'])
    cacheClearBtn.onclick = async () => {
      if (!(await confirmDialog('确定清空视频信息缓存？', '清空缓存', true))) return
      try {
        await api.clearVideoCache()
        toast('缓存已清空', 'success')
        cacheInfoEl.innerHTML = ''
        cacheInfoEl.appendChild(el('div', {}, ['缓存记录数: 0']))
        cacheInfoEl.appendChild(el('div', {}, ['缓存文件大小: 0 KB']))
      } catch (e) {
        toast((e as Error).message, 'error')
      }
    }
    form.append(
      el('div', { class: 'bg-surface-alt rounded-lg p-3 my-3' }, [
        el('div', { class: 'text-sm font-medium mb-2' }, ['视频信息缓存']),
        cacheInfoEl,
        el('div', {}, [cacheClearBtn]),
      ]),
    )

    // 自动加载日志和缓存信息
    api.getLog().then(r => {
      logSizeEl.textContent = `日志大小: ${(r.size / 1024).toFixed(1)} KB`
    }).catch(() => {
      logSizeEl.textContent = '加载失败'
    })
    api.getVideoCacheInfo().then(r => {
      cacheInfoEl.innerHTML = ''
      cacheInfoEl.appendChild(el('div', {}, [`缓存记录数: ${r.count}`]))
      cacheInfoEl.appendChild(el('div', {}, [`缓存文件大小: ${(r.size / 1024).toFixed(1)} KB`]))
    }).catch(() => {
      cacheInfoEl.textContent = '加载失败'
    })

    // 播放设置（2026-09-16 修复⑤：剪辑页预览器默认静音改为可配置，不再硬编码）
    const mutedChk = el('input', {
      type: 'checkbox', class: 'w-4 h-4', checked: s.playerMuted !== false,
    }) as HTMLInputElement
    form.append(
      el('div', { class: 'bg-surface-alt rounded-lg p-3 my-3' }, [
        el('div', { class: 'text-sm font-medium mb-2' }, ['播放']),
        field(
          '剪辑页预览器默认静音',
          el('label', { class: 'flex items-center gap-2 cursor-pointer' }, [
            mutedChk,
            el('span', { class: 'text-sm' }, ['打开剪辑页时预览器默认处于静音状态']),
          ]),
          '取消勾选则预览器默认开启声音（浏览器可能限制自动播放带声视频，首次播放需手动点击播放按钮）',
        ),
      ]),
    )

    // 操作按钮
    const btns = el('div', { class: 'flex justify-end gap-2 mt-4' })
    const reload = el('button', { class: 'btn flex items-center gap-1.5' }, [])
    reload.append(svgIcon('refresh', 16) as unknown as Node, el('span', {}, ['重新加载']))
    reload.onclick = async () => {
      await load()
      await store.loadInfo()
      toast('已重新加载', 'success')
    }
    const save = el('button', { class: 'btn btn-primary flex items-center gap-1.5' }, [])
    save.append(svgIcon('save', 16) as unknown as Node, el('span', {}, ['保存']))
    save.onclick = async () => {
      if (!settings) return
      const transferMode = (form.querySelector('input[name="transferMode"]:checked') as HTMLInputElement)?.value || 'http'
      const body: Settings = {
        schedulerIntervalSec: Number(intervalInput.value) || 1,
        chunkSizeMB: Number(chunkInput.value) || 4,
        maxRetry: Number(retryInput.value) || 0,
        historyLimit: Number(historyInput.value) || 1000,
        outputSuffix: settings.outputSuffix,
        accessiblePaths: settings.accessiblePaths || [],
        transferMode,
        smbUser: smbUserInput.value.trim(),
        smbPassword: smbPasswordInput.value,
        logLevel: logLevelSel.value,
        maxLocalTranscodeCount: Number(maxLocalTranscodeInput.value) || 1,
        playerMuted: mutedChk.checked,
      }
      try {
        const r = await api.saveSettings(body)
        settings = r.settings
        toast('保存成功，部分设置需重启应用后生效', 'success')
        renderForm()
      } catch (e) {
        toast((e as Error).message, 'error')
      }
    }
    btns.append(reload, save)
    form.appendChild(btns)
    body.appendChild(form)
  }

  function openLogViewer(content: string) {
    const overlay = el('div', { class: 'fixed inset-0 bg-black/50 z-50 flex items-start justify-center p-4 pt-[10vh]' })
    const modal = el('div', { class: 'card w-full max-w-4xl h-[85vh] flex flex-col' })
    const closeBtn = el('button', { class: 'btn btn-sm btn-danger' }, ['关闭'])
    closeBtn.onclick = () => overlay.remove()
    // P2-2：日志查看器补对话框语义（role=dialog / aria-modal / Esc / 焦点陷阱 / 返回焦点）
    const titleEl = el('h3', { class: 'font-medium' }, ['运行日志'])
    const header = el('div', { class: 'flex items-center justify-between p-4 border-b border-line shrink-0' }, [
      titleEl,
      closeBtn,
    ])
    
    // 将ANSI转义码转换为HTML颜色
    const htmlContent = parseAnsiColors(content || '(空)')
    const pre = el('pre', { 
      class: 'flex-1 font-mono text-xs p-4 overflow-auto',
      // P0-1：日志面板底色收敛为主题令牌 --c-log-bg（亮/暗各一套）
      style: 'background-color: var(--c-log-bg); white-space: pre-wrap; word-break: break-all;'
    }) as HTMLPreElement
    pre.innerHTML = htmlContent
    
    modal.append(header, pre)
    overlay.appendChild(modal)
    overlay.onclick = (e) => { if (e.target === overlay) overlay.remove() }
    document.body.appendChild(overlay)
    setupDialogAccessibility({ overlay, titleEl, onClose: () => overlay.remove() })
    closeBtn.focus()
  }
  
  // ANSI颜色代码映射
  // P0-1：ANSI 语义色收敛为 --ansi-* 主题变量（style.css 单一真源，亮/暗各一套），
  // src 内不再出现 #hex 硬编码
  const ansiColorMap: Record<number, string> = {
    0: 'color: inherit',      // 重置
    1: 'font-weight: bold',   // 粗体
    30: 'color: var(--ansi-fg-30)',     // 黑色
    31: 'color: var(--ansi-fg-31)',     // 红色
    32: 'color: var(--ansi-fg-32)',     // 绿色
    33: 'color: var(--ansi-fg-33)',     // 黄色
    34: 'color: var(--ansi-fg-34)',     // 蓝色
    35: 'color: var(--ansi-fg-35)',     // 紫色
    36: 'color: var(--ansi-fg-36)',     // 青色
    37: 'color: var(--ansi-fg-37)',     // 白色
    39: 'color: inherit',     // 默认前景色
    40: 'background-color: var(--ansi-bg-40)', // 黑色背景
    41: 'background-color: var(--ansi-bg-41)', // 红色背景
    42: 'background-color: var(--ansi-bg-42)', // 绿色背景
    43: 'background-color: var(--ansi-bg-43)', // 黄色背景
    44: 'background-color: var(--ansi-bg-44)', // 蓝色背景
    45: 'background-color: var(--ansi-bg-45)', // 紫色背景
    46: 'background-color: var(--ansi-bg-46)', // 青色背景
    47: 'background-color: var(--ansi-bg-47)', // 白色背景
    49: 'background-color: transparent', // 默认背景色
  }
  
  // 解析ANSI转义码并转换为HTML
  function parseAnsiColors(text: string): string {
    // 转义HTML特殊字符
    text = text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
    
    const result: string[] = []
    let currentStyle: string[] = []
    
    const ansiRegex = /\x1b\[([0-9;]*)([a-zA-Z])/g
    let match
    
    let lastIndex = 0
    while ((match = ansiRegex.exec(text)) !== null) {
      // 添加匹配前的普通文本
      if (match.index > lastIndex) {
        if (currentStyle.length > 0) {
          result.push(`<span style="${currentStyle.join('; ')}">${text.substring(lastIndex, match.index)}</span>`)
        } else {
          result.push(text.substring(lastIndex, match.index))
        }
      }
      
      // 处理ANSI代码
      const codes = match[1].split(';').filter(c => c !== '')
      for (const code of codes) {
        const numCode = parseInt(code)
        if (numCode === 0) {
          // 重置所有样式
          currentStyle = []
        } else if (ansiColorMap[numCode]) {
          // 应用颜色样式
          currentStyle.push(ansiColorMap[numCode])
        }
      }
      
      lastIndex = ansiRegex.lastIndex
    }
    
    // 添加剩余的文本
    if (lastIndex < text.length) {
      if (currentStyle.length > 0) {
        result.push(`<span style="${currentStyle.join('; ')}">${text.substring(lastIndex)}</span>`)
      } else {
        result.push(text.substring(lastIndex))
      }
    }
    
    return result.join('')
  }

  load()
}
