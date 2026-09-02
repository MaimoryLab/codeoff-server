package devices

import (
	"errors"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestPairExchangeAuthenticateAndRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Z0-9]{8}$`).MatchString(pairing.Token) {
		t.Fatalf("pairing token = %q", pairing.Token)
	}
	if !store.PairingActive() {
		t.Fatal("new pairing is not active")
	}
	device, token, err := store.Exchange(pairing.Token, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Exchange(pairing.Token, "Other"); !errors.Is(err, ErrInvalidPairingToken) {
		t.Fatalf("pairing token was reused: %v", err)
	}
	if store.PairingActive() {
		t.Fatal("consumed pairing is active")
	}
	if authenticated, ok := store.Authenticate(token); !ok || authenticated.ID != device.ID {
		t.Fatalf("authentication failed: %+v, %v", authenticated, ok)
	}
	disconnect := store.Connect(device.ID)
	if devices := store.List(); len(devices) != 1 || !devices[0].Connected {
		t.Fatalf("connected devices = %+v", devices)
	}
	if count := store.ConnectedCount(); count != 1 {
		t.Fatalf("connected count = %d, want 1", count)
	}
	disconnect()
	if store.List()[0].Connected {
		t.Fatal("disconnected device is still connected")
	}
	if count := store.ConnectedCount(); count != 0 {
		t.Fatalf("connected count = %d, want 0", count)
	}
	store.HoldThread(device.ID, "thread-1")
	store.HoldThread(device.ID, "thread-2")
	if count := store.List()[0].ThreadCount; count != 2 {
		t.Fatalf("thread count = %d, want 2", count)
	}
	store.ReleaseThread("thread-1")
	if count := store.List()[0].ThreadCount; count != 1 {
		t.Fatalf("thread count after release = %d, want 1", count)
	}
	if err := store.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Authenticate(token); ok {
		t.Fatal("revoked token still authenticates")
	}
}

func TestPairingExpires(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store.now = func() time.Time { return now }
	pairing, err := store.NewPairing()
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now.Add(11 * time.Minute) }
	if store.PairingActive() {
		t.Fatal("expired pairing is active")
	}
	if _, _, err := store.Exchange(pairing.Token, "Phone"); !errors.Is(err, ErrInvalidPairingToken) {
		t.Fatalf("expired token accepted: %v", err)
	}
}

func TestDevicesPersistWithoutPlaintextToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store, _ := Open(path)
	pairing, _ := store.NewPairing()
	_, token, err := store.Exchange(pairing.Token, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Authenticate(token); !ok {
		t.Fatal("persisted token hash did not authenticate")
	}
}

func TestServerIdentityPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	identity := store.Server()
	if identity.ID == "" || identity.Name == "" {
		t.Fatalf("server identity = %+v", identity)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Server() != identity {
		t.Fatalf("server identity changed: %v -> %v", identity, reopened.Server())
	}
}
