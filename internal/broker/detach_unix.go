//go:build !windows

package broker

import (
	"os/exec"
	"syscall"
)

// detach starts the child in its own session so it outlives the agent CLI
// call (and a hung-up terminal) that launched it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
