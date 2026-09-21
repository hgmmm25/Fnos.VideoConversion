package api

// B-03：EDL 项目 CRUD（03 §4.2 / §4.3）。
//
// 约定：
//   - 失败响应统一 {ok:false, code, msg, detail?}（03 §4.1），不复用存量 {error} 风格；
//   - 本文件只做「契约级闸门」：非空、数值范围、片段时长关系、路径结构（L1/L2）。
//     完整白名单（扩展名、落地 EvalSymlinks、payload 未知字段、体积/深度上限）
//     由 D-02 的 FVCC/server/edl_validate.go 接管，二者不重复实现；
//   - 素材存在性预检、盘余量、选机等属于 B-04 renderEDLProject 的职责。

import (
	"fvcc/internal/security"
	"errors"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// ===== 错误码（03 §5.3）=====
const (
	errCodeRevConflict    = "E_REV_CONFLICT"
	errCodeProjectMissing = "E_PROJECT_NOT_FOUND" // 03 §5.3 未定义，实现补充（见 10 号台账）
	errCodeProjectNameUse = "E_PROJECT_NAME_USED" // 同上，对应 store.ErrProjectNameUsed
)

// ===== 约束（03 §2.5 / 07 §3.6）=====
const (
	edlMaxNameLen = 64
)

// edlProjectInput 新建/保存项目的请求体（03 §4.2）。
type edlProjectInput struct {
	Rev      int       `json:"rev"` // PUT 必带（03 §4.3）
	Name     string    `json:"name"`
	Timeline Timeline  `json:"timeline"`
	Clips    []EDLClip `json:"clips"`
}

// edlViolation 校验失败详情（03 §5.1：必须可定位到 field/index）。
type edlViolation struct {
	Msg    string
	Detail gin.H
}

// ===== Handlers =====

// listEDLProjects GET /edl/projects（03 §4.2：含 clipCount）。
//
// 响应同时携带 items（03 §4.2 既有契约，既有用例与外部消费方依赖该字段）
// 与 projects/total 别名：前端 api.listProjects 按 {projects,total} 解析，
// 服务端只回 items 会使剪辑页「历史项目列表」渲染为空（2026-09-16 修复）。
func (h *Handlers) listEDLProjects(c *gin.Context) {
	items := h.store.GetProjects()
	if items == nil {
		items = []ProjectSummary{}
	}
	c.JSON(http.StatusOK, gin.H{
		"items":    items,
		"projects": items,
		"total":    len(items),
	})
}

// getEDLProject GET /edl/projects/:id。
func (h *Handlers) getEDLProject(c *gin.Context) {
	id := c.Param("id")
	p, ok := h.store.GetProject(id)
	if !ok {
		edlErr(c, http.StatusNotFound, errCodeProjectMissing, "项目不存在", gin.H{"id": id})
		return
	}
	c.JSON(http.StatusOK, p)
}

// createEDLProject POST /edl/projects（03 §4.2）。
func (h *Handlers) createEDLProject(c *gin.Context) {
	in, ok := h.bindEDLProjectInput(c)
	if !ok {
		return
	}
	if v := validateProjectInput(in.Name, &in.Timeline, in.Clips); v != nil {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, v.Msg, v.Detail)
		return
	}

	p, err := h.store.CreateProject(Project{
		Name:     in.Name,
		Timeline: in.Timeline,
		Clips:    in.Clips,
	})
	if err != nil {
		if errors.Is(err, ErrProjectNameUsed) {
			edlErr(c, http.StatusConflict, errCodeProjectNameUse, "项目名称已存在", gin.H{"name": in.Name})
			return
		}
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, err.Error(), nil)
		return
	}
	c.JSON(http.StatusOK, p)
}

// updateEDLProject PUT /edl/projects/:id（乐观锁，03 §4.3）。
func (h *Handlers) updateEDLProject(c *gin.Context) {
	id := c.Param("id")
	in, ok := h.bindEDLProjectInput(c)
	if !ok {
		return
	}
	if in.Rev <= 0 {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, "PUT 必须携带 rev", gin.H{"field": "rev"})
		return
	}
	if v := validateProjectInput(in.Name, &in.Timeline, in.Clips); v != nil {
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, v.Msg, v.Detail)
		return
	}

	cur, err := h.store.UpdateProject(id, in.Rev, Project{
		Name:     in.Name,
		Timeline: in.Timeline,
		Clips:    in.Clips,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrRevConflict):
			edlErr(c, http.StatusConflict, errCodeRevConflict, "项目已在其他页面被修改",
				gin.H{"serverRev": cur.Rev, "clientRev": in.Rev})
		case errors.Is(err, ErrProjectNotFound):
			edlErr(c, http.StatusNotFound, errCodeProjectMissing, "项目不存在", gin.H{"id": id})
		case errors.Is(err, ErrProjectNameUsed):
			edlErr(c, http.StatusConflict, errCodeProjectNameUse, "项目名称已存在", gin.H{"name": in.Name})
		default:
			edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, err.Error(), nil)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"rev": cur.Rev, "savedAt": cur.UpdatedAt})
}

// deleteEDLProject DELETE /edl/projects/:id（仅删元数据，不动素材）。
func (h *Handlers) deleteEDLProject(c *gin.Context) {
	id := c.Param("id")
	if !h.store.DeleteProject(id) {
		edlErr(c, http.StatusNotFound, errCodeProjectMissing, "项目不存在", gin.H{"id": id})
		return
	}
	h.store.AppendAudit(AuditEntry{
		Actor:  security.GetEDLActor(c),
		Action: "project.delete",
		Target: id,
		Result: "ok",
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ===== 内部：解析与校验 =====

// bindEDLProjectInput 限制体积并解析请求体；失败时已写入响应，返回 ok=false。
func (h *Handlers) bindEDLProjectInput(c *gin.Context) (edlProjectInput, bool) {
	var in edlProjectInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, edlMaxBodyBytes)
	if err := c.ShouldBindJSON(&in); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			edlErr(c, http.StatusRequestEntityTooLarge, errCodeEDLTooLarge,
				"项目 JSON 超过 256 KB 上限", gin.H{"maxBytes": edlMaxBodyBytes})
			return in, false
		}
		edlErr(c, http.StatusBadRequest, errCodeEDLInvalid, "请求体解析失败: "+err.Error(), nil)
		return in, false
	}
	in.Name = strings.TrimSpace(in.Name)
	return in, true
}

// validateProjectInput 契约级校验，并按 03 §2.2 回填时间线默认值。
func validateProjectInput(name string, tl *Timeline, clips []EDLClip) *edlViolation {
	if name == "" {
		return &edlViolation{Msg: "项目名称不能为空", Detail: gin.H{"field": "name"}}
	}
	if utf8.RuneCountInString(name) > edlMaxNameLen {
		return &edlViolation{Msg: "项目名称过长", Detail: gin.H{"field": "name", "maxLen": edlMaxNameLen}}
	}
	if v := validateTimeline(tl); v != nil {
		return v
	}
	if len(clips) > edlMaxClips {
		return &edlViolation{Msg: "clips 数量超出上限", Detail: gin.H{"field": "clips", "count": len(clips), "max": edlMaxClips}}
	}

	var total int64
	for i := range clips {
		cl := &clips[i]
		if v := validateClipFile(cl.File); v != nil {
			v.Detail["index"] = i
			return v
		}
		if cl.ClipID == "" || len(cl.ClipID) > 64 {
			return &edlViolation{Msg: "clipId 非法", Detail: gin.H{"field": "clips", "index": i}}
		}
		if cl.AssetID == "" || len(cl.AssetID) > 64 {
			return &edlViolation{Msg: "assetId 非法", Detail: gin.H{"field": "clips", "index": i}}
		}
		if cl.Transition != nil {
			return &edlViolation{Msg: "transition 在 P0 必须为空", Detail: gin.H{"field": "clips", "index": i}}
		}
		// speed：0 视为缺省（store 回填 1.0），非 0 必须恒为 1.0（03 §2.5）
		if cl.Speed != 0 && math.Abs(cl.Speed-1.0) >= 1e-6 {
			return &edlViolation{Msg: "speed 在 P0 必须为 1.0", Detail: gin.H{"field": "clips", "index": i}}
		}
		if cl.InMs < 0 {
			return &edlViolation{Msg: "入点为负", Detail: gin.H{"field": "clips", "index": i, "inMs": cl.InMs}}
		}
		if cl.OutMs <= cl.InMs {
			return &edlViolation{Msg: "出点不晚于入点", Detail: gin.H{"field": "clips", "index": i}}
		}
		if cl.OutMs-cl.InMs < edlMinClipMs {
			return &edlViolation{Msg: "片段时长不足 100ms", Detail: gin.H{"field": "clips", "index": i, "durationMs": cl.OutMs - cl.InMs}}
		}
		if cl.SourceDurationMs > 0 && cl.OutMs > cl.SourceDurationMs {
			return &edlViolation{Msg: "出点超出素材时长", Detail: gin.H{"field": "clips", "index": i, "maxOutMs": cl.SourceDurationMs}}
		}
		total += cl.OutMs - cl.InMs
	}
	if total > edlMaxTotalMs {
		return &edlViolation{Msg: "clips 总时长超过 6 小时", Detail: gin.H{"field": "clips", "totalMs": total, "max": edlMaxTotalMs}}
	}
	return nil
}

// validateTimeline 校验时间线数值范围，缺省项回填 03 §2.2 默认值。
func validateTimeline(tl *Timeline) *edlViolation {
	if tl.FPS == 0 {
		tl.FPS = 30
	}
	if tl.FPS < 1 || tl.FPS > 120 {
		return &edlViolation{Msg: "timeline.fps 超出 1~120", Detail: gin.H{"field": "timeline.fps", "fps": tl.FPS}}
	}
	if tl.Width == 0 {
		tl.Width = 1920
	}
	if tl.Height == 0 {
		tl.Height = 1080
	}
	if tl.Width < 16 || tl.Width > 7680 || tl.Width%2 != 0 {
		return &edlViolation{Msg: "timeline.width 非法（16~7680 且为偶数）", Detail: gin.H{"field": "timeline.width", "width": tl.Width}}
	}
	if tl.Height < 16 || tl.Height > 7680 || tl.Height%2 != 0 {
		return &edlViolation{Msg: "timeline.height 非法（16~7680 且为偶数）", Detail: gin.H{"field": "timeline.height", "height": tl.Height}}
	}
	if tl.SampleRate == 0 {
		tl.SampleRate = 48000
	}
	if tl.SampleRate < 8000 || tl.SampleRate > 192000 {
		return &edlViolation{Msg: "timeline.sampleRate 非法（8000~192000）", Detail: gin.H{"field": "timeline.sampleRate", "sampleRate": tl.SampleRate}}
	}
	if !tl.Audio {
		return &edlViolation{Msg: "P0 必须开启音频导出（timeline.audio=true）", Detail: gin.H{"field": "timeline.audio"}}
	}
	return nil
}

// validateClipFile 片段的相对路径 L1~L3 校验（07 §3.2）。
// 规则实现统一收敛到 edl_validate.go（D-02）；本函数只做「项目 CRUD 场景」的消息映射：
// 项目保存阶段的路径非法统一记为 400 E_EDL_INVALID，不在此处区分 E_ASSET_NOT_IN_ROOT。
func validateClipFile(f string) *edlViolation {
	if e := validateRelPath(f, edlAllowedSourceExt); e != nil {
		return &edlViolation{Msg: clipFileMsgFor(e), Detail: gin.H{"field": "clips.file"}}
	}
	return nil
}

// clipFileMsgFor 把 D-02 路径校验错误映射为项目 CRUD 的既有文案（保持 03 §4.2 响应契约）。
func clipFileMsgFor(e *edlValidationError) string {
	switch {
	case strings.Contains(e.Msg, "反斜杠"), strings.Contains(e.Msg, "绝对路径"):
		return "clips.file 必须以 POSIX 相对路径书写"
	case strings.Contains(e.Msg, "非法路径段"):
		return "clips.file 含非法路径段"
	case strings.Contains(e.Msg, "层级"):
		return "clips.file 目录层级超过 8 段"
	case strings.Contains(e.Msg, "单段长度"):
		return "clips.file 单段长度超过 100"
	case strings.Contains(e.Msg, "控制字符"):
		return "clips.file 含控制字符"
	case strings.Contains(e.Msg, "扩展名"):
		return "clips.file 扩展名不在白名单（.mp4/.mov/.mkv/.m4v/.avi/.mxf）"
	case strings.Contains(e.Msg, "长度超过 255"), strings.Contains(e.Msg, "为空"):
		return "clips.file 长度非法（1~255）"
	default:
		return "clips.file 含非法字符"
	}
}
