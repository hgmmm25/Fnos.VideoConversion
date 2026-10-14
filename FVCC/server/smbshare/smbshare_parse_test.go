package smbshare

import (
	"path/filepath"
	"testing"
)

// P3-7 覆盖率补强：smbshare 纯函数（配置解析 / 根归属 / 入参校验）单测。
// 涉及 /etc/samba 读取与用户查询的分支只测错误路径，保证 Windows 主机与 Linux CI 均可运行。

func TestParseShares(t *testing.T) {
	content := `
# 注释行
; 分号注释
[media]
	path = /vol1/media
comment = yes
[movies]
path=/vol2/movies
[broken]
path =
[empty]
other = x
`
	shares := parseShares(content)
	if len(shares) != 2 {
		t.Fatalf("期望解析出 2 个共享，实际 %d: %+v", len(shares), shares)
	}
	if shares[0].Name != "media" || shares[0].Path != "/vol1/media" {
		t.Fatalf("media 共享解析异常: %+v", shares[0])
	}
	if shares[1].Name != "movies" || shares[1].Path != "/vol2/movies" {
		t.Fatalf("movies 共享解析异常: %+v", shares[1])
	}
	// broken（空 path）与 empty（无 path 行）不入列表；非 path 行（comment/other）忽略
	for _, s := range shares {
		if s.Name == "broken" || s.Name == "empty" {
			t.Fatalf("不应收录无有效 path 的共享: %+v", s)
		}
	}
	if got := parseShares(""); got != nil {
		t.Fatalf("空内容应返回 nil，实际 %+v", got)
	}
}

func TestWithinOrEqual(t *testing.T) {
	// 斜杠字面量经 FromSlash 归一为本机分隔符，Windows 主机与 Linux CI 同一组用例成立
	cases := []struct {
		abs, root string
		want      bool
	}{
		{"/vol1/media", "/vol1/media", true},   // 同目录
		{"/vol1/media/a", "/vol1/media", true}, // 子路径
		{"/vol1/media2/a", "/vol1/media", false},
		{"/vol1/other", "/vol1/media", false},
	}
	for _, c := range cases {
		abs := filepath.FromSlash(c.abs)
		root := filepath.FromSlash(c.root)
		if got := withinOrEqual(abs, root); got != c.want {
			t.Errorf("withinOrEqual(%q, %q) = %v，期望 %v", abs, root, got, c.want)
		}
	}
}

func TestEmptyUsernameErrorPaths(t *testing.T) {
	if _, err := resolveUID(""); err == nil {
		t.Error("resolveUID 空用户名应报错")
	}
	if _, err := ListShares(""); err == nil {
		t.Error("ListShares 空用户名应报错")
	}
	if _, err := IsPathShared("", "/vol1/media"); err == nil {
		t.Error("IsPathShared 空用户名应报错")
	}
	if _, err := FindShareForPath("", "/vol1/media"); err == nil {
		t.Error("FindShareForPath 空用户名应报错")
	}
	if _, err := BuildSMBURL("10.0.0.1", "", "/vol1/media/a.mp4"); err == nil {
		t.Error("BuildSMBURL 空用户名应报错")
	}
}

func TestResolveUIDUnknownUser(t *testing.T) {
	// 不存在的用户：Windows（LSA 查询失败）与 Linux（NSS 未命中）均返回错误
	if _, err := resolveUID("fvcc-no-such-user-zz"); err == nil {
		t.Fatal("未知用户应报错")
	}
	if _, err := ListShares("fvcc-no-such-user-zz"); err == nil {
		t.Fatal("未知用户的 ListShares 应透传错误")
	}
}
