package integrationtest

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

const closedLoopCaseIDEnv = "CLOSED_LOOP_CASE_ID"

func configureTestChildProcess(command *exec.Cmd) {
	// The closed-loop runner already places the case in an isolated process
	// group and rejects descendants that escape it. Ordinary test runs still
	// need a child-owned group so cleanup can signal the whole local tree.
	if os.Getenv(closedLoopCaseIDEnv) == "" {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}

func signalTestChildProcess(command *exec.Cmd, signal syscall.Signal) error {
	if command.Process == nil {
		return os.ErrProcessDone
	}

	target := command.Process.Pid
	if command.SysProcAttr != nil && command.SysProcAttr.Setpgid {
		target = -target
	}
	err := syscall.Kill(target, signal)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
