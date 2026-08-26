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

	"github.com/MaimoryLab/codeoff-server/internal/daemon"
)

func main() {
	statePath := flag.String("state", "", "daemon state file")
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
	case "status", "info":
		mustJSON(client.get("/api/v1/admin/status"))
	case "devices":
		mustJSON(client.get("/api/v1/admin/devices"))
	case "pair":
		mustJSON(client.post("/api/v1/admin/pair", nil))
	case "revoke":
		if len(args) != 2 {
			fatal(errors.New("usage: codeoff-cli revoke DEVICE_ID"))
		}
		mustJSON(client.post("/api/v1/admin/revoke", map[string]string{"id": args[1]}))
	case "restart":
		if len(args) != 2 || (args[1] != "appserver" && args[1] != "tunnel") {
			fatal(errors.New("usage: codeoff-cli restart appserver|tunnel"))
		}
		mustJSON(client.post("/api/v1/admin/restart/"+args[1], nil))
	case "shutdown":
		mustJSON(client.post("/api/v1/admin/shutdown", nil))
	default:
		fatal(fmt.Errorf("unknown command %q", args[0]))
	}
}

type client struct {
	baseURL string
	token   string
}

func (c *client) get(path string) any {
	return c.request(http.MethodGet, path, nil)
}

func (c *client) post(path string, body any) any {
	return c.request(http.MethodPost, path, body)
}

func (c *client) request(method, path string, body any) any {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.baseURL, "/")+path, reader)
	if err != nil {
		fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		fatal(fmt.Errorf("daemon returned %s: %s", response.Status, strings.TrimSpace(string(data))))
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		fatal(fmt.Errorf("decode daemon response: %w", err))
	}
	return value
}

func mustJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(data))
}

func usage() {
	path, _ := filepath.Abs(os.Args[0])
	fmt.Fprintf(os.Stderr, "usage: %s [--state PATH] status|info|pair|devices|revoke ID|restart appserver|tunnel|shutdown\n", filepath.Base(path))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
