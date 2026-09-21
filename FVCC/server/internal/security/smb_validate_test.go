package security

import (
	"testing"

	"fvcc/internal/store/model"
)

func TestValidateSMBPathNonSMB(t *testing.T) {
	err := ValidateSMBPath(model.Settings{TransferMode: "local"}, "/mnt/media/a.mp4")
	if err != nil {
		t.Errorf("非 SMB 模式应直接通过, got %v", err)
	}
	err = ValidateSMBPath(model.Settings{TransferMode: "smb", SMBUser: ""}, "/mnt/media/a.mp4")
	if err != nil {
		t.Errorf("SMB 模式未配置用户应直接通过, got %v", err)
	}
}
