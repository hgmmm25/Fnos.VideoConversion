package security

import (
	"errors"
	"fmt"

	"fvcc/internal/store/model"
	"fvcc/smbshare"
)

// ErrSMBNotShared 表示路径未位于任何 SMB 共享目录内（客户端可纠正的错误）。
// 调用方可用 errors.Is 区分状态码：未共享返回 403，其余校验失败返回 500。
var ErrSMBNotShared = errors.New("该路径未通过SMB共享，SMB模式下不可访问")

// ValidateSMBPath 在 SMB 传输模式下校验 path 是否位于已共享目录内。
// 非 SMB 模式或未配置 SMB 用户时直接返回 nil。
//
// P1-2：收敛 doScanDirectory/probeVideo/browseDirs/createTask 四处重复的
// "TransferMode == smb && SMBUser != '' → smbshare.IsPathShared" 校验逻辑。
// （P2-1 阶段 A：由 Handlers 方法改为独立函数，调用点同步调整，行为不变。）
func ValidateSMBPath(settings model.Settings, path string) error {
	if settings.TransferMode != "smb" || settings.SMBUser == "" {
		return nil
	}
	ok, err := smbshare.IsPathShared(settings.SMBUser, path)
	if err != nil {
		return fmt.Errorf("校验共享状态失败: %v", err)
	}
	if !ok {
		return ErrSMBNotShared
	}
	return nil
}
