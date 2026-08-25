package diagnostics

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ToolStatus describes one locally discoverable runtime or executable.
type ToolStatus struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Snapshot is the status shown by the desktop UI and local control API.
type Snapshot struct {
	Platform     string     `json:"platform"`
	Architecture string     `json:"architecture"`
	CheckedAt    time.Time  `json:"checkedAt"`
	Node         ToolStatus `json:"node"`
	Codex        ToolStatus `json:"codex"`
	AppServer    ToolStatus `json:"appServer"`
	Cloudflared  ToolStatus `json:"cloudflared"`
}

// Check probes commands using the user's login-shell PATH when running as a macOS app.
func Check(ctx context.Context) Snapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	configurePath()

	node := checkCommand(ctx, "node", "--version")
	codex := checkCommand(ctx, "codex", "--version")
	appServer := checkCommand(ctx, "codex", "app-server", "--help")
	cloudflared := checkCommand(ctx, "cloudflared", "--version")

	return Snapshot{
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
		CheckedAt:    time.Now().UTC(),
		Node:         node,
		Codex:        codex,
		AppServer:    appServer,
		Cloudflared:  cloudflared,
	}
}

func configurePath() {
	if runtime.GOOS != "darwin" {
		return
	}
	executable, err := os.Executable()
	if err != nil || !strings.Contains(filepath.ToSlash(executable), ".app/Contents/MacOS/") {
		return
	}

	environment := os.Environ()
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/zsh"
	}
	if !filepath.IsAbs(shell) || !isExecutable(shell) {
		return
	}

	path := loginShellPath(shell, environment)
	if path != "" {
		_ = os.Setenv("PATH", path)
	}
}

func loginShellPath(shell string, environment []string) string {
	// ponytail: cap shell startup at three seconds; keep the launchd PATH if setup hangs.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, shell, "-lic", `printf '\036%s\037' "$PATH"`)
	command.Env = environment
	command.WaitDelay = 250 * time.Millisecond
	output, _ := command.Output()
	if ctx.Err() != nil {
		return ""
	}
	start := bytes.LastIndexByte(output, 0x1e)
	if start < 0 {
		return ""
	}
	end := bytes.IndexByte(output[start+1:], 0x1f)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(string(output[start+1 : start+1+end]))
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func checkCommand(ctx context.Context, name string, args ...string) ToolStatus {
	path, err := exec.LookPath(name)
	if err != nil {
		return ToolStatus{Error: "not found"}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, path, args...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return ToolStatus{Path: path, Error: message}
	}

	version := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	return ToolStatus{Installed: true, Path: path, Version: version}
}
