package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/codeoff-server/internal/devices"
)

type adminStub struct {
	store *devices.Store
}

func (a adminStub) Status(context.Context) any           { return map[string]string{"status": "ok"} }
func (a adminStub) NewPairing() (devices.Pairing, error) { return a.store.NewPairing() }
func (a adminStub) Devices() []devices.Device            { return a.store.List() }
func (a adminStub) RevokeDevice(id string) error         { return a.store.Revoke(id) }
func (adminStub) RestartAppServer() error                { return nil }
func (adminStub) RestartTunnel() error                   { return nil }
func (adminStub) Shutdown() error                        { return nil }

func TestAdminRoutesRequireToken(t *testing.T) {
	store, err := devices.Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewWithOptions(func(context.Context) map[string]bool { return map[string]bool{} }, store, Options{
		Admin: adminStub{store: store}, AdminToken: "secret", AppServers: []AppServer{nil},
	})
	httpServer := httptest.NewServer(server.httpServer.Handler)
	t.Cleanup(httpServer.Close)
	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/admin/status", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.StatusCode)
	}
	request.Header.Set("Authorization", "Bearer secret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized status = %d", response.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" {
		t.Fatalf("status = %#v", result)
	}
}
