// C-08：渲染方案（presetKey）展示表 —— 05 §5.1「枚举表（服务端权威，前端仅展示）」
// 提交渲染只接受 presetKey（03 §4.4：不在服务端枚举表内即 E_PROFILE_INVALID），
// 因此渲染弹窗不以下拉 GET /api/profiles（那是既有的转码方案 Profile，与 presetKey 不同域）。
// 决策对齐见 10-实施进度与下一步.md「C 组决策对齐清单 C-08-①」。

/** 与 FVCC `handlers_render.go: edlRenderPresetTable` 同集的展示项（前端仅展示，不做校验） */
export interface PresetOption {
  key: string
  /** 下拉展示名（含编码器与适用场景，05 §5.1「适用」列） */
  label: string
  /** 编码器（05 §5.1「编码器」列） */
  encoder: string
  /** 关键参数（05 §5.1「关键参数」列，hover 提示） */
  args: string
  /** 代理专用（04 §3.3）：不出现在渲染弹窗，仅由代理任务使用 */
  proxyOnly?: boolean
}

export const PRESET_OPTIONS: PresetOption[] = [
  { key: 'copy_same_source', label: '同源快速导出（无重编码）', encoder: 'copy', args: '同源同参时直接拼接 → 秒级完成（05 §3.1 P-A）' },
  { key: 'h264_nvenc_p5', label: 'H.264 · NVENC 默认', encoder: 'h264_nvenc', args: '-rc vbr -cq 18 -preset p5 -pix_fmt yuv420p' },
  { key: 'h264_nvenc_p7', label: 'H.264 · NVENC 高质量', encoder: 'h264_nvenc', args: '-rc vbr -cq 16 -preset p7' },
  { key: 'hevc_nvenc_p5', label: 'H.265 · NVENC 体积优先', encoder: 'hevc_nvenc', args: '-rc vbr -cq 20 -preset p5 -tag:v hvc1' },
  { key: 'h264_qsv_balanced', label: 'H.264 · Intel QSV 均衡', encoder: 'h264_qsv', args: '-global_quality 20 -preset medium' },
  { key: 'hevc_qsv_balanced', label: 'H.265 · Intel QSV 均衡', encoder: 'hevc_qsv', args: '-global_quality 22 -preset medium -tag:v hvc1' },
  { key: 'h264_amf_balanced', label: 'H.264 · AMD AMF 均衡', encoder: 'h264_amf', args: '-quality balanced -rc cqp -qp_i 20 -qp_p 22' },
  { key: 'libx264_medium', label: 'H.264 · 软件编码（兜底）', encoder: 'libx264', args: '-crf 18 -preset medium' },
  { key: 'libx264_slow', label: 'H.264 · 软件编码高质量', encoder: 'libx264', args: '-crf 16 -preset slow' },
  { key: 'libx265_medium', label: 'H.265 · 软件编码体积优先', encoder: 'libx265', args: '-crf 20 -preset medium -tag:v hvc1' },
  { key: 'proxy_720p_h264', label: '代理 720p · 软编', encoder: 'libx264', args: '-crf 23 -preset veryfast -vf scale=-2:720（04 §3.3）', proxyOnly: true },
  { key: 'proxy_720p_nvenc', label: '代理 720p · NVENC', encoder: 'h264_nvenc', args: '-cq 23 -preset p5 -vf scale=-2:720（节点空闲时）', proxyOnly: true },
]

/** 默认方案（05 §5.1「默认（NVIDIA）」） */
export const DEFAULT_PRESET_KEY = 'h264_nvenc_p5'

/** 代理任务固定方案（需求6：默认 GPU 硬编 proxy_720p_nvenc，与服务端 defaultProxyTemplate 一致；
 *  节点无 NVIDIA 编码器时由远端 FVCS 自动降级软编，见 FVCS proxy_flow.go） */
export const PROXY_PRESET_KEY = 'proxy_720p_nvenc'

/** 渲染弹窗可用项：排除代理专用（04 §3.3）与 copy 以外的降级由服务端处理（05 §5.2） */
export const RENDER_PRESETS: PresetOption[] = PRESET_OPTIONS.filter((p) => !p.proxyOnly)

export function findPreset(key: string): PresetOption | undefined {
  return PRESET_OPTIONS.find((p) => p.key === key)
}

export function presetLabel(key: string): string {
  const p = findPreset(key)
  return p ? p.label : key || '—'
}

/** 硬编降级提示（05 §5.2）：选中的硬编在渲染节点不可用时静默降级，不报错 */
export function fallbackHint(key: string): string {
  const p = findPreset(key)
  if (!p) return ''
  if (p.encoder === 'copy' || p.encoder === 'libx264' || p.encoder === 'libx265') return ''
  return `节点未检测到 ${p.encoder} 时自动降级为软件编码（05 §5.2），不影响提交`
}
