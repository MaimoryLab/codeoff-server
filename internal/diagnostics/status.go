package diagnostics

import (
	"context"
	"os/exec"
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

// Check probes the commands without changing the user's environment.
func Check(ctx context.Context) Snapshot {
	if ctx == nil {
		ctx = context.Background()
	}

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
