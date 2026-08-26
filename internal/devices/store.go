package devices

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"
)

var ErrInvalidPairingToken = errors.New("invalid or expired pairing token")

type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
	Connected bool      `json:"connected,omitempty"`
}

type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Pairing struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type record struct {
	Device
	TokenHash []byte `json:"tokenHash"`
}

type Store struct {
	mu          sync.RWMutex
	path        string
	devices     []record
	pairingHash [sha256.Size]byte
	pairingEnd  time.Time
	now         func() time.Time
	connections map[string]int
	server      Server
}

type persisted struct {
	Devices []record `json:"devices"`
	Server  Server   `json:"server"`
}

func Open(path string) (*Store, error) {
	store := &Store{path: path, now: time.Now, connections: make(map[string]int)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store.initServer()
	}
	if err != nil {
		return nil, err
	}
	var saved persisted
	if err := json.Unmarshal(data, &saved); err == nil && saved.Server.ID != "" {
		store.devices, store.server = saved.Devices, saved.Server
		return store, nil
	}
	if err := json.Unmarshal(data, &store.devices); err != nil {
		return nil, err
	}
	return store.initServer()
}

func (s *Store) initServer() (*Store, error) {
	id, err := randomUUID()
	if err != nil {
		return nil, err
	}
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "Codeoff Server"
	}
	s.server = Server{ID: id, Name: name}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Server() Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.server
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(persisted{Devices: s.devices, Server: s.server}, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), "devices-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(s.path)
	}
	return os.Rename(temporaryName, s.path)
}

func DefaultPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "codex-remote", "devices.json"), nil
}

func (s *Store) NewPairing() (Pairing, error) {
	token := rand.Text()[:8]
	expiresAt := s.now().Add(10 * time.Minute).UTC()
	s.mu.Lock()
	s.pairingHash = sha256.Sum256([]byte(token))
	s.pairingEnd = expiresAt
	s.mu.Unlock()
	return Pairing{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Store) PairingActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.pairingEnd.IsZero() && s.now().Before(s.pairingEnd)
}

func (s *Store) Exchange(pairingToken, name string) (Device, string, error) {
	presented := sha256.Sum256([]byte(pairingToken))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairingEnd.IsZero() || !s.now().Before(s.pairingEnd) || subtle.ConstantTimeCompare(presented[:], s.pairingHash[:]) != 1 {
		return Device{}, "", ErrInvalidPairingToken
	}
	s.pairingEnd = time.Time{}
	s.pairingHash = [sha256.Size]byte{}

	id, err := randomToken(16)
	if err != nil {
		return Device{}, "", err
	}
	token, err := randomToken(32)
	if err != nil {
		return Device{}, "", err
	}
	if name == "" {
		name = "Mobile device"
	}
	now := s.now().UTC()
	device := Device{ID: id, Name: name, CreatedAt: now, LastSeen: now}
	hash := sha256.Sum256([]byte(token))
	s.devices = append(s.devices, record{Device: device, TokenHash: hash[:]})
	if err := s.saveLocked(); err != nil {
		s.devices = s.devices[:len(s.devices)-1]
		return Device{}, "", err
	}
	return device, token, nil
}

func (s *Store) Authenticate(token string) (Device, bool) {
	presented := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.devices {
		if subtle.ConstantTimeCompare(presented[:], s.devices[index].TokenHash) == 1 {
			// ponytail: last-seen is persisted on the next device mutation to avoid a disk write per request.
			s.devices[index].LastSeen = s.now().UTC()
			return s.devices[index].Device, true
		}
	}
	return Device{}, false
}

func (s *Store) List() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Device, len(s.devices))
	for index := range s.devices {
		result[index] = s.devices[index].Device
		result[index].Connected = s.connections[result[index].ID] > 0
	}
	return result
}

func (s *Store) Connect(id string) func() {
	s.mu.Lock()
	s.connections[id]++
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		if s.connections[id] <= 1 {
			delete(s.connections, id)
		} else {
			s.connections[id]--
		}
		s.mu.Unlock()
	}
}

func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := slices.Clone(s.devices)
	s.devices = slices.DeleteFunc(s.devices, func(candidate record) bool { return candidate.ID == id })
	if len(before) == len(s.devices) {
		return nil
	}
	if err := s.saveLocked(); err != nil {
		s.devices = before
		return err
	}
	delete(s.connections, id)
	return nil
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func randomUUID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", buffer[:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:]), nil
}
