// 主题配色管理
// 所有颜色变量定义在 style.css 中（:root 为明亮主题，[data-theme="dark"] 为暗黑主题）
// 本模块负责主题切换、持久化，以及维护可选主题列表
// 扩展新主题步骤：1) 在 style.css 添加 [data-theme="xxx"] 颜色覆盖；2) 在下方 themeOptions 与 VALID_THEMES 注册

export interface ThemeOption {
  id: string
  name: string
}

const STORAGE_KEY = 'fvcc-theme'

/** 已注册的主题列表（新增主题在此追加即可） */
export const themeOptions: ThemeOption[] = [
  { id: 'auto', name: '跟随系统' },
  { id: 'light', name: '明亮' },
  { id: 'dark', name: '暗黑' },
]

const VALID_THEMES = themeOptions.map((t) => t.id)

export function getCurrentTheme(): string {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored && VALID_THEMES.includes(stored)) return stored
  return 'auto'
}

function getSystemTheme(): string {
  const fnosMode = localStorage.getItem('fnos-theme-mode')
  if (fnosMode === '20') return 'dark'
  if (fnosMode === '10') return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function setTheme(themeId: string) {
  if (!VALID_THEMES.includes(themeId)) return
  localStorage.setItem(STORAGE_KEY, themeId)
  const effectiveTheme = themeId === 'auto' ? getSystemTheme() : themeId
  if (effectiveTheme === 'dark') {
    document.documentElement.setAttribute('data-theme', 'dark')
  } else {
    document.documentElement.removeAttribute('data-theme')
  }
}

/** 在应用启动时调用，恢复用户上次选择的主题 */
export function initTheme() {
  setTheme(getCurrentTheme())
  
  const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
  mediaQuery.addEventListener('change', () => {
    if (getCurrentTheme() === 'auto') {
      setTheme('auto')
    }
  })
  
  window.addEventListener('storage', (e) => {
    if (e.key === 'fnos-theme-mode' && getCurrentTheme() === 'auto') {
      setTheme('auto')
    }
  })
}
