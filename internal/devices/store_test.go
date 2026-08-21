package devices

import (
	"errors"
	"path/filepath"
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
	device, token, err := store.Exchange(pairing.Token, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Exchange(pairing.Token, "Other"); !errors.Is(err, ErrInvalidPairingToken) {
		t.Fatalf("pairing token was reused: %v", err)
	}
	if authenticated, ok := store.Authenticate(token); !ok || authenticated.ID != device.ID {
		t.Fatalf("authentication failed: %+v, %v", authenticated, ok)
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
