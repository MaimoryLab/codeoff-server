package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaimoryLab/codeoff-server/internal/daemon"
	"github.com/mdp/qrterminal/v3"
	qrcode "github.com/skip2/go-qrcode"
)

var (
	jsonOutput bool
	qrDir      string
	qrTerminal bool
)

type client struct {
	baseURL string
	token   string
}

type overview struct {
	AppServer        runtimeState `json:"appServer"`
	Tunnel           tunnelState  `json:"tunnel"`
	ControlAddr      string       `json:"controlAddr"`
	ControlAddrs     []string     `json:"controlAddrs"`
	ServerUUID       string       `json:"serverUuid"`
	ConnectedClients int          `json:"connectedClients"`
}

type runtimeState struct {
	Running  bool   `json:"running"`
	Starting bool   `json:"starting"`
	Stopping bool   `json:"stopping"`
	Error    string `json:"error"`
}

type tunnelState struct {
	Running  bool   `json:"running"`
	Starting bool   `json:"starting"`
	Stopping bool   `json:"stopping"`
	External bool   `json:"external"`
	URL      string `json:"url"`
	Error    string `json:"error"`
}

type device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LastSeen  string `json:"lastSeen"`
	Connected bool   `json:"connected"`
}

type pairing struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

func main() {
	statePath := flag.String("state", "", "daemon state file")
	flag.BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	flag.StringVar(&qrDir, "qr-dir", "", "directory for QR PNG files (default: render in terminal)")
	flag.BoolVar(&qrTerminal, "qr-terminal", false, "render the QR code in the terminal")
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}

	path := *statePath
	if path == "" {
		var err error
		path, err = daemon.DefaultStatePath()
		if err != nil {
			fatal(err)
		}
	}
	state, err := daemon.LoadState(path)
	if err != nil {
		fatal(fmt.Errorf("load daemon state: %w", err))
	}
	client := &client{baseURL: state.ControlAddr, token: state.AdminToken}
	args := flag.Args()

	switch args[0] {
	case "status":
		raw := must(client.get("/api/v1/admin/status"))
		if jsonOutput {
			printJSON(raw)
		} else {
			printStatus(raw)
		}
	case "devices":
		raw := must(client.get("/api/v1/admin/devices"))
		if jsonOutput {
			printJSON(raw)
		} else {
			printDevices(raw)
		}
	case "pair":
		printPair(client)
	case "connect":
		printConnect(client)
	case "revoke":
		if len(args) != 2 {
			fatal(errors.New("usage: codeoff-cli revoke DEVICE_ID"))
		}
		raw := must(client.post("/api/v1/admin/revoke", map[string]string{"id": args[1]}))
		printAction("device revoked", raw)
	case "restart":
		if len(args) != 2 || (args[1] != "appserver" && args[1] != "tunnel") {
			fatal(errors.New("usage: codeoff-cli restart appserver|tunnel"))
		}
		raw := must(client.post("/api/v1/admin/restart/"+args[1], nil))
		printAction(args[1]+" restarted", raw)
	case "shutdown":
		raw := must(client.post("/api/v1/admin/shutdown", nil))
		printAction("daemon shutdown requested", raw)
	default:
		fatal(fmt.Errorf("unknown command %q", args[0]))
	}
}

func (c *client) get(path string) (json.RawMessage, error) {
	return c.request(http.MethodGet, path, nil)
}

func (c *client) post(path string, body any) (json.RawMessage, error) {
	return c.request(http.MethodPost, path, body)
}

func (c *client) request(method, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.baseURL, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("daemon returned %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	if !json.Valid(data) {
		return nil, errors.New("daemon returned invalid JSON")
	}
	return bytes.TrimSpace(data), nil
}

func printPair(c *client) {
	statusRaw := must(c.get("/api/v1/admin/status"))
	var status overview
	if err := json.Unmarshal(statusRaw, &status); err != nil {
		fatal(fmt.Errorf("decode daemon status: %w", err))
	}

	raw := must(c.post("/api/v1/admin/pair", nil))
	var pair pairing
	if err := json.Unmarshal(raw, &pair); err != nil {
		fatal(fmt.Errorf("decode pairing response: %w", err))
	}
	payload := map[string]any{
		"serverUuid":      status.ServerUUID,
		"listenAddresses": status.ControlAddrs,
		"tunnelAddress":   status.Tunnel.URL,
	}
	payload["pairingCode"] = pair.Token
	qrPath, qrErr := outputQRCode(payload)
	if jsonOutput {
		output := map[string]any{
			"serverUuid":      status.ServerUUID,
			"listenAddresses": status.ControlAddrs,
			"tunnelAddress":   status.Tunnel.URL,
			"qrPath":          qrPath,
		}
		output["token"] = pair.Token
		output["expiresAt"] = pair.ExpiresAt
		if qrErr != nil {
			output["qrError"] = qrErr.Error()
		}
		printValue(output)
		return
	}
	fmt.Printf("Pairing code: %s\nExpires: %s\n", pair.Token, formatTime(pair.ExpiresAt))
	if qrTerminal || qrDir == "" {
		fmt.Println("QR code:")
		printTerminalQRCode(payload)
	}
	if qrPath != "" {
		fmt.Printf("QR code: %s\n", qrPath)
	} else if qrErr != nil {
		fmt.Printf("QR code: unavailable (%s)\n", qrErr)
	}
}

func printConnect(c *client) {
	statusRaw := must(c.get("/api/v1/admin/status"))
	var status overview
	if err := json.Unmarshal(statusRaw, &status); err != nil {
		fatal(fmt.Errorf("decode daemon status: %w", err))
	}
	payload := map[string]any{
		"serverUuid":      status.ServerUUID,
		"listenAddresses": status.ControlAddrs,
		"tunnelAddress":   status.Tunnel.URL,
	}
	qrPath, qrErr := outputQRCode(payload)
	if jsonOutput {
		output := map[string]any{
			"serverUuid":      status.ServerUUID,
			"listenAddresses": status.ControlAddrs,
			"tunnelAddress":   status.Tunnel.URL,
			"qrPath":          qrPath,
		}
		if qrErr != nil {
			output["qrError"] = qrErr.Error()
		}
		printValue(output)
		return
	}
	fmt.Println("Connection QR code")
	if qrTerminal || qrDir == "" {
		fmt.Println("QR code:")
		printTerminalQRCode(payload)
	}
	if qrPath != "" {
		fmt.Printf("QR code: %s\n", qrPath)
	} else if qrErr != nil {
		fmt.Printf("QR code: unavailable (%s)\n", qrErr)
	}
}

func outputQRCode(payload any) (string, error) {
	if qrDir == "" {
		return "", nil
	}
	return writeQRCode(payload, qrDir)
}

func writeQRCode(payload any, dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create QR directory: %w", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("codeoff-%d.png", time.Now().UnixNano()))
	if err := qrcode.WriteFile(string(data), qrcode.Medium, 512, path); err != nil {
		return "", fmt.Errorf("write QR code: %w", err)
	}
	return filepath.Abs(path)
}

func printTerminalQRCode(payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		fatal(fmt.Errorf("encode QR payload: %w", err))
	}
	qrterminal.GenerateHalfBlock(string(data), qrterminal.M, os.Stdout)
}

func printStatus(raw json.RawMessage) {
	var status overview
	if err := json.Unmarshal(raw, &status); err != nil {
		fatal(fmt.Errorf("decode status: %w", err))
	}
	fmt.Printf("Server UUID: %s\nControl API: %s\nConnected clients: %d\n", status.ServerUUID, status.ControlAddr, status.ConnectedClients)
	fmt.Printf("App-server: %s\n", stateLabel(status.AppServer.Running, status.AppServer.Starting, status.AppServer.Stopping, status.AppServer.Error))
	fmt.Printf("CF Tunnel: %s", stateLabel(status.Tunnel.Running, status.Tunnel.Starting, status.Tunnel.Stopping, status.Tunnel.Error))
	if status.Tunnel.URL != "" {
		fmt.Printf(" (%s)", status.Tunnel.URL)
	}
	fmt.Println()
}

func printDevices(raw json.RawMessage) {
	var devices []device
	if err := json.Unmarshal(raw, &devices); err != nil {
		fatal(fmt.Errorf("decode devices: %w", err))
	}
	if len(devices) == 0 {
		fmt.Println("No devices paired.")
		return
	}
	fmt.Printf("Devices (%d):\n", len(devices))
	for _, device := range devices {
		state := "disconnected"
		if device.Connected {
			state = "connected"
		}
		fmt.Printf("- %s [%s] %s, last seen %s\n", device.Name, device.ID, state, formatTime(device.LastSeen))
	}
}

func printAction(message string, raw json.RawMessage) {
	if jsonOutput {
		printJSON(raw)
		return
	}
	fmt.Println(message + ".")
}

func stateLabel(running, starting, stopping bool, errText string) string {
	if starting {
		return "starting"
	}
	if stopping {
		return "stopping"
	}
	if running {
		return "running"
	}
	if errText != "" {
		return "error: " + errText
	}
	return "stopped"
}

func formatTime(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04:05 MST")
}

func printJSON(value json.RawMessage) {
	_, _ = os.Stdout.Write(value)
	_, _ = os.Stdout.Write([]byte{'\n'})
}

func printValue(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatal(err)
	}
	printJSON(data)
}

func must(value json.RawMessage, err error) json.RawMessage {
	if err != nil {
		fatal(err)
	}
	return value
}

func usage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, `Usage:
  %s [flags] <command> [arguments]

Commands:
  status                   Show daemon and service status
  devices                  List paired devices
  connect                  Create a connection QR code
  pair                     Create a pairing code and QR code
  revoke DEVICE_ID         Revoke a paired device
  restart appserver|tunnel Restart a service
  shutdown                 Request daemon shutdown

Flags:
  --state PATH             Daemon state file
  --json                   Print machine-readable JSON
  --qr-dir DIR             QR PNG output directory (default: terminal)
  --qr-terminal             Render QR code in the terminal

Examples:
  %s status
  %s --json devices
  %s --qr-dir ./qr pair
`, name, name, name, name)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
