package protocol

import (
	"testing"

	"fvcc/internal/store/model"
)

func TestApplyJSONUpdatesBasic(t *testing.T) {
	p := model.Profile{ID: "profile-1", Name: "old", Width: 1920, Crf: 23, DeleteSource: false, Volume: 1.0}
	updates := map[string]interface{}{
		"name":         "new",
		"width":        float64(1280),
		"crf":          float64(28),
		"deleteSource": true,
		"volume":       2.5,
	}
	ApplyJSONUpdates(&p, updates)
	if p.Name != "new" {
		t.Errorf("name = %q, want new", p.Name)
	}
	if p.Width != 1280 {
		t.Errorf("width = %d, want 1280", p.Width)
	}
	if p.Crf != 28 {
		t.Errorf("crf = %d, want 28", p.Crf)
	}
	if !p.DeleteSource {
		t.Error("deleteSource = false, want true")
	}
	if p.Volume != 2.5 {
		t.Errorf("volume = %v, want 2.5", p.Volume)
	}
}

func TestApplyJSONUpdatesPartialKeepsRest(t *testing.T) {
	p := model.Profile{ID: "profile-1", Name: "keep-name", Width: 1920, Height: 1080, Vcodec: "libx264"}
	ApplyJSONUpdates(&p, map[string]interface{}{"width": float64(640)})
	if p.Width != 640 {
		t.Errorf("width = %d, want 640", p.Width)
	}
	if p.Name != "keep-name" || p.Height != 1080 || p.Vcodec != "libx264" {
		t.Errorf("未更新的字段被意外覆盖: %+v", p)
	}
}

func TestApplyJSONUpdatesIgnoresUnknownAndBadType(t *testing.T) {
	p := model.Profile{ID: "profile-1", Name: "stable", Crf: 20}
	ApplyJSONUpdates(&p, map[string]interface{}{
		"notAField": "x",   // 未知字段忽略
		"crf":      "abc",  // 类型不匹配忽略
		"name":      123.0, // 类型不匹配忽略
	})
	if p.Name != "stable" || p.Crf != 20 {
		t.Errorf("未知/类型不匹配字段不应生效: %+v", p)
	}
}

func TestApplyJSONUpdatesNonPtrNoPanic(t *testing.T) {
	p := model.Profile{Name: "x"}
	ApplyJSONUpdates(p, map[string]interface{}{"name": "y"}) // 传值不传指针，不 panic
	if p.Name != "x" {
		t.Errorf("传值不应修改: %+v", p)
	}
}
