// Package fynetheme 让 FVCS 桌面 GUI（托盘主窗口/设置窗口）的主题色贴近 FVCC 前端蓝调。
// v1.3.0 批次⑥：主色与 FVCC style.css 的 --c-primary 方向A 令牌对齐——
// 浅色 #2563eb（rgb 37 99 235）、暗色 #3b82f6（rgb 59 130 246）。
package fynetheme

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	primaryLight = color.NRGBA{R: 0x25, G: 0x63, B: 0xEB, A: 0xFF}
	primaryDark  = color.NRGBA{R: 0x3B, G: 0x82, B: 0xF6, A: 0xFF}
)

type fvccTheme struct {
	fyne.Theme
	primary color.Color
}

func (t *fvccTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return t.primary
	}
	return t.Theme.Color(name, variant)
}

// Light 返回贴近 FVCC 蓝调的浅色主题（基于 Fyne 默认主题）。
func Light() fyne.Theme {
	return &fvccTheme{Theme: theme.DefaultTheme(), primary: primaryLight}
}

// Dark 返回贴近 FVCC 蓝调的暗色主题（基于 Fyne 暗色主题）。
func Dark() fyne.Theme {
	return &fvccTheme{Theme: theme.DarkTheme(), primary: primaryDark}
}
