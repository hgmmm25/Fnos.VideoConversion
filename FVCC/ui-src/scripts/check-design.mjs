#!/usr/bin/env node
/**
 * FVCC 设计契约门禁（DESIGN.md named rules 的机器化子集）
 *
 * 规则：
 *  R1 禁止硬编码 hex（src/style.css 的 --ansi-* 声明为登记豁免）
 *  R2 禁止 Tailwind 原色板（slate/blue/red/... 带数字色阶）
 *  R3 禁止默认态阴影（shadow-sm/md/lg/shadow；shadow-xl 仅限弹层，允许）
 *  R4 禁止 <12px 文字（text-[Npx]，N<12）
 *  R5 禁止 max-height 过渡（transition/animation/keyframes 使用 max-height）
 *
 * 范围（增量优先，避免存量一次性大量告警）：
 *  --all                全量扫描 src/ 下全部 .ts/.tsx/.css/.html
 *  --files a b c        仅扫描显式文件
 *  默认                  git diff --name-only HEAD（仓库根自动探测），过滤 ui-src/src；
 *                       无 git 或 diff 为空时输出提示并以 0 退出（无增量可查）
 *
 * 用法：
 *  node scripts/check-design.mjs --all
 *  node scripts/check-design.mjs --files src/pages/tasks.ts
 *  node scripts/check-design.mjs            # 增量（build 集成用）
 */
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const UI_SRC = resolve(__dirname, '..')
const SRC = join(UI_SRC, 'src')
const EXT = /\.(ts|tsx|css|html)$/

const args = process.argv.slice(2)
const flagIndex = args.indexOf('--all')
const filesFlag = args.indexOf('--files')
let targets = []
if (flagIndex >= 0) {
  targets = collectAll()
} else if (filesFlag >= 0) {
  targets = args.slice(filesFlag + 1).map((p) => resolve(UI_SRC, p))
} else {
  targets = gitChanged()
}

/** 全量收集 src 下目标文件（排除 *.d.ts 与 node_modules） */
function collectAll() {
  const out = []
  const walk = (dir) => {
    const entries = readdirSync(dir, { withFileTypes: true })
    for (const e of entries) {
      const p = join(dir, e.name)
      if (e.isDirectory()) {
        if (e.name === 'node_modules' || e.name === '.git') continue
        walk(p)
      } else if (EXT.test(e.name) && !e.name.endsWith('.d.ts')) {
        out.push(p)
      }
    }
  }
  walk(SRC)
  return out
}

/** git diff --name-only HEAD + 未跟踪文件，映射回绝对路径；无仓库返回 [] */
function gitChanged() {
  try {
    const root = execFileSync('git', ['rev-parse', '--show-toplevel'], {
      cwd: UI_SRC,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim()
    const out = execFileSync(
      'git',
      ['diff', '--name-only', '--diff-filter=ACMR', 'HEAD', '--', 'FVCC/ui-src/src'],
      { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }
    )
    // 未跟踪文件（新增未 add）也纳入增量范围，避免门禁空转
    let untracked = ''
    try {
      untracked = execFileSync(
        'git',
        ['ls-files', '--others', '--exclude-standard', '--', 'FVCC/ui-src/src'],
        { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }
      )
    } catch {
      /* 忽略 */
    }
    const files = (out + '\n' + untracked)
      .split('\n').map((l) => l.trim()).filter(Boolean).map((p) => join(root, p))
    return files.filter((p) => existsSync(p) && EXT.test(p) && !p.endsWith('.d.ts'))
  } catch {
    return []
  }
}

/** 剥离注释：// 、/* *​/ （CSS/TS）、<!-- --> （HTML）。返回行 -> 去注释后文本 */
function stripComments(text) {
  // 块注释（CSS/TS/HTML）与行注释（//，忽略 http:// 等）
  return text
    .replace(/<!--[\s\S]*?-->/g, ' ')
    .replace(/\/\*[\s\S]*?\*\//g, ' ')
    .replace(/(^|[^:"'`])\/\/[^\n]*/g, '$1')
}

const rules = []

function reg(name, re, fix) {
  rules.push({ name, re, fix })
}

// R1 hex（.css 中 --ansi- 声明行豁免；ID 选择器形态 #letter... 豁免）
reg('R1-hex', /#[0-9a-fA-F]{3,8}(?![0-9a-fA-F])/g, '改用 --c-* 令牌 / tailwind token 类；日志 ANSI 语义色仅允许在 style.css 的 --ansi-* 声明中登记')

// R2 Tailwind 原色板（token 色 primary/accent/danger/... 不在列表中；bg-black/white 中性允许）
reg(
  'R2-tailwind-palette',
  /\b(?:bg|text|border|ring|ring-offset|outline|fill|stroke|from|to|via|divide|accent|caret|decoration|placeholder|shadow)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{1,3}\b/g,
  '改用 --c-* 令牌映射类（primary/signal/success/danger/warning/neutral/ink/...）'
)

// R3 默认态阴影（shadow / shadow-sm / shadow-md；shadow-lg/xl 仅限弹层/抽屉等浮动层，允许）
reg('R3-default-shadow', /\bshadow(?:-(?:sm|md))?(?!-|[\w])/g, '默认态无阴影（明度分层代替）；仅弹层/抽屉可保留 shadow-lg/shadow-xl')

// R4 <12px 文字
reg('R4-small-text', /text-\[(\d+(?:\.\d+)?)px\]/g, '禁止 <12px 文字：改 text-xs（12px）或更大的字阶')

// R5 max-height 过渡（transition/animation 使用 max-height；keyframes 内 max-height）
reg('R5-maxheight-transition', /(?:transition|animation)\s*:[^;}]{0,80}max-height|max-height[^;}]{0,80}(?:transition|animation)|@keyframes[^{}]*\{[^{}]*max-height[^{}]*\}/g, '动画只动 transform/opacity；展开收起用 grid-template-rows 0fr→1fr 或 transform')

const isCss = (p) => p.endsWith('.css')

function scanFile(absPath) {
  const raw = readFileSync(absPath, 'utf8')
  const text = stripComments(raw)
  const hits = []
  const lines = text.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    for (const r of rules) {
      r.re.lastIndex = 0
      let m
      while ((m = r.re.exec(line)) !== null) {
        // .css 的 --ansi-* 声明行豁免 R1
        if (r.name === 'R1-hex' && isCss(absPath) && /^\s*--ansi-/.test(raw.split('\n')[i])) continue
        // .css 中 ID 选择器（# 后字母开头）豁免 R1
        if (r.name === 'R1-hex' && isCss(absPath)) {
          const before = line.slice(0, m.index)
          if (/(^|[\s,{>])\s*#[a-zA-Z][\w-]*$/.test(before)) continue
        }
        hits.push({ line: i + 1, rule: r.name, match: m[0], fix: r.fix })
        break
      }
    }
  }
  return hits
}

const violations = []
for (const t of targets) {
  if (!existsSync(t)) {
    console.warn(`[check-design] 跳过（不存在）: ${t}`)
    continue
  }
  const hits = scanFile(t)
  for (const h of hits) {
    violations.push({ file: t, ...h })
  }
}

if (targets.length === 0) {
  console.log('[check-design] 无增量文件（git diff 为空或非仓库），跳过校验。全量运行：node scripts/check-design.mjs --all')
  process.exit(0)
}

if (violations.length > 0) {
  console.error(`[check-design] 发现 ${violations.length} 处设计契约违规：`)
  for (const v of violations) {
    console.error(`  ${relative(UI_SRC, v.file)}:${v.line}  [${v.rule}] ${v.match}\n       → ${v.fix}`)
  }
  process.exit(1)
}

console.log(`[check-design] 通过：${targets.length} 个文件，无违规。`)
