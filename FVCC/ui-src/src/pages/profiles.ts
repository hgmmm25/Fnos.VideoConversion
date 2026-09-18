import { store } from '../store'
import { api } from '../api'
import { el, toast, confirmDialog, emptyState, svgIcon } from '../ui'
import { type Profile } from '../types'
import { openDirBrowser } from './scanner'

export function renderProfiles(container: HTMLElement) {
  const wrap = el('div', { class: 'flex flex-col h-full p-4 gap-3' })

  // 视图状态：list=方案清单，edit=编辑/创建
  let view: 'list' | 'edit' = 'list'
  let currentId: string | null = null

  // 编辑视图下不响应 store 通知，避免转码进度推送导致表单重建、丢失用户正在编辑的值
  const render = () => {
    let scrollPos = 0
    const scrollContainer = wrap.querySelector('.overflow-auto') as HTMLElement
    if (scrollContainer) {
      scrollPos = scrollContainer.scrollTop
    }
    wrap.innerHTML = ''
    if (view === 'list') {
      wrap.appendChild(renderList())
    } else {
      const p = store.profiles.find((x) => x.id === currentId)
      wrap.appendChild(renderEdit(p))
    }
    const newScrollContainer = wrap.querySelector('.overflow-auto') as HTMLElement
    if (newScrollContainer) {
      newScrollContainer.scrollTop = scrollPos
    }
  }

  // 仅在列表视图响应 store 通知；编辑视图保持稳定，避免输入中途被重建
  const onStoreChange = () => {
    if (view === 'list') render()
  }

  function renderList(): HTMLElement {
    const addBtn = el('button', { class: 'btn btn-primary ml-auto flex items-center gap-1.5' }, [])
    addBtn.append(svgIcon('plus', 16) as unknown as Node, el('span', {}, ['新增方案']))
    const header = el('div', { class: 'flex items-center' }, [
      el('h2', { class: 'text-lg font-semibold' }, ['转码方案']),
      addBtn,
    ])
    addBtn.onclick = () => {
      currentId = null
      view = 'edit'
      render()
    }

    const list = el('div', { class: 'flex-1 overflow-auto' })
    if (store.profiles.length === 0) {
      // P2-1：空状态承载"下一步动作"
      list.appendChild(
        emptyState('暂无方案，请点击右上角「新增方案」创建', 'settings', {
          label: '新增方案',
          onClick: () => {
            addBtn.click()
          },
        })
      )
    } else {
      const grid = el('div', { class: 'grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3' })
      for (const p of store.profiles) {
        grid.appendChild(profileCard(p))
      }
      list.appendChild(grid)
    }

    const body = el('div', { class: 'flex flex-col flex-1 gap-3 overflow-hidden' }, [header, list])
    return body
  }

  function profileCard(p: Profile): HTMLElement {
    const card = el('div', {
      class: 'card p-4 cursor-pointer hover:ring-2 hover:ring-primary/30 transition',
    })
    const head = el('div', { class: 'flex items-center justify-between' }, [
      el('div', { class: 'font-medium' }, [p.name]),
      el('div', { class: 'flex items-center gap-1' }, [
        (() => {
          const editBtn = el('button', { class: 'btn btn-sm' }, [])
          editBtn.append(svgIcon('edit', 14) as unknown as Node)
          editBtn.title = '编辑'
          editBtn.onclick = (e) => {
            e.stopPropagation()
            currentId = p.id
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
            if (!(await confirmDialog(`确定删除方案「${p.name}」？`))) return
            try {
              await api.deleteProfile(p.id)
              await store.loadProfiles()
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
    const info = el('div', { class: 'text-xs text-ink-muted mt-2 space-y-1' }, [
      p.customFfmpeg ? el('div', { class: 'truncate' }, [`FFmpeg: ${p.customFfmpegArgs || '(空)'}`]) : el('div', {}, [`视频: ${p.vcodec ? p.vcodec + ' · ' + (p.width ? p.width + 'x' + p.height : '原值') + ' · ' + (p.rateControl || 'crf') : '未启用'}`]),
      !p.customFfmpeg ? el('div', {}, [`音频: ${p.acodec ? p.acodec + (p.acodec !== 'copy' ? ' · ' + (p.audioBitrate === 'original' ? '原值' : p.audioBitrate || '原值') : '') : '未启用'}`]) : el('div', {}, []),
      p.extraArgs ? el('div', { class: 'truncate' }, [`额外: ${p.extraArgs}`]) : el('div', {}, []),
    ])
    card.append(head, info)
    card.onclick = () => {
      currentId = p.id
      view = 'edit'
      render()
    }
    return card
  }

  function renderEdit(p: Profile | undefined): HTMLElement {
    const goBack = () => { view = 'list'; render() }
    const backBtn = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
    backBtn.append(svgIcon('back', 14) as unknown as Node, el('span', {}, ['返回清单']))
    backBtn.onclick = goBack
    const header = el('div', { class: 'flex items-center gap-2' }, [
      backBtn,
      el('h2', { class: 'text-lg font-semibold' }, [p ? '编辑方案' : '新增方案']),
    ])
    const body = el('div', { class: 'flex-1 overflow-auto' }, [editForm(p, goBack)])
    return el('div', { class: 'flex flex-col flex-1 gap-3 overflow-hidden' }, [header, body])
  }

  store.subscribe(onStoreChange)
  render()
  container.appendChild(wrap)
}

function editForm(p: Profile | undefined, onBack: () => void): HTMLElement {
  const form = el('div', { class: 'card p-4 max-w-4xl mx-auto w-full' })

  const field = (label: string, control: HTMLElement, hint?: string) => {
    const children: Node[] = [el('label', { class: 'block text-sm mb-1' }, [label]), control]
    if (hint) children.push(el('div', { class: 'text-xs text-ink-muted mt-1' }, [hint]))
    return el('div', { class: 'mb-3' }, children)
  }
  const row = (a: HTMLElement, b: HTMLElement) =>
    el('div', { class: 'grid grid-cols-1 sm:grid-cols-2 gap-3' }, [a, b])

  const nameInput = el('input', { class: 'input', placeholder: '方案名称', value: p?.name ?? '' }) as HTMLInputElement

  // 视频模块开关
  const videoAuto = el('input', { type: 'checkbox' }) as HTMLInputElement
  videoAuto.checked = p === undefined || !!p?.vcodec
  const videoParams = el('div', {})
  // videoOptionsDiv 包含除编码器选择外的所有视频选项，copy 模式下隐藏
  // 提前创建空 div 以便 updateVcodecDeps 引用（避免 TDZ）
  const videoOptionsDiv = el('div', {})

  // 视频参数
  const vcodec = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [
    ['copy', 'copy (不重编码)'],
    ['libx264', 'libx264 (H.264 软编)'],
    ['libx265', 'libx265 (H.265 软编)'],
    ['h264_nvenc', 'h264_nvenc (N卡 H.264)'],
    ['hevc_nvenc', 'hevc_nvenc (N卡 H.265)'],
    ['h264_amf', 'h264_amf (A卡 H.264)'],
    ['hevc_amf', 'hevc_amf (A卡 H.265)'],
    ['libvpx-vp9', 'libvpx-vp9 (VP9 软编)'],
    ['av1', 'av1 (AV1)'],
    ['mpeg4', 'mpeg4 (MPEG-4)'],
  ]) {
    vcodec.appendChild(el('option', { value: v }, [l]))
  }
  vcodec.value = p?.vcodec ?? 'libx264'

  const resSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [
    ['original', '保持原值'],
    ['4096x2160', '4096×2160 (4K)'],
    ['3840x2160', '3840×2160 (4K UHD)'],
    ['2560x1440', '2560×1440 (2K)'],
    ['1920x1080', '1920×1080 (1080p)'],
    ['1280x720', '1280×720 (720p)'],
    ['854x480', '854×480 (480p)'],
    ['640x360', '640×360 (360p)'],
    ['custom', '手动输入'],
  ]) {
    resSel.appendChild(el('option', { value: v }, [l]))
  }
  const widthInput = el('input', { type: 'number', class: 'input', placeholder: '宽度', min: '1', value: p?.width ? String(p.width) : '' }) as HTMLInputElement
  const heightInput = el('input', { type: 'number', class: 'input', placeholder: '高度', min: '1', value: p?.height ? String(p.height) : '' }) as HTMLInputElement
  if (p?.width && p?.height) {
    const matched = ['4096x2160', '3840x2160', '2560x1440', '1920x1080', '1280x720', '854x480', '640x360'].includes(`${p.width}x${p.height}`)
    resSel.value = matched ? `${p.width}x${p.height}` : 'custom'
  } else {
    resSel.value = 'original'
  }
  const customResRow = el('div', { class: 'flex gap-2' }, [widthInput, heightInput])

  if (resSel.value !== 'custom') customResRow.style.display = 'none'

  const aspectRatioSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [['scale', '原比例缩放'], ['crop', '等比例裁剪'], ['fill', '等比例填充'], ['stretch', '拉伸适配']]) {
    aspectRatioSel.appendChild(el('option', { value: v }, [l]))
  }
  aspectRatioSel.value = p?.aspectRatio ?? 'scale'

  const fpsSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['original', '10', '15', '20', '24', '25', '30', '50', '60', 'custom']) {
    fpsSel.appendChild(el('option', { value: o }, [o === 'custom' ? '自定义' : o === 'original' ? '保持原值' : o]))
  }
  fpsSel.value = p?.fps ?? 'original'
  const fpsCustomInput = el('input', { type: 'number', class: 'input', placeholder: '如 29.97', min: '0.1', max: '120', step: '0.01', value: p?.fpsCustom ? String(p.fpsCustom) : '' }) as HTMLInputElement
  const fpsCustomGroup = el('div', {}, [fpsCustomInput])
  fpsSel.onchange = () => {
    fpsCustomGroup.style.display = fpsSel.value === 'custom' ? 'block' : 'none'
  }
  if (fpsSel.value !== 'custom') fpsCustomGroup.style.display = 'none'

  const rcSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [['crf', 'CRF 恒定画质'], ['cbr', 'CBR 恒定码率'], ['vbr', 'VBR 可变码率']]) {
    rcSel.appendChild(el('option', { value: v }, [l]))
  }
  rcSel.value = p?.rateControl ?? 'crf'

  const qualityControlSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [['quality', '质量'], ['bitrate', '码率']]) {
    qualityControlSel.appendChild(el('option', { value: v }, [l]))
  }
  qualityControlSel.value = p?.qualityControl ?? 'quality'

  const crfInput = el('input', { type: 'number', class: 'input', min: '0', max: '63', value: p?.crf ? String(p.crf) : '23' }) as HTMLInputElement

  const constantQualityRange = el('input', { type: 'range', class: 'input', min: '0', max: '63', value: p?.constantQuality ? String(p.constantQuality) : '23' }) as HTMLInputElement
  const constantQualityInput = el('input', { type: 'number', class: 'input', min: '0', max: '63', value: p?.constantQuality ? String(p.constantQuality) : '23' }) as HTMLInputElement
  constantQualityRange.oninput = () => { constantQualityInput.value = constantQualityRange.value }
  constantQualityInput.oninput = () => { constantQualityRange.value = constantQualityInput.value }
  const constantQualityRow = el('div', { class: 'flex items-center gap-3' }, [el('div', { class: 'flex-1' }, [constantQualityRange]), el('div', { class: 'w-24' }, [constantQualityInput])])
  const constantQualityGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['恒定质量']),
    constantQualityRow,
  ])

  const bitrateInput = el('input', { type: 'number', class: 'input', placeholder: 'kbps', min: '1', value: p?.bitrate ?? '' }) as HTMLInputElement
  const bitrateGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['视频码率 (kbps)']),
    bitrateInput,
  ])

  let videoMaxRateGroup: HTMLElement | null = null
  let videoBufSizeGroup: HTMLElement | null = null

  function updateQualityControlDeps() {
    const qc = qualityControlSel.value
    const rc = rcSel.value
    if (qc === 'quality') {
      constantQualityGroup.style.display = ''
      bitrateGroup.style.display = 'none'
      if (videoMaxRateGroup) videoMaxRateGroup.style.display = 'none'
      if (videoBufSizeGroup) videoBufSizeGroup.style.display = 'none'
    } else {
      constantQualityGroup.style.display = 'none'
      bitrateGroup.style.display = ''
      if (rc === 'vbr') {
        if (videoMaxRateGroup) videoMaxRateGroup.style.display = ''
        if (videoBufSizeGroup) videoBufSizeGroup.style.display = ''
      } else {
        if (videoMaxRateGroup) videoMaxRateGroup.style.display = 'none'
        if (videoBufSizeGroup) videoBufSizeGroup.style.display = 'none'
      }
    }
  }

  rcSel.onchange = () => {
    updateQualityControlDeps()
  }
  qualityControlSel.onchange = updateQualityControlDeps

  const presetSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [['ultrafast', '极速 (最低质量)'], ['superfast', '超快'], ['veryfast', '很快'], ['faster', '较快'], ['fast', '快'], ['medium', '中等 (默认)'], ['slow', '慢'], ['slower', '较慢'], ['veryslow', '极慢 (最高质量)']]) {
    presetSel.appendChild(el('option', { value: v }, [l]))
  }
  presetSel.value = p?.preset ?? 'medium'

  const pixFmtSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['original', 'yuv420p', 'yuv422p', 'yuv444p', 'nv12', 'rgb24']) {
    pixFmtSel.appendChild(el('option', { value: o }, [o === 'original' ? '保持原值' : o]))
  }
  pixFmtSel.value = p?.pixFmt ?? 'original'

  const profileSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [['', '默认'], ['baseline', 'baseline (H.264)'], ['main', 'main (H.264/H.265)'], ['high', 'high (H.264)'], ['high10', 'high10 (H.264)'], ['main10', 'main10 (H.265)'], ['main12', 'main12 (H.265)']]) {
    profileSel.appendChild(el('option', { value: v }, [l]))
  }
  profileSel.value = p?.profile ?? ''

  const tuneSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [
    ['', '默认 (不调优)'],
    ['film', 'film (电影)'],
    ['animation', 'animation (动画)'],
    ['grain', 'grain (保留颗粒)'],
    ['stillimage', 'stillimage (静态图像)'],
    ['psnr', 'psnr (PSNR 优化)'],
    ['ssim', 'ssim (SSIM 优化)'],
    ['fastdecode', 'fastdecode (快速解码)'],
    ['zerolatency', 'zerolatency (零延迟)'],
  ]) {
    tuneSel.appendChild(el('option', { value: v }, [l]))
  }
  tuneSel.value = p?.tune ?? ''

  const rotateSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['0', '90', '180', '270']) {
    rotateSel.appendChild(el('option', { value: o }, [o + '°']))
  }
  rotateSel.value = p?.rotate ? String(p.rotate) : '0'

  const gopInput = el('input', { type: 'number', class: 'input', placeholder: '0=自动', min: '0', value: p?.gop ? String(p.gop) : '0' }) as HTMLInputElement
  const bframesInput = el('input', { type: 'number', class: 'input', placeholder: '0=自动', min: '0', value: p?.bframes ? String(p.bframes) : '0' }) as HTMLInputElement

  const scaleAlgoSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['bicubic', 'bilinear', 'lanczos', 'neighbor']) {
    scaleAlgoSel.appendChild(el('option', { value: o }, [o]))
  }
  scaleAlgoSel.value = p?.scaleAlgo ?? 'bicubic'

  const aspectRatioField = field('缩放处理', aspectRatioSel)
  const scaleAlgoField = field('缩放算法', scaleAlgoSel)
  const scaleRow = el('div', { class: 'grid grid-cols-1 sm:grid-cols-2 gap-3' }, [aspectRatioField, scaleAlgoField])
  if (resSel.value === 'original') scaleRow.style.display = 'none'
  resSel.onchange = () => {
    customResRow.style.display = resSel.value === 'custom' ? 'flex' : 'none'
    scaleRow.style.display = resSel.value === 'original' ? 'none' : ''
    if (resSel.value !== 'custom' && resSel.value !== 'original') {
      const [w, h] = resSel.value.split('x')
      widthInput.value = w
      heightInput.value = h
    }
  }

  // ===== 视频扩展参数 =====
  // 最大视频码率 + 码率缓冲区（仅编码器非 copy 时启用；缓冲区依赖最大码率）
  const videoMaxRateInput = el('input', { type: 'number', class: 'input', placeholder: 'kbps', min: '1', value: p?.videoMaxRate ? String(p.videoMaxRate) : '' }) as HTMLInputElement
  const videoBufSizeInput = el('input', { type: 'number', class: 'input', placeholder: 'kbps', min: '1', value: p?.videoBufSize ? String(p.videoBufSize) : '' }) as HTMLInputElement
  videoMaxRateGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['最大视频码率 (kbps)']),
    videoMaxRateInput,
  ])
  videoBufSizeGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['码率缓冲区 (kbps)']),
    videoBufSizeInput,
  ])
  updateQualityControlDeps()
  function updateVideoBufState() {
    const copyMode = vcodec.value === 'copy'
    videoMaxRateInput.disabled = copyMode
    videoBufSizeInput.disabled = copyMode || !(Number(videoMaxRateInput.value) > 0)
  }
  videoMaxRateInput.addEventListener('input', updateVideoBufState)
  videoMaxRateInput.addEventListener('change', updateVideoBufState)

  // 画面亮度 / 对比度 / 饱和度（所有编码器生效，包括 copy）
  const videoBrightnessRange = el('input', { type: 'range', class: 'input', min: '-1', max: '1', step: '0.05', value: p?.videoBrightness !== undefined ? String(p.videoBrightness) : '0' }) as HTMLInputElement
  const videoBrightnessInput = el('input', { type: 'number', class: 'input', min: '-1', max: '1', step: '0.05', value: p?.videoBrightness !== undefined ? String(p.videoBrightness) : '0' }) as HTMLInputElement
  videoBrightnessRange.oninput = () => { videoBrightnessInput.value = videoBrightnessRange.value }
  videoBrightnessInput.oninput = () => { videoBrightnessRange.value = videoBrightnessInput.value }
  const videoBrightnessRow = el('div', { class: 'flex items-center gap-3' }, [el('div', { class: 'flex-1' }, [videoBrightnessRange]), el('div', { class: 'w-24' }, [videoBrightnessInput])])

  const videoContrastRange = el('input', { type: 'range', class: 'input', min: '0', max: '2', step: '0.05', value: p?.videoContrast !== undefined ? String(p.videoContrast) : '1' }) as HTMLInputElement
  const videoContrastInput = el('input', { type: 'number', class: 'input', min: '0', max: '2', step: '0.05', value: p?.videoContrast !== undefined ? String(p.videoContrast) : '1' }) as HTMLInputElement
  videoContrastRange.oninput = () => { videoContrastInput.value = videoContrastRange.value }
  videoContrastInput.oninput = () => { videoContrastRange.value = videoContrastInput.value }
  const videoContrastRow = el('div', { class: 'flex items-center gap-3' }, [el('div', { class: 'flex-1' }, [videoContrastRange]), el('div', { class: 'w-24' }, [videoContrastInput])])

  const videoSaturationRange = el('input', { type: 'range', class: 'input', min: '0', max: '3', step: '0.05', value: p?.videoSaturation !== undefined ? String(p.videoSaturation) : '1' }) as HTMLInputElement
  const videoSaturationInput = el('input', { type: 'number', class: 'input', min: '0', max: '3', step: '0.05', value: p?.videoSaturation !== undefined ? String(p.videoSaturation) : '1' }) as HTMLInputElement
  videoSaturationRange.oninput = () => { videoSaturationInput.value = videoSaturationRange.value }
  videoSaturationInput.oninput = () => { videoSaturationRange.value = videoSaturationInput.value }
  const videoSaturationRow = el('div', { class: 'flex items-center gap-3' }, [el('div', { class: 'flex-1' }, [videoSaturationRange]), el('div', { class: 'w-24' }, [videoSaturationInput])])

  // 去隔行 / 锐化（所有编码器生效）
  const enableYadifCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  enableYadifCheck.checked = p?.enableYadif ?? false

  const enableUnsharpCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  enableUnsharpCheck.checked = p?.enableUnsharp ?? false
  const unsharpStrengthInput = el('input', { type: 'number', class: 'input', min: '0', max: '3', step: '0.01', value: p?.unsharpStrength !== undefined ? String(p.unsharpStrength) : '1.0' }) as HTMLInputElement
  const unsharpStrengthGroup = el('div', { class: 'mt-2 mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['锐化强度']),
    unsharpStrengthInput,
  ])
  enableUnsharpCheck.onchange = () => {
    unsharpStrengthGroup.style.display = enableUnsharpCheck.checked ? 'block' : 'none'
  }
  if (!enableUnsharpCheck.checked) unsharpStrengthGroup.style.display = 'none'

  // 色彩空间（编码器非 copy 启用）
  const colorSpaceSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [['original', '保持原值'], ['bt709', 'BT709 普通高清'], ['bt2020', 'BT2020 (HDR)'], ['smpte170m', 'SMPTE170M (NTSC)']]) {
    colorSpaceSel.appendChild(el('option', { value: v }, [l]))
  }
  colorSpaceSel.value = p?.colorSpace ?? 'original'

  // 参考帧数量（仅 x264/x265）
  const videoRefsInput = el('input', { type: 'number', class: 'input', placeholder: '0=自动', min: '1', max: '16', value: p?.videoRefs ? String(p.videoRefs) : '' }) as HTMLInputElement

  // x264 自适应量化强度（仅 libx264）
  const x264AQStrengthInput = el('input', { type: 'number', class: 'input', min: '0', max: '1.5', step: '0.01', value: p?.x264AQStrength !== undefined ? String(p.x264AQStrength) : '1.0' }) as HTMLInputElement

  // NVENC 空间域/时间域自适应量化（仅 nvenc）
  const nvencSpatialAQCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  nvencSpatialAQCheck.checked = p?.nvencSpatialAQ ?? false
  const nvencTemporalAQCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  nvencTemporalAQCheck.checked = p?.nvencTemporalAQ ?? false

  // 场景切换阈值（编码器非 copy）
  const videoScThresholdInput = el('input', { type: 'number', class: 'input', placeholder: '0=自动', min: '0', max: '1000', value: p?.videoScThreshold ? String(p.videoScThreshold) : '0' }) as HTMLInputElement

  // x265 CTU 尺寸 / 帧间决策等级（仅 libx265）
  const ctuSizeSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['64', '32', '16']) {
    ctuSizeSel.appendChild(el('option', { value: o }, [o]))
  }
  ctuSizeSel.value = p?.ctuSize ? String(p.ctuSize) : '64'

  const rdLevelSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['0', '1', '2', '3', '4', '5', '6']) {
    rdLevelSel.appendChild(el('option', { value: o }, [o]))
  }
  rdLevelSel.value = p?.rdLevel !== undefined ? String(p.rdLevel) : '0'

  // 视频编码器联动：根据 vcodec 控制 x264/x265/nvenc 专属字段的显隐与禁用
  // - copy：编码类参数置灰，滤镜参数依旧可用
  // - libx264：启用 videoRefs、x264AQStrength
  // - libx265：启用 videoRefs、ctuSize、rdLevel
  // - h264_nvenc/hevc_nvenc：启用 nvencSpatialAQ、nvencTemporalAQ
  const videoRefsGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['参考帧数量 (仅 x264/x265)']),
    videoRefsInput,
  ])
  const x264AQGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['x264 自适应量化强度 (仅 libx264)']),
    x264AQStrengthInput,
  ])
  const nvencAQGroup = el('div', { class: 'flex flex-col gap-2 mb-3' }, [
    el('div', { class: 'text-sm mb-1' }, ['NVENC 自适应量化 (仅 N卡)']),
    el('label', { class: 'flex items-center gap-2 text-sm' }, [nvencSpatialAQCheck, el('span', {}, ['空间域自适应量化'])]),
    el('label', { class: 'flex items-center gap-2 text-sm' }, [nvencTemporalAQCheck, el('span', {}, ['时间域自适应量化'])]),
  ])
  const ctuSizeGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['x265 CTU 尺寸 (仅 libx265)']),
    ctuSizeSel,
  ])
  const rdLevelGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['x265 帧间决策等级 (仅 libx265)']),
    rdLevelSel,
  ])

  // 可按编码器隐藏的视频字段包装（提前定义避免 TDZ）
  const presetField = field('预设档位', presetSel, '编码速度/质量权衡（仅 libx264/libx265）')
  const tuneField = field('调优 (tune)', tuneSel, '针对特定内容类型优化编码，仅 x264/x265 生效')
  const bframesField = field('B帧数量', bframesInput)
  const scThresholdField = field('场景切换阈值', videoScThresholdInput, '0=自动，越大越不易新增关键帧（仅 x264/x265）')

  // 按编码器族重建 profile 选项
  function rebuildProfileOptions(v: string) {
    const prev = profileSel.value
    profileSel.innerHTML = ''
    let opts: [string, string][] = [['', '默认']]
    if (v === 'libx264' || v === 'h264_nvenc' || v === 'h264_amf' || v === 'mpeg4') {
      opts = [['', '默认'], ['baseline', 'baseline'], ['main', 'main'], ['high', 'high'], ['high10', 'high10 (仅软编)']]
    } else if (v === 'libx265' || v === 'hevc_nvenc' || v === 'hevc_amf') {
      opts = [['', '默认'], ['main', 'main'], ['main10', 'main10'], ['main12', 'main12 (仅 libx265)']]
    }
    for (const [val, l] of opts) profileSel.appendChild(el('option', { value: val }, [l]))
    profileSel.value = opts.some(([val]) => val === prev) ? prev : ''
  }

  function updateVcodecDeps() {
    const v = vcodec.value
    const copyMode = v === 'copy'
    // copy 模式：隐藏除编码器外的所有视频选项
    videoOptionsDiv.style.display = copyMode ? 'none' : ''
    if (copyMode) {
      updateVideoBufState()
      return
    }
    // 编码类参数恢复可用
    videoMaxRateInput.disabled = false
    colorSpaceSel.disabled = false
    updateVideoBufState()

    // 编码器能力判定
    const isX264 = v === 'libx264'
    const isX265 = v === 'libx265'
    const isX264Family = isX264 || isX265
    const isNvenc = v === 'h264_nvenc' || v === 'hevc_nvenc'
    const isVp9 = v === 'libvpx-vp9'
    const isAv1 = v === 'av1'
    const crfCapable = isX264Family || isVp9 || isAv1

    // 预设档位：仅 libx264/libx265（选项为 x264 preset 名，其它编码器参数名/值不同）
    presetField.style.display = isX264Family ? '' : 'none'
    // 调优 tune：仅 libx264/libx265
    tuneField.style.display = isX264Family ? '' : 'none'
    // 场景切换阈值：仅 libx264/libx265
    scThresholdField.style.display = isX264Family ? '' : 'none'
    videoScThresholdInput.disabled = !isX264Family
    // B帧：VP9 无 B 帧概念
    bframesField.style.display = isVp9 ? 'none' : ''
    bframesInput.disabled = isVp9

    // 码率控制选项：不支持 CRF 的编码器移除 CRF 选项
    const rcPrev = rcSel.value
    rcSel.innerHTML = ''
    const rcOpts: [string, string][] = crfCapable
      ? [['crf', 'CRF 恒定画质'], ['cbr', 'CBR 恒定码率'], ['vbr', 'VBR 可变码率']]
      : [['cbr', 'CBR 恒定码率'], ['vbr', 'VBR 可变码率']]
    for (const [val, l] of rcOpts) rcSel.appendChild(el('option', { value: val }, [l]))
    rcSel.value = rcOpts.some(([val]) => val === rcPrev) ? rcPrev : 'vbr'

    // 画面档次 profile：按编码器族过滤选项
    rebuildProfileOptions(v)

    // x264/x265/nvenc 专属
    videoRefsGroup.style.display = isX264Family ? 'block' : 'none'
    videoRefsInput.disabled = !isX264Family
    x264AQGroup.style.display = isX264 ? 'block' : 'none'
    ctuSizeGroup.style.display = isX265 ? 'block' : 'none'
    rdLevelGroup.style.display = isX265 ? 'block' : 'none'
    nvencAQGroup.style.display = isNvenc ? 'flex' : 'none'

    // 根据编码器设置恒定质量的范围和默认值
    const qualityDefaults: Record<string, { min: number; max: number; def: number }> = {
      'libx264': { min: 0, max: 51, def: 23 },
      'libx265': { min: 0, max: 51, def: 28 },
      'h264_nvenc': { min: 0, max: 51, def: 28 },
      'hevc_nvenc': { min: 0, max: 51, def: 30 },
      'h264_amf': { min: 0, max: 51, def: 29 },
      'hevc_amf': { min: 0, max: 51, def: 31 },
      'libvpx-vp9': { min: 0, max: 63, def: 30 },
      'av1': { min: 0, max: 63, def: 28 },
      'mpeg4': { min: 1, max: 31, def: 15 },
    }
    const qd = qualityDefaults[v] || { min: 0, max: 63, def: 23 }
    constantQualityRange.min = String(qd.min)
    constantQualityRange.max = String(qd.max)
    constantQualityInput.min = String(qd.min)
    constantQualityInput.max = String(qd.max)
    if (!p?.constantQuality) {
      constantQualityRange.value = String(qd.def)
      constantQualityInput.value = String(qd.def)
    }
    updateQualityControlDeps()
  }

  // 视频开关联动
  videoAuto.onchange = () => {
    videoParams.style.display = videoAuto.checked ? 'block' : 'none'
    if (videoAuto.checked) {
      if (!vcodec.value || vcodec.value === '') {
        vcodec.value = 'libx264'
        resSel.value = 'original'
        widthInput.value = ''
        heightInput.value = ''
        aspectRatioSel.value = 'scale'
        fpsSel.value = 'original'
        fpsCustomInput.value = ''
        rcSel.value = 'crf'
        crfInput.value = '23'
        bitrateInput.value = ''
        qualityControlSel.value = 'original'
        constantQualityInput.value = '23'
        presetSel.value = 'medium'
        pixFmtSel.value = 'original'
        profileSel.value = 'original'
        tuneSel.value = 'none'
        rotateSel.value = '0'
        gopInput.value = ''
        bframesInput.value = ''
        scaleAlgoSel.value = 'bicubic'
        videoMaxRateInput.value = ''
        videoBufSizeInput.value = ''
        videoBrightnessInput.value = '0'
        videoContrastInput.value = '1'
        videoSaturationInput.value = '1'
        enableYadifCheck.checked = false
        enableUnsharpCheck.checked = false
        unsharpStrengthInput.value = '1.0'
        colorSpaceSel.value = 'original'
        videoRefsInput.value = ''
        x264AQStrengthInput.value = '1.0'
        nvencSpatialAQCheck.checked = false
        nvencTemporalAQCheck.checked = false
        videoScThresholdInput.value = ''
        ctuSizeSel.value = '64'
        rdLevelSel.value = '0'
      }
    } else {
      vcodec.value = ''
    }
    updateVcodecDeps()
  }
  // 视频编码器联动：vcodec 变化时更新 x264/x265/nvenc 专属字段
  vcodec.addEventListener('change', updateVcodecDeps)
  updateVcodecDeps()
  videoParams.style.display = videoAuto.checked ? 'block' : 'none'

  // 音频模块开关
  const audioAuto = el('input', { type: 'checkbox' }) as HTMLInputElement
  audioAuto.checked = p === undefined || !!p?.acodec
  const audioParams = el('div', {})
  // audioOptionsDiv 包含除编码器选择外的所有音频选项，copy 模式下隐藏
  // 提前创建空 div 以便 updateAcodecDeps 引用（避免 TDZ）
  const audioOptionsDiv = el('div', {})

  // 音频参数
  const acodec = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['copy', 'aac', 'libmp3lame', 'opus', 'flac', 'pcm_s16le']) {
    acodec.appendChild(el('option', { value: o }, [o]))
  }
  acodec.value = p?.acodec ?? 'aac'

  const channelsSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [['original', '保持原值'], ['1', '1 (单声道)'], ['2', '2 (立体声)'], ['4', '4'], ['6', '6 (5.1)'], ['8', '8 (7.1)']]) {
    channelsSel.appendChild(el('option', { value: v }, [l]))
  }
  channelsSel.value = p?.channels ?? 'original'

  const sampleRateSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['original', '8000', '16000', '24000', '32000', '44100', '48000', 'custom']) {
    sampleRateSel.appendChild(el('option', { value: o }, [o === 'custom' ? '自定义' : o === 'original' ? '保持原值' : o + ' Hz']))
  }
  sampleRateSel.value = p?.sampleRate ?? 'original'
  const sampleRateCustomInput = el('input', { type: 'number', class: 'input', placeholder: '采样率', min: '1', value: p?.sampleRateCustom ? String(p.sampleRateCustom) : '' }) as HTMLInputElement
  const sampleRateCustomGroup = el('div', {}, [sampleRateCustomInput])
  sampleRateSel.onchange = () => {
    sampleRateCustomGroup.style.display = sampleRateSel.value === 'custom' ? 'block' : 'none'
  }
  if (sampleRateSel.value !== 'custom') sampleRateCustomGroup.style.display = 'none'

  const audioBitrateSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['original', '64', '96', '128', '192', '256', '320', 'custom']) {
    audioBitrateSel.appendChild(el('option', { value: o }, [o === 'custom' ? '自定义' : o === 'original' ? '保持原值' : o + 'k']))
  }
  audioBitrateSel.value = p?.audioBitrate ?? 'original'
  const audioBitrateCustomInput = el('input', { type: 'number', class: 'input', placeholder: 'kbps', min: '1', value: p?.audioBitrateCustom ? String(p.audioBitrateCustom) : '' }) as HTMLInputElement
  const audioBitrateCustomGroup = el('div', {}, [audioBitrateCustomInput])
  audioBitrateSel.onchange = () => {
    audioBitrateCustomGroup.style.display = audioBitrateSel.value === 'custom' ? 'block' : 'none'
  }
  if (audioBitrateSel.value !== 'custom') audioBitrateCustomGroup.style.display = 'none'

  const sampleFmtSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['original', 's16', 's32', 'flt', 'fltp']) {
    sampleFmtSel.appendChild(el('option', { value: o }, [o === 'original' ? '保持原值' : o]))
  }
  sampleFmtSel.value = p?.sampleFmt ?? 'original'

  const aacProfileSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['original', 'lc', 'he', 'he_v2']) {
    aacProfileSel.appendChild(el('option', { value: o }, [o === 'original' ? '保持原值' : o]))
  }
  aacProfileSel.value = p?.aacProfile ?? 'original'

  const volumeRange = el('input', { type: 'range', class: 'input', min: '0', max: '5', step: '0.1', value: p?.volume ? String(p.volume) : '1.0' }) as HTMLInputElement
  const volumeInput = el('input', { type: 'number', class: 'input', min: '0', max: '5', step: '0.1', value: p?.volume ? String(p.volume) : '1.0' }) as HTMLInputElement
  volumeRange.oninput = () => { volumeInput.value = volumeRange.value }
  volumeInput.oninput = () => { volumeRange.value = volumeInput.value }
  const volumeRow = el('div', { class: 'flex items-center gap-3' }, [
    el('div', { class: 'flex-1' }, [volumeRange]),
    el('div', { class: 'w-24' }, [volumeInput]),
  ])

  const silenceCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  silenceCheck.checked = p?.silence ?? false

  // ===== 音频扩展参数 =====
  // 自动音量归一化（acodec≠copy 启用）
  const audioDynNormCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  audioDynNormCheck.checked = p?.audioDynNorm ?? false

  // 音频高频截止频率（aac/opus/libmp3lame 启用）
  const audioCutoffInput = el('input', { type: 'number', class: 'input', placeholder: 'Hz', min: '1000', max: '24000', value: p?.audioCutoff ? String(p.audioCutoff) : '' }) as HTMLInputElement

  // Opus 压缩级别（仅 opus）
  const opusCompLevelSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '10']) {
    opusCompLevelSel.appendChild(el('option', { value: o }, [o]))
  }
  opusCompLevelSel.value = p?.opusCompLevel !== undefined ? String(p.opusCompLevel) : '0'

  // 音视频同步偏移（acodec≠copy 启用）
  const audioSyncOffsetInput = el('input', { type: 'number', class: 'input', placeholder: 'ms', min: '-2000', max: '2000', value: p?.audioSyncOffset ? String(p.audioSyncOffset) : '' }) as HTMLInputElement

  // 音频编码器联动：根据 acodec 启用/禁用扩展字段
  const audioCutoffGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['音频高频截止频率 (Hz)']),
    audioCutoffInput,
  ])
  const opusCompGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['Opus 压缩级别 (仅 opus)']),
    opusCompLevelSel,
  ])
  const audioDynNormLabel = el('label', { class: 'flex items-center gap-2 text-sm mb-3' }, [audioDynNormCheck, el('span', {}, ['自动音量归一化'])])
  const audioSyncOffsetGroup = el('div', { class: 'mb-3' }, [
    el('label', { class: 'block text-sm mb-1' }, ['音视频同步偏移 (ms)']),
    audioSyncOffsetInput,
  ])

  // 可按编码器隐藏的音频字段包装
  const audioBitrateField = field('音频码率', audioBitrateSel)
  const aacProfileField = field('AAC 档次', aacProfileSel)

  // 按编码器重建采样位深选项（pcm_s16le 仅 s16）
  function rebuildSampleFmtOptions(v: string) {
    const prev = sampleFmtSel.value
    sampleFmtSel.innerHTML = ''
    const opts = v === 'pcm_s16le' ? ['original', 's16'] : ['original', 's16', 's32', 'flt', 'fltp']
    for (const o of opts) sampleFmtSel.appendChild(el('option', { value: o }, [o === 'original' ? '保持原值' : o]))
    sampleFmtSel.value = opts.includes(prev) ? prev : 'original'
  }

  function updateAcodecDeps() {
    const v = acodec.value
    const copyMode = v === 'copy'
    // copy 模式：隐藏除编码器外的所有音频选项
    audioOptionsDiv.style.display = copyMode ? 'none' : ''
    if (copyMode) return
    audioDynNormCheck.disabled = false
    audioCutoffGroup.style.display = (v === 'aac' || v === 'opus' || v === 'libmp3lame') ? 'block' : 'none'
    audioCutoffInput.disabled = false
    opusCompGroup.style.display = v === 'opus' ? 'block' : 'none'
    opusCompLevelSel.disabled = false
    audioSyncOffsetInput.disabled = false
    // AAC 档次：仅 aac
    aacProfileField.style.display = v === 'aac' ? '' : 'none'
    // 音频码率：无损编码 flac/pcm_s16le 无意义
    const lossless = v === 'flac' || v === 'pcm_s16le'
    audioBitrateField.style.display = lossless ? 'none' : ''
    audioBitrateCustomGroup.style.display = (lossless || audioBitrateSel.value !== 'custom') ? 'none' : 'block'
    // 采样位深：pcm_s16le 仅 s16
    rebuildSampleFmtOptions(v)
  }

  // 音频开关联动
  audioAuto.onchange = () => {
    audioParams.style.display = audioAuto.checked ? 'block' : 'none'
    if (audioAuto.checked) {
      if (!acodec.value || acodec.value === '') {
        acodec.value = 'copy'
        channelsSel.value = 'original'
        sampleRateSel.value = 'original'
        sampleRateCustomInput.value = ''
        audioBitrateSel.value = 'original'
        audioBitrateCustomInput.value = ''
        sampleFmtSel.value = 'original'
        aacProfileSel.value = 'original'
        volumeInput.value = '1.0'
        silenceCheck.checked = false
        audioDynNormCheck.checked = false
        audioCutoffInput.value = ''
        opusCompLevelSel.value = '3'
        audioSyncOffsetInput.value = ''
      }
    } else {
      acodec.value = ''
    }
    updateAcodecDeps()
  }
  audioParams.style.display = audioAuto.checked ? 'block' : 'none'
  acodec.addEventListener('change', updateAcodecDeps)
  updateAcodecDeps()

  // 输出设置
  const outputSuffixInput = el('input', {
    class: 'input', value: p?.outputSuffix || '_trans.mp4', placeholder: '_trans.mp4',
  }) as HTMLInputElement

  const extraArgs = el('textarea', { class: 'input', rows: '2', placeholder: '额外 FFmpeg 参数' }) as HTMLTextAreaElement
  extraArgs.value = p?.extraArgs ?? ''

  // 自定义 FFmpeg 参数模式
  const customFfmpegCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  customFfmpegCheck.checked = p?.customFfmpeg ?? false
  const customFfmpegArgs = el('textarea', { class: 'input font-mono text-xs', rows: '3', placeholder: '如: -c:v libx264 -crf 23 -c:a aac -b:a 128k' }) as HTMLTextAreaElement
  customFfmpegArgs.value = p?.customFfmpegArgs ?? ''

  // ===== 高级封装参数（永久显示，不受音视频开关控制） =====
  const hwAccelSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const [v, l] of [['original', '默认自动'], ['auto', '自动选择硬件'], ['cuda', 'NVIDIA CUDA'], ['qsv', 'Intel QSV'], ['d3d11va', 'Windows D3D11VA'], ['none', '关闭硬件加速']]) {
    hwAccelSel.appendChild(el('option', { value: v }, [l]))
  }
  hwAccelSel.value = p?.hwAccel ?? 'original'

  const movFastStartCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  movFastStartCheck.checked = p?.movFastStart ?? false

  const threadCountSel = el('select', { class: 'input' }) as HTMLSelectElement
  for (const o of ['auto', '1', '2', '4', '6', '8', '12', '16']) {
    threadCountSel.appendChild(el('option', { value: o }, [o]))
  }
  threadCountSel.value = p?.threadCount ?? 'auto'

  // FFmpeg 参数预览
  const ffmpegPreview = el('textarea', {
    class: 'input font-mono text-xs', rows: '3', readonly: 'true', placeholder: 'FFmpeg 参数预览',
  }) as HTMLTextAreaElement
  ffmpegPreview.style.backgroundColor = 'var(--c-log-bg)'

  function updateFFmpegPreview() {
    // 自定义 FFmpeg 参数模式：直接显示用户输入的参数
    if (customFfmpegCheck.checked) {
      ffmpegPreview.value = customFfmpegArgs.value.trim() || '(请输入自定义 FFmpeg 参数)'
      return
    }
    const parts: string[] = []
    // 视频参数
    if (videoAuto.checked) {
      if (vcodec.value && vcodec.value !== 'copy') parts.push(`-c:v ${vcodec.value}`)
      else if (vcodec.value === 'copy') parts.push(`-c:v copy`)

      const vCopyMode = vcodec.value === 'copy'
      if (!vCopyMode) {
        const resVal = resSel.value
        if (resVal !== 'original' && resVal !== 'custom') {
          parts.push(`-s ${resVal}`)
        } else if (resVal === 'custom' && widthInput.value && heightInput.value) {
          parts.push(`-s ${widthInput.value}x${heightInput.value}`)
        }

        // 视频滤镜（合并到 -vf）：比例处理 + eq + yadif + unsharp
        const vfParts: string[] = []
        if (resVal !== 'original' && aspectRatioSel.value !== 'scale' && aspectRatioSel.value) {
          if (aspectRatioSel.value === 'crop') vfParts.push('crop=iw:ih')
          else if (aspectRatioSel.value === 'fill') vfParts.push('pad=iw:ih')
          else if (aspectRatioSel.value === 'stretch') vfParts.push('scale=iw:ih')
        }
        // eq 滤镜：亮度/对比度/饱和度（非默认值时加入）
        const eqParts: string[] = []
        const b = parseFloat(videoBrightnessInput.value)
        const c = parseFloat(videoContrastInput.value)
        const s = parseFloat(videoSaturationInput.value)
        if (!isNaN(b) && b !== 0) eqParts.push(`brightness=${b}`)
        if (!isNaN(c) && c !== 1) eqParts.push(`contrast=${c}`)
        if (!isNaN(s) && s !== 1) eqParts.push(`saturation=${s}`)
        if (eqParts.length) vfParts.push(`eq=${eqParts.join(':')}`)
        if (enableYadifCheck.checked) vfParts.push('yadif')
        if (enableUnsharpCheck.checked) {
          const strength = parseFloat(unsharpStrengthInput.value)
          const v = isNaN(strength) ? 1.0 : strength
          vfParts.push(`unsharp=3:3:${v}:3:3:0.0`)
        }
        if (vfParts.length) parts.push(`-vf ${vfParts.join(',')}`)

        const fpsVal = fpsSel.value
        if (fpsVal === 'custom' && fpsCustomInput.value) parts.push(`-r ${fpsCustomInput.value}`)
        else if (fpsVal !== 'original') parts.push(`-r ${fpsVal}`)

        if (qualityControlSel.value === 'quality') {
          const qVal = constantQualityInput.value
          const vc = vcodec.value
          let qParam = '-crf'
          if (vc === 'h264_nvenc' || vc === 'hevc_nvenc' || vc === 'h264_amf' || vc === 'hevc_amf') {
            qParam = '-cq'
          } else if (vc === 'mpeg4') {
            qParam = '-qp'
          }
          parts.push(`${qParam} ${qVal}`)
        } else {
          if (bitrateInput.value) parts.push(`-b:v ${bitrateInput.value}k`)
          if (rcSel.value === 'cbr') parts.push(`-minrate ${bitrateInput.value}k`, `-maxrate ${bitrateInput.value}k`, `-bufsize ${bitrateInput.value}k`)
          if (rcSel.value === 'vbr') {
            if (videoMaxRateInput.value && Number(videoMaxRateInput.value) > 0) parts.push(`-maxrate ${videoMaxRateInput.value}k`)
            if (videoBufSizeInput.value && Number(videoBufSizeInput.value) > 0) parts.push(`-bufsize ${videoBufSizeInput.value}k`)
          }
        }

        const vc = vcodec.value
        const x264fam = vc === 'libx264' || vc === 'libx265'
        if (presetSel.value && x264fam) parts.push(`-preset ${presetSel.value}`)
        if (pixFmtSel.value !== 'original') parts.push(`-pix_fmt ${pixFmtSel.value}`)
        if (profileSel.value) parts.push(`-profile:v ${profileSel.value}`)
        if (tuneSel.value && x264fam) parts.push(`-tune ${tuneSel.value}`)
        if (rotateSel.value !== '0') parts.push(`-metadata:s:v:0 rotate=${rotateSel.value}`)
        if (gopInput.value && Number(gopInput.value) > 0) parts.push(`-g ${gopInput.value}`)
        if (bframesInput.value && Number(bframesInput.value) > 0 && vc !== 'libvpx-vp9') parts.push(`-bf ${bframesInput.value}`)
        if (resVal !== 'original' && scaleAlgoSel.value) parts.push(`-sws_flags ${scaleAlgoSel.value}`)
        // 色彩空间
        if (colorSpaceSel.value !== 'original' && colorSpaceSel.value) {
          parts.push(`-colorspace ${colorSpaceSel.value}`, `-color_primaries ${colorSpaceSel.value}`, `-color_trc ${colorSpaceSel.value}`)
        }
        // 参考帧
        if (videoRefsInput.value && Number(videoRefsInput.value) > 0) parts.push(`-refs ${videoRefsInput.value}`)
        // 场景切换阈值（仅 x264/x265）
        if (x264fam && videoScThresholdInput.value && Number(videoScThresholdInput.value) > 0) parts.push(`-sc_threshold ${videoScThresholdInput.value}`)
        // x264 自适应量化强度
        if (vcodec.value === 'libx264' && x264AQStrengthInput.value) parts.push(`-x264-opts aq-strength=${x264AQStrengthInput.value}`)
        // x265 专属
        if (vcodec.value === 'libx265') {
          const x265opts: string[] = []
          if (ctuSizeSel.value) x265opts.push(`ctu-size=${ctuSizeSel.value}`)
          if (rdLevelSel.value) x265opts.push(`rd-level=${rdLevelSel.value}`)
          if (x265opts.length) parts.push(`-x265-params ${x265opts.join(':')}`)
        }
        // NVENC 专属
        if (vcodec.value === 'h264_nvenc' || vcodec.value === 'hevc_nvenc') {
          if (nvencSpatialAQCheck.checked) parts.push(`-rc-lookahead 1`)
          if (nvencTemporalAQCheck.checked) parts.push(`-temporal-aq 1`)
        }
      }
    } else {
      parts.push(`-vn`)
    }

    // 音频参数
    if (audioAuto.checked) {
      if (acodec.value && acodec.value !== 'copy') parts.push(`-c:a ${acodec.value}`)
      else if (acodec.value === 'copy') parts.push(`-c:a copy`)

      const aCopyMode = acodec.value === 'copy'
      if (!aCopyMode) {
        if (channelsSel.value !== 'original') parts.push(`-ac ${channelsSel.value}`)

        if (sampleRateSel.value === 'custom' && sampleRateCustomInput.value) parts.push(`-ar ${sampleRateCustomInput.value}`)
        else if (sampleRateSel.value !== 'original') parts.push(`-ar ${sampleRateSel.value}`)

        // 音频码率：无损编码 flac/pcm_s16le 无意义
        const aLossless = acodec.value === 'flac' || acodec.value === 'pcm_s16le'
        if (!aLossless) {
          if (audioBitrateSel.value === 'custom' && audioBitrateCustomInput.value) parts.push(`-b:a ${audioBitrateCustomInput.value}k`)
          else if (audioBitrateSel.value !== 'original') parts.push(`-b:a ${audioBitrateSel.value}k`)
        }

        if (sampleFmtSel.value !== 'original') parts.push(`-sample_fmt ${sampleFmtSel.value}`)
        if (aacProfileSel.value !== 'original' && acodec.value === 'aac') parts.push(`-profile:a ${aacProfileSel.value}`)

        // 音频滤镜（合并到 -af）：volume + silenceremove + dynaudnorm
        const afParts: string[] = []
        if (volumeInput.value && Number(volumeInput.value) !== 1.0) afParts.push(`volume=${volumeInput.value}`)
        if (silenceCheck.checked) afParts.push('silenceremove=stop_periods=-1:stop_duration=0:stop_threshold=-50dB')
        if (audioDynNormCheck.checked) afParts.push('dynaudnorm')
        if (afParts.length) parts.push(`-af ${afParts.join(',')}`)

        // 音频高频截止频率
        if (audioCutoffInput.value && Number(audioCutoffInput.value) > 0) parts.push(`-cutoff ${audioCutoffInput.value}`)
        // Opus 压缩级别
        if (acodec.value === 'opus') parts.push(`-compression_level ${opusCompLevelSel.value}`)
        // 音视频同步偏移
        if (audioSyncOffsetInput.value) parts.push(`-async ${audioSyncOffsetInput.value}`)
      }
    } else {
      parts.push(`-an`)
    }

    // 高级封装参数（全局）
    if (hwAccelSel.value !== 'original' && hwAccelSel.value) parts.push(`-hwaccel ${hwAccelSel.value}`)
    if (movFastStartCheck.checked) parts.push(`-movflags +faststart`)
    if (threadCountSel.value && threadCountSel.value !== 'auto') parts.push(`-threads ${threadCountSel.value}`)

    if (extraArgs.value.trim()) parts.push(extraArgs.value.trim())

    ffmpegPreview.value = parts.join(' ')
  }

  // 监听所有表单变化
  const allInputs = [
    vcodec, resSel, widthInput, heightInput, aspectRatioSel, fpsSel, fpsCustomInput, rcSel, qualityControlSel, crfInput, constantQualityRange, constantQualityInput, bitrateInput, presetSel, pixFmtSel, profileSel, tuneSel, rotateSel, gopInput, bframesInput, scaleAlgoSel,
    videoMaxRateInput, videoBufSizeInput, videoBrightnessRange, videoBrightnessInput, videoContrastRange, videoContrastInput, videoSaturationRange, videoSaturationInput,
    enableYadifCheck, enableUnsharpCheck, unsharpStrengthInput, colorSpaceSel, videoRefsInput, x264AQStrengthInput, nvencSpatialAQCheck, nvencTemporalAQCheck, videoScThresholdInput, ctuSizeSel, rdLevelSel,
    acodec, channelsSel, sampleRateSel, sampleRateCustomInput, audioBitrateSel, audioBitrateCustomInput, sampleFmtSel, aacProfileSel, volumeRange, volumeInput,
    audioDynNormCheck, audioCutoffInput, opusCompLevelSel, audioSyncOffsetInput,
    hwAccelSel, movFastStartCheck, threadCountSel,
  ]
  for (const inp of allInputs) {
    inp.addEventListener('input', updateFFmpegPreview)
    inp.addEventListener('change', updateFFmpegPreview)
  }
  videoAuto.addEventListener('change', updateFFmpegPreview)
  audioAuto.addEventListener('change', updateFFmpegPreview)
  extraArgs.addEventListener('input', updateFFmpegPreview)
  customFfmpegCheck.addEventListener('change', updateFFmpegPreview)
  customFfmpegArgs.addEventListener('input', updateFFmpegPreview)

  // 布局
  // 构建视频参数区域：填充 videoOptionsDiv（已在前面创建）
  videoOptionsDiv.append(
    customResRow,
    scaleRow,
    row(field('帧率', fpsSel), presetField),
    fpsCustomGroup,
    row(field('码率控制', rcSel), field('质量控制', qualityControlSel)),
    constantQualityGroup,
    row(bitrateGroup, videoMaxRateGroup),
    row(videoBufSizeGroup, field('关键帧间隔', gopInput)),
    row(field('像素格式', pixFmtSel), field('画面档次', profileSel)),
    row(tuneField, field('旋转', rotateSel)),
    row(bframesField, scThresholdField),
    row(field('色彩空间', colorSpaceSel), field('色彩饱和度', videoSaturationRow, '范围 0~3.0，默认 1')),
    // 滤镜区
    el('div', { class: 'text-xs text-ink-muted mt-2 mb-1' }, ['画面调整（滤镜）']),
    row(field('画面亮度', videoBrightnessRow, '范围 -1.0~1.0，默认 0'), field('画面对比度', videoContrastRow, '范围 0~2.0，默认 1')),
    el('label', { class: 'flex items-center gap-2 text-sm mb-3' }, [enableYadifCheck, el('span', {}, ['开启去隔行 (yadif)'])]),
    el('label', { class: 'flex items-center gap-2 text-sm' }, [enableUnsharpCheck, el('span', {}, ['开启画面锐化 (unsharp)'])]),
    unsharpStrengthGroup,
    // 编码器专属区
    videoRefsGroup,
    x264AQGroup,
    nvencAQGroup,
    ctuSizeGroup,
    rdLevelGroup,
  )
  videoParams.append(
    row(field('编码器', vcodec), field('分辨率', resSel)),
    videoOptionsDiv,
  )

  // 构建音频参数区域：填充 audioOptionsDiv（已在前面创建）
  audioOptionsDiv.append(
    row(field('采样率', sampleRateSel), field('采样位深', sampleFmtSel)),
    sampleRateCustomGroup,
    row(audioBitrateField, aacProfileField),
    audioBitrateCustomGroup,
    field('音量增益', volumeRow),
    el('label', { class: 'flex items-center gap-2 text-sm mb-3' }, [silenceCheck, el('span', {}, ['静音检测裁剪'])]),
    audioDynNormLabel,
    row(audioCutoffGroup, audioSyncOffsetGroup),
    opusCompGroup,
  )
  audioParams.append(
    row(field('编码器', acodec), field('声道数', channelsSel)),
    audioOptionsDiv,
  )

  // 整体表单：各区块用命名变量，便于自定义模式隐藏
  const videoSection = el('div', { class: 'border-t border-line my-3 pt-3' }, [
    el('label', { class: 'flex items-center gap-2 text-sm font-medium mb-3' }, [videoAuto, el('span', {}, ['启用视频编码'])]),
    videoParams,
  ])

  const audioSection = el('div', { class: 'border-t border-line my-3 pt-3' }, [
    el('label', { class: 'flex items-center gap-2 text-sm font-medium mb-3' }, [audioAuto, el('span', {}, ['启用音频编码'])]),
    audioParams,
  ])

  const advancedSection = el('div', { class: 'border-t border-line my-3 pt-3' }, [
    el('div', { class: 'text-sm font-medium mb-2' }, ['高级-封装参数']),
    row(field('硬件解码模式', hwAccelSel), field('编码线程数量', threadCountSel)),
    el('label', { class: 'flex items-center gap-2 text-sm mb-3 mt-3' }, [movFastStartCheck, el('span', {}, ['MP4 网页快速播放 (faststart)'])]),
  ])

  const outputPathCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  outputPathCheck.checked = p?.outputPath ? true : false
  const outputPathInput = el('input', { type: 'text', class: 'input', placeholder: '文件夹路径', value: p?.outputPath ?? '' }) as HTMLInputElement
  const outputPathBtn = el('button', { class: 'btn btn-sm' }, ['浏览']) as HTMLButtonElement
  const outputPathGroup = el('div', { class: 'flex gap-2 mb-3' }, [outputPathInput, outputPathBtn])
  outputPathBtn.onclick = () => {
    openDirBrowser((path: string) => {
      outputPathInput.value = path
    })
  }
  outputPathCheck.onchange = () => {
    outputPathGroup.style.display = outputPathCheck.checked ? 'flex' : 'none'
  }
  if (!outputPathCheck.checked) outputPathGroup.style.display = 'none'

  const deleteSourceCheck = el('input', { type: 'checkbox' }) as HTMLInputElement
  deleteSourceCheck.checked = p?.deleteSource ?? false
  const deleteSourceLabel = el('span', {}, ['删除源文件（转码完成后删除源文件，此操作不可逆）'])
  const deleteSourceHint = el('div', {}, ['⚠ 警告：启用后转码完成将永久删除源文件，此操作不可逆！'])
  deleteSourceHint.style.fontSize = '12px'
  deleteSourceHint.style.marginTop = '4px'
  deleteSourceHint.style.color = 'rgb(var(--c-danger-hover))'
  deleteSourceHint.style.display = 'none'
  const deleteSourceWrap = el('div', { class: 'mb-3' }, [
    el('label', { class: 'flex items-center gap-2 text-sm' }, [deleteSourceCheck, deleteSourceLabel]),
    deleteSourceHint,
  ])
  function updateDeleteSourceStyle() {
    deleteSourceHint.style.display = deleteSourceCheck.checked ? 'block' : 'none'
  }
  deleteSourceCheck.onchange = async () => {
    if (deleteSourceCheck.checked) {
      if (!(await confirmDialog('警告：此操作将在转码完成后删除源文件，此操作不可逆！确定要启用吗？', '危险操作确认', true))) {
        deleteSourceCheck.checked = false
      }
    }
    updateDeleteSourceStyle()
  }
  updateDeleteSourceStyle()

  const outputSection = el('div', { class: 'border-t border-line my-3 pt-3' }, [
    el('div', { class: 'text-sm font-medium mb-2' }, ['输出设置']),
    field('默认输出后缀', outputSuffixInput, '转码后文件名在原文件名基础上添加的后缀，如 _trans.mp4'),
    el('label', { class: 'flex items-center gap-2 text-sm mb-3' }, [outputPathCheck, el('span', {}, ['转换文件另存为'])]),
    outputPathGroup,
    deleteSourceWrap,
  ])

  const extraSection = el('div', { class: 'border-t border-line my-3 pt-3' }, [
    el('div', { class: 'text-sm font-medium mb-2' }, ['高级选项']),
    el('label', { class: 'flex items-center gap-2 text-sm mb-3' }, [customFfmpegCheck, el('span', {}, ['自定义 FFmpeg 参数（勾选后忽略上述所有编码选项，直接使用下方输入的参数）'])]),
    field('FFmpeg 参数', customFfmpegArgs, '勾选「自定义」后，转码时直接使用此参数'),
    field('额外参数', extraArgs),
    field('FFmpeg 参数预览', ffmpegPreview, '当前表单参数将生成以下 FFmpeg 命令行参数'),
  ])

  form.append(
    field('方案名称 *', nameInput),
    videoSection,
    audioSection,
    advancedSection,
    outputSection,
    extraSection,
  )

  // 自定义 FFmpeg 模式联动：勾选后隐藏除方案名称、自定义、FFmpeg 参数、输出设置外的所有区块
  function updateCustomFfmpegDeps() {
    const custom = customFfmpegCheck.checked
    videoSection.style.display = custom ? 'none' : ''
    audioSection.style.display = custom ? 'none' : ''
    advancedSection.style.display = custom ? 'none' : ''
    // 输出设置始终显示
    const extraChildren = Array.from(extraSection.children)
    if (extraChildren.length >= 5) {
      ;(extraChildren[0] as HTMLElement).style.display = custom ? 'none' : ''
      ;(extraChildren[2] as HTMLElement).style.display = custom ? '' : 'none'
      ;(extraChildren[3] as HTMLElement).style.display = custom ? 'none' : ''
      ;(extraChildren[4] as HTMLElement).style.display = custom ? 'none' : ''
    }
  }
  customFfmpegCheck.addEventListener('change', () => {
    updateCustomFfmpegDeps()
    updateFFmpegPreview()
  })
  updateCustomFfmpegDeps()

  updateFFmpegPreview()

  // 操作按钮
  const btns = el('div', { class: 'flex justify-end gap-2 mt-4 pt-3 border-t border-line' })
  if (p) {
    const del = el('button', { class: 'btn btn-danger mr-auto flex items-center gap-1.5' }, [])
    del.append(svgIcon('trash', 14) as unknown as Node, el('span', {}, ['删除']))
    del.onclick = async () => {
      if (!(await confirmDialog(`确定删除方案「${p.name}」？`))) return
      try {
        await api.deleteProfile(p.id)
        await store.loadProfiles()
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
    const body: Partial<Profile> = {
      name: nameInput.value.trim(),
      // 自定义 FFmpeg 参数模式
      customFfmpeg: customFfmpegCheck.checked,
      customFfmpegArgs: customFfmpegCheck.checked ? customFfmpegArgs.value.trim() : '',
      // 输出设置
      outputPath: outputPathCheck.checked ? outputPathInput.value.trim() : '',
      deleteSource: deleteSourceCheck.checked,
    }
    if (!body.name) {
      toast('请填写方案名称')
      return
    }
    if (customFfmpegCheck.checked && !body.customFfmpegArgs) {
      toast('勾选自定义模式后请填写 FFmpeg 参数')
      return
    }
    if (!customFfmpegCheck.checked) {
      const isResOriginal = resSel.value === 'original'
      if (videoAuto.checked) {
        Object.assign(body, {
          vcodec: vcodec.value,
          width: !isResOriginal && widthInput.value ? Number(widthInput.value) : 0,
          height: !isResOriginal && heightInput.value ? Number(heightInput.value) : 0,
          aspectRatio: aspectRatioSel.value,
          fps: fpsSel.value,
          fpsCustom: fpsCustomInput.value ? Number(fpsCustomInput.value) : 0,
          rateControl: rcSel.value,
          crf: Number(crfInput.value),
          bitrate: bitrateInput.value,
          qualityControl: qualityControlSel.value,
          constantQuality: Number(constantQualityInput.value),
          preset: presetSel.value,
          pixFmt: pixFmtSel.value,
          profile: profileSel.value,
          tune: tuneSel.value,
          rotate: Number(rotateSel.value),
          gop: Number(gopInput.value),
          bframes: Number(bframesInput.value),
          scaleAlgo: scaleAlgoSel.value,
          videoMaxRate: videoMaxRateInput.value ? Number(videoMaxRateInput.value) : 0,
          videoBufSize: videoBufSizeInput.value ? Number(videoBufSizeInput.value) : 0,
          videoBrightness: Number(videoBrightnessInput.value),
          videoContrast: Number(videoContrastInput.value),
          videoSaturation: Number(videoSaturationInput.value),
          enableYadif: enableYadifCheck.checked,
          enableUnsharp: enableUnsharpCheck.checked,
          unsharpStrength: Number(unsharpStrengthInput.value),
          colorSpace: colorSpaceSel.value,
          videoRefs: videoRefsInput.value ? Number(videoRefsInput.value) : 0,
          x264AQStrength: Number(x264AQStrengthInput.value),
          nvencSpatialAQ: nvencSpatialAQCheck.checked,
          nvencTemporalAQ: nvencTemporalAQCheck.checked,
          videoScThreshold: videoScThresholdInput.value ? Number(videoScThresholdInput.value) : 0,
          ctuSize: Number(ctuSizeSel.value),
          rdLevel: Number(rdLevelSel.value),
        })
      } else {
        Object.assign(body, { vcodec: '' })
      }
      if (audioAuto.checked) {
        Object.assign(body, {
          acodec: acodec.value,
          channels: channelsSel.value,
          sampleRate: sampleRateSel.value,
          sampleRateCustom: sampleRateCustomInput.value ? Number(sampleRateCustomInput.value) : 0,
          audioBitrate: audioBitrateSel.value,
          audioBitrateCustom: audioBitrateCustomInput.value ? Number(audioBitrateCustomInput.value) : 0,
          sampleFmt: sampleFmtSel.value,
          aacProfile: aacProfileSel.value,
          volume: Number(volumeInput.value),
          silence: silenceCheck.checked,
          audioDynNorm: audioDynNormCheck.checked,
          audioCutoff: audioCutoffInput.value ? Number(audioCutoffInput.value) : 0,
          opusCompLevel: Number(opusCompLevelSel.value),
          audioSyncOffset: audioSyncOffsetInput.value ? Number(audioSyncOffsetInput.value) : 0,
        })
      } else {
        Object.assign(body, { acodec: '' })
      }
      Object.assign(body, {
        hwAccel: hwAccelSel.value,
        movFastStart: movFastStartCheck.checked,
        threadCount: threadCountSel.value,
        extraArgs: extraArgs.value,
      })
    }
    Object.assign(body, {
      outputSuffix: outputSuffixInput.value.trim() || '_trans.mp4',
    })
    try {
      if (p) await api.updateProfile(p.id, { ...p, ...body })
      else await api.createProfile(body)
      await store.loadProfiles()
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
