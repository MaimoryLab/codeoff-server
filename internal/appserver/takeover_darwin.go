package appserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type processInfo struct {
	pid     int
	parent  int
	command string
}

func terminateThreadOwner(ctx context.Context, codexHome, threadID string) error {
	if !validThreadID(threadID) {
		return errors.New("invalid thread id")
	}
	lockPath := filepath.Join(codexHome, "thread-writer-locks", threadID+".lock")
	output, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-t", "--", lockPath).Output()
	if err != nil {
		return fmt.Errorf("find thread owner: %w", err)
	}
	lines := strings.Fields(string(output))
	if len(lines) != 1 {
		return fmt.Errorf("expected one thread owner, found %d", len(lines))
	}
	pid, err := strconv.Atoi(lines[0])
	if err != nil || pid < 2 {
		return errors.New("invalid thread owner pid")
	}

	owner, err := inspectProcess(ctx, pid)
	if err != nil {
		return err
	}
	if filepath.Base(owner.command) != "codex" {
		return fmt.Errorf("refusing to terminate unexpected thread owner %q", owner.command)
	}
	target := owner
	if bundle := appBundle(owner.command); bundle != "" {
		for target.parent > 1 {
			parent, err := inspectProcess(ctx, target.parent)
			if err != nil || !strings.HasPrefix(parent.command, bundle+string(filepath.Separator)) {
				break
			}
			target = parent
		}
	}
	if target.pid == os.Getpid() {
		return errors.New("refusing to terminate codex-server")
	}
	process, err := os.FindProcess(target.pid)
	if err != nil {
		return fmt.Errorf("find owner process: %w", err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("terminate thread owner: %w", err)
	}
	return nil
}

func inspectProcess(ctx context.Context, pid int) (processInfo, error) {
	output, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "pid=", "-o", "ppid=", "-o", "comm=").Output()
	if err != nil {
		return processInfo{}, fmt.Errorf("inspect owner process: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 3 {
		return processInfo{}, errors.New("invalid owner process information")
	}
	parsedPID, pidErr := strconv.Atoi(fields[0])
	parent, parentErr := strconv.Atoi(fields[1])
	if pidErr != nil || parentErr != nil || parsedPID != pid {
		return processInfo{}, errors.New("invalid owner process information")
	}
	return processInfo{pid: pid, parent: parent, command: strings.Join(fields[2:], " ")}, nil
}

func appBundle(command string) string {
	const marker = ".app/Contents/"
	index := strings.Index(command, marker)
	if index < 0 {
		return ""
	}
	return command[:index+len(".app")]
}

func validThreadID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for index, char := range id {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
