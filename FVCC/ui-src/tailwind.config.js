/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,js,html}'],
  // P0-1：与 src/theme.ts / index.html 的实际机制对齐 —— 暗色通过
  // document.documentElement 的 data-theme="dark" 属性激活，亮色为移除属性
  // （非 .dark class 切换），故使用 selector 策略而非 class 策略。
  darkMode: ['selector', '[data-theme="dark"]'],
  theme: {
    extend: {
      colors: {
        // 主色
        primary: {
          DEFAULT: 'rgb(var(--c-primary) / <alpha-value>)',
          hover: 'rgb(var(--c-primary-hover) / <alpha-value>)',
          soft: 'rgb(var(--c-primary-soft) / <alpha-value>)',
          'soft-text': 'rgb(var(--c-primary-soft-text) / <alpha-value>)',
        },
        // 成功
        success: {
          DEFAULT: 'rgb(var(--c-success) / <alpha-value>)',
          soft: 'rgb(var(--c-success-soft) / <alpha-value>)',
          'soft-text': 'rgb(var(--c-success-soft-text) / <alpha-value>)',
        },
        // 危险
        danger: {
          DEFAULT: 'rgb(var(--c-danger) / <alpha-value>)',
          hover: 'rgb(var(--c-danger-hover) / <alpha-value>)',
          soft: 'rgb(var(--c-danger-soft) / <alpha-value>)',
          'soft-text': 'rgb(var(--c-danger-soft-text) / <alpha-value>)',
        },
        // 警告
        warning: {
          DEFAULT: 'rgb(var(--c-warning) / <alpha-value>)',
          soft: 'rgb(var(--c-warning-soft) / <alpha-value>)',
          'soft-text': 'rgb(var(--c-warning-soft-text) / <alpha-value>)',
        },
        // P1-1：信号色（进行中/活动态；--c-signal 单一真源）
        signal: {
          DEFAULT: 'rgb(var(--c-signal) / <alpha-value>)',
          soft: 'rgb(var(--c-signal-soft) / <alpha-value>)',
          'soft-text': 'rgb(var(--c-signal-soft-text) / <alpha-value>)',
        },
        // 中性状态
        neutral: {
          DEFAULT: 'rgb(var(--c-neutral) / <alpha-value>)',
          soft: 'rgb(var(--c-neutral-soft) / <alpha-value>)',
          'soft-text': 'rgb(var(--c-neutral-soft-text) / <alpha-value>)',
        },
        // 表面 / 背景
        page: 'rgb(var(--c-page) / <alpha-value>)',
        surface: {
          DEFAULT: 'rgb(var(--c-surface) / <alpha-value>)',
          hover: 'rgb(var(--c-surface-hover) / <alpha-value>)',
          alt: 'rgb(var(--c-surface-alt) / <alpha-value>)',
        },
        line: {
          DEFAULT: 'rgb(var(--c-line) / <alpha-value>)',
          subtle: 'rgb(var(--c-line-subtle) / <alpha-value>)',
        },
        // 文字
        ink: {
          DEFAULT: 'rgb(var(--c-ink) / <alpha-value>)',
          muted: 'rgb(var(--c-ink-muted) / <alpha-value>)',
          subtle: 'rgb(var(--c-ink-subtle) / <alpha-value>)',
        },
        // P0-1：拖拽落点指示线（tasks.ts 排序落点，避免 border-t-2 硬编码色板）
        'drop-line': 'rgb(var(--c-drop-line) / <alpha-value>)',
        // P0-1：预览区恒定深底上的辅助文字（preview.ts 空状态等）
        preview: {
          ink: 'rgb(var(--c-preview-ink) / <alpha-value>)',
        },
        // P1-5：时间线 clip 数据色（类型承载语义，与列表页状态色 signal/success/danger 解耦）
        clip: {
          video: 'rgb(var(--c-clip-video) / <alpha-value>)',
          audio: 'rgb(var(--c-clip-audio) / <alpha-value>)',
          image: 'rgb(var(--c-clip-image) / <alpha-value>)',
          text: 'rgb(var(--c-clip-text) / <alpha-value>)',
          marker: 'rgb(var(--c-clip-marker) / <alpha-value>)',
        },
      },
      borderWidth: {
        // P0-1：拖拽落点线宽 token（style.css --drop-line-width 单一真源）
        drop: 'var(--drop-line-width)',
      },
      transitionTimingFunction: {
        // P0-3：缓动 token（style.css --ease-* 单一真源，动画类统一引用）
        'out-strong': 'var(--ease-out-strong)',
        'in-out-strong': 'var(--ease-in-out-strong)',
      },
      // P1-3：字体族（token 在 style.css :root 定义）
      fontFamily: {
        sans: ['var(--font-sans)'],
        mono: ['var(--font-mono)'],
      },
    },
  },
  plugins: [],
}
