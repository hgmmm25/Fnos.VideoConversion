import { defineConfig } from 'vite'
import { resolve } from 'path'

// 生产环境：前端由 Go 后端托管，资源统一前缀 /app/fvcc
// 开发环境：Vite 代理 /app/fvcc/api 与 /app/fvcc/ws 到 Go 后端 (127.0.0.1:8088)
export default defineConfig({
  base: '/app/fvcc/',
  resolve: {
    alias: { '@': resolve(import.meta.dirname, 'src') },
  },
  build: {
    outDir: '../app/ui',
    emptyOutDir: true,
    assetsDir: 'assets',
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash].[ext]',
      },
    },
  },
  server: {
    port: 5180,
    proxy: {
      '/app/fvcc/api': { target: 'http://127.0.0.1:8088', changeOrigin: true },
      '/app/fvcc/ws': { target: 'ws://127.0.0.1:8088', ws: true, changeOrigin: true },
    },
  },
})
