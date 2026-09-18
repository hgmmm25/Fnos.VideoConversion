// 浏览器 WebSocket 客户端：接收任务状态推送，自动重连。

type Listener = (msg: any) => void
type StatusListener = (connected: boolean) => void

class WSClient {
  private ws: WebSocket | null = null
  private listeners = new Set<Listener>()
  private statusListeners = new Set<StatusListener>()
  private reconnectTimer: number | null = null
  private closed = false
  /** P1-4：当前连接状态（供全局状态条订阅，不随页面销毁） */
  private connected = false

  /** 当前是否已建立 WS 连接 */
  isConnected(): boolean {
    return this.connected
  }

  /** 订阅连接状态变化（返回取消订阅函数） */
  onStatusChange(listener: StatusListener): () => void {
    this.statusListeners.add(listener)
    return () => this.statusListeners.delete(listener)
  }

  private setConnected(v: boolean) {
    if (this.connected === v) return
    this.connected = v
    this.statusListeners.forEach((l) => {
      try {
        l(v)
      } catch (e) {
        console.warn('[ws] status handler', e)
      }
    })
  }

  connect() {
    if (this.ws) return
    this.closed = false
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${location.host}/app/fvcc/ws`
    this.ws = new WebSocket(url)
    this.ws.onopen = () => {
      console.log('[ws] connected')
      this.setConnected(true)
    }
    this.ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data)
        this.listeners.forEach((l) => l(msg))
      } catch (e) {
        console.warn('[ws] parse error', e)
      }
    }
    this.ws.onclose = () => {
      this.ws = null
      this.setConnected(false)
      if (!this.closed) {
        console.log('[ws] disconnected, reconnect in 3s')
        this.reconnectTimer = window.setTimeout(() => this.connect(), 3000)
      }
    }
    this.ws.onerror = () => this.ws?.close()
  }

  on(listener: Listener) {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  close() {
    this.closed = true
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.ws?.close()
  }
}

export const ws = new WSClient()
