package protocol

import (
	"reflect"
	"strings"
)

// ApplyJSONUpdates 将 partial（gin ShouldBindJSON 解码出的 map）按结构体 json tag
// 白名单映射到 dst，仅更新 partial 中出现的字段，保持部分更新（patch）语义。
//
// 设计动机（P1-1）：原 updateProfile 以 59 块手写 `if v, ok := updates["xxx"]; ok {...}`
// 实现字段映射，字段新增时极易漏改。本 helper 从目标结构体的 json tag 自动推导
// 白名单，新增字段零改动；类型不匹配或未知字段一律忽略，与旧实现行为一致。
func ApplyJSONUpdates(dst interface{}, partial map[string]interface{}) {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}

	// json tag -> 字段 映射（白名单）
	fields := make(map[string]reflect.Value)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		if idx := strings.Index(tag, ","); idx >= 0 {
			tag = tag[:idx]
		}
		fields[tag] = v.Field(i)
	}

	for key, raw := range partial {
		fv, ok := fields[key]
		if !ok || !fv.CanSet() {
			continue // 未知字段忽略
		}
		rv := reflect.ValueOf(raw)
		if !rv.IsValid() {
			continue
		}
		// JSON 数字统一解码为 float64，需按目标类型转换
		switch fv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if rv.Kind() == reflect.Float64 {
				fv.SetInt(int64(rv.Float()))
				continue
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if rv.Kind() == reflect.Float64 {
				fv.SetUint(uint64(rv.Float()))
				continue
			}
		case reflect.Float32, reflect.Float64:
			if rv.Kind() == reflect.Float64 {
				fv.SetFloat(rv.Float())
				continue
			}
		case reflect.Bool:
			if rv.Kind() == reflect.Bool {
				fv.SetBool(rv.Bool())
				continue
			}
		case reflect.String:
			if rv.Kind() == reflect.String {
				fv.SetString(rv.String())
				continue
			}
		default:
			if rv.Type().AssignableTo(fv.Type()) {
				fv.Set(rv)
				continue
			}
		}
		// 类型不匹配忽略（与旧实现"类型断言失败即跳过"一致）
	}
}
