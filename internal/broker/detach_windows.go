//go:build windows

package broker

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) {
	const createNewProcessGroup = 0x00000200
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}
