// P2-2：按 key 隔离保存/恢复列表滚动位置；renderList/renderEdit 切换时保持位置
const positions = new Map<string, number>()

export function saveScrollPos(el: HTMLElement | null, key: string) {
  if (el) positions.set(key, el.scrollTop)
}
export function restoreScrollPos(el: HTMLElement | null, key: string) {
  if (el) {
    const v = positions.get(key)
    if (v !== undefined) el.scrollTop = v
  }
}
