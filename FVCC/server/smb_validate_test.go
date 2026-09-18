package main

import "testing"

func TestValidateSMBPathNonSMB(t *testing.T) {
	h := &Handlers{}
	err := h.validateSMBPath(Settings{TransferMode: "local"}, "/mnt/media/a.mp4")
	if err != nil {
		t.Errorf("非 SMB 模式应直接通过, got %v", err)
	}
	err = h.validateSMBPath(Settings{TransferMode: "smb", SMBUser: ""}, "/mnt/media/a.mp4")
	if err != nil {
		t.Errorf("SMB 模式未配置用户应直接通过, got %v", err)
	}
}
