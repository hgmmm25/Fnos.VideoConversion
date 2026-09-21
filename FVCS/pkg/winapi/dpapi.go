package winapi

// DPAPI（Windows 数据保护 API）封装 —— 设计文档 07 §5.3
//
// 用途：FVCS 本地凭据档案（pkg/smb 的 CredStore）在落库前对 SMB 密码做加密，
// 使 SQLite 中不出现明文口令（验收点：secret_cipher 中查不到明文）。
//
// 作用域：使用 CRYPTPROTECT_LOCAL_MACHINE —— 本机任意账户可解，适配"服务以
// 不同账户运行（SYSTEM / 交互用户）+ 托盘端唤起"的场景；进程调用时禁止弹 UI
// （CRYPTPROTECT_UI_FORBIDDEN），避免无人值守下卡住。
//
// 说明：DPAPI 保护的是"本机静态存储"，不提供跨机迁移能力；如需迁移档案需重新录入。

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	crypt32                = windows.NewLazySystemDLL("crypt32.dll")
	cryptProtectDataProc   = crypt32.NewProc("CryptProtectData")
	cryptUnprotectDataProc = crypt32.NewProc("CryptUnprotectData")
	localFreeProc          = kernel32.NewProc("LocalFree")
)

const (
	cryptProtectUIForbidden  = 0x00000001
	cryptProtectLocalMachine = 0x00000004

	dpapiFlags = cryptProtectUIForbidden | cryptProtectLocalMachine
)

// dataBlob 对应 Win32 DATA_BLOB
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newDataBlob(data []byte) *dataBlob {
	if len(data) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
}

// bytes 复制 blob 内容（原缓冲区由 LocalFree 释放，必须先拷贝）
func (b *dataBlob) bytes() []byte {
	if b.pbData == nil || b.cbData == 0 {
		return nil
	}
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

// DPAPIProtect 加密明文，description 为可选描述（同样受保护，可留空）。
func DPAPIProtect(plain []byte, description string) ([]byte, error) {
	if len(plain) == 0 {
		return nil, fmt.Errorf("dpapi: 明文为空")
	}

	in := newDataBlob(plain)

	var descr *uint16
	if description != "" {
		if p, err := windows.UTF16PtrFromString(description); err == nil {
			descr = p
		}
	}

	var out dataBlob
	ret, _, callErr := cryptProtectDataProc.Call(
		uintptr(unsafe.Pointer(in)),
		uintptr(unsafe.Pointer(descr)),
		0,
		0,
		0,
		uintptr(dpapiFlags),
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("dpapi: CryptProtectData 失败: %v", callErr)
	}
	defer localFreeProc.Call(uintptr(unsafe.Pointer(out.pbData)))

	return out.bytes(), nil
}

// DPAPIUnprotect 解密由 DPAPIProtect 产生的密文。
func DPAPIUnprotect(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, fmt.Errorf("dpapi: 密文为空")
	}

	in := newDataBlob(cipher)

	var descr *uint16
	var out dataBlob
	ret, _, callErr := cryptUnprotectDataProc.Call(
		uintptr(unsafe.Pointer(in)),
		uintptr(unsafe.Pointer(&descr)),
		0,
		0,
		0,
		uintptr(dpapiFlags),
		uintptr(unsafe.Pointer(&out)),
	)
	if descr != nil {
		defer localFreeProc.Call(uintptr(unsafe.Pointer(descr)))
	}
	if ret == 0 {
		return nil, fmt.Errorf("dpapi: CryptUnprotectData 失败: %v", callErr)
	}
	defer localFreeProc.Call(uintptr(unsafe.Pointer(out.pbData)))

	return out.bytes(), nil
}
