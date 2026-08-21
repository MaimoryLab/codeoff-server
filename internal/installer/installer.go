package installer

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

type Progress func(string)

type Installer struct {
	progress Progress
}

func New(progress Progress) *Installer {
	return &Installer{progress: progress}
}

func (i *Installer) InstallNode(ctx context.Context) error {
	switch runtime.GOOS {
	case "darwin":
		return i.run(ctx, "brew", "install", "node")
	case "windows":
		return i.run(ctx, "winget", "install", "--id", "OpenJS.NodeJS.LTS", "--exact", "--accept-package-agreements", "--accept-source-agreements")
	case "linux":
		if _, err := exec.LookPath("apt-get"); err != nil {
			return errors.New("automatic Node.js installation requires Homebrew, winget, or apt-get")
		}
		if err := i.run(ctx, "sudo", "apt-get", "update"); err != nil {
			return err
		}
		return i.run(ctx, "sudo", "apt-get", "install", "-y", "nodejs", "npm")
	default:
		return fmt.Errorf("automatic Node.js installation is unsupported on %s", runtime.GOOS)
	}
}

func (i *Installer) InstallCodex(ctx context.Context) error {
	return i.run(ctx, "npm", "install", "--global", "@openai/codex@latest")
}

func (i *Installer) run(ctx context.Context, name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s is not installed", name)
	}
	if i.progress != nil {
		i.progress("Running " + name)
	}
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, output)
	}
	if i.progress != nil {
		i.progress(name + " finished")
	}
	return nil
}
