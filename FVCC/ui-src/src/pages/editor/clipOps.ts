// C-05/C-06/C-07 共用：时间线片段构造（01 §4.2 整段添加 / §4.3 打点添加）
// 统一在此处做上限校验与提示，避免素材面板、时间线工具条、预览器三处重复实现。
import { toast } from '../../ui'
import type { EDLClip } from '../../types'
import type { EditorStore } from './editorStore'
import { EDL_LIMITS, clipDurationMs, newClipId } from './format'

/** 供构造片段的最小素材视图（AssetRef 与 PreviewAsset 均可满足） */
export interface ClipSource {
  assetId: string
  file: string
  durationMs: number
}

/** 由素材 + 入出点构造片段；入出点向 0 取整，始终写整数毫秒（03 §2.1） */
export function buildClip(asset: ClipSource, inMs: number, outMs: number): EDLClip {
  return {
    clipId: newClipId(),
    assetId: asset.assetId,
    file: asset.file,
    inMs: Math.max(0, Math.round(inMs)),
    outMs: Math.max(0, Math.round(outMs)),
    speed: 1.0,
    sourceDurationMs: Math.max(0, Math.round(asset.durationMs)),
    transition: null,
  }
}

interface Result {
  ok: boolean
  clipId?: string
  reason?: string
}

/** 整段添加（入点 0 → 出点取源时长） */
export function addWholeClip(edlStore: EditorStore, asset: ClipSource, atIndex?: number): Result {
  return addClipRange(edlStore, asset, 0, asset.durationMs, atIndex)
}

/** 按打点区间添加（02 §7.3） */
export function addRangeClip(
  edlStore: EditorStore,
  asset: ClipSource,
  inMs: number,
  outMs: number,
  atIndex?: number
): Result {
  return addClipRange(edlStore, asset, inMs, outMs, atIndex)
}

function addClipRange(
  edlStore: EditorStore,
  asset: ClipSource,
  inMs: number,
  outMs: number,
  atIndex?: number
): Result {
  const state = edlStore.getState()
  const usedMs = edlStore.derive().totalMs
  if (state.clips.length >= EDL_LIMITS.maxClips) {
    return fail(`片段数已达上限 ${EDL_LIMITS.maxClips} 个（01 §5.1），请先合并或删除片段`)
  }
  const durMs = clipDurationMs(inMs, outMs)
  if (durMs < EDL_LIMITS.minClipMs) {
    return fail(`片段时长不足 ${EDL_LIMITS.minClipMs}ms，无法添加（01 §5.1）`)
  }
  if (usedMs + durMs > EDL_LIMITS.maxTotalMs) {
    return fail(
      `添加后总时长将超过上限 ${Math.round(EDL_LIMITS.maxTotalMs / 3600000)} 小时，已阻止（01 §5.1）`
    )
  }
  if (atIndex !== undefined && (atIndex < 0 || atIndex > state.clips.length)) {
    return fail('插入位置非法')
  }

  const clip = buildClip(asset, inMs, outMs)
  edlStore.dispatch({ type: 'addClip', clip, atIndex })

  // 复核：dispatch 内部也会做上限保护，未落地则回滚提示
  const after = edlStore.getState()
  if (!after.clips.some((c) => c.clipId === clip.clipId)) {
    return fail('片段未能添加（超出上限或被拒绝）')
  }
  return { ok: true, clipId: clip.clipId }
}

function fail(reason: string): Result {
  toast(reason, 'error')
  return { ok: false, reason }
}
