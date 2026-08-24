package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/codex-server/internal/appserver"
	"github.com/MaimoryLab/codex-server/internal/control"
	"github.com/MaimoryLab/codex-server/internal/devices"
	"github.com/MaimoryLab/codex-server/internal/diagnostics"
	"github.com/MaimoryLab/codex-server/internal/installer"
	"github.com/MaimoryLab/codex-server/internal/tunnel"
)

const defaultListenAddr = "127.0.0.1:11037"

type AppService struct {
	mu           sync.RWMutex
	installMu    sync.Mutex
	status       diagnostics.Snapshot
	progress     string
	appServer    *appserver.Manager
	devices      *devices.Store
	tunnel       *tunnel.Manager
	controlURL   string
	controlAddr  string
	control      *control.Server
	listenAddr   string
	settingsPath string
	controlMu    sync.Mutex
}

type Overview struct {
	Environment diagnostics.Snapshot `json:"environment"`
	AppServer   appserver.State      `json:"appServer"`
	Tunnel      tunnel.State         `json:"tunnel"`
	ControlAddr string               `json:"controlAddr"`
	ListenAddr  string               `json:"listenAddr"`
}

func NewAppService() (*AppService, error) {
	devicePath, err := devices.DefaultPath()
	if err != nil {
		return nil, err
	}
	deviceStore, err := devices.Open(devicePath)
	if err != nil {
		return nil, err
	}
	settingsPath := filepath.Join(filepath.Dir(devicePath), "settings.json")
	listenAddr, err := loadListenAddr(settingsPath)
	if err != nil {
		return nil, err
	}
	service := &AppService{appServer: appserver.NewManager(), devices: deviceStore, tunnel: tunnel.NewManager(), listenAddr: listenAddr, settingsPath: settingsPath}
	service.RefreshStatus()
	return service, nil
}

func (s *AppService) NewPairing() (devices.Pairing, error) { return s.devices.NewPairing() }

func (s *AppService) PairingActive() bool { return s.devices.PairingActive() }

func (s *AppService) Devices() []devices.Device { return s.devices.List() }

func (s *AppService) RevokeDevice(id string) error { return s.devices.Revoke(id) }

func (s *AppService) Overview() Overview {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Overview{
		Environment: s.status,
		AppServer:   s.appServer.State(),
		Tunnel:      s.tunnel.State(),
		ControlAddr: s.controlAddr,
		ListenAddr:  s.listenAddr,
	}
}

func (s *AppService) startControlServer() error {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.mu.RLock()
	address := s.listenAddr
	s.mu.RUnlock()
	return s.openControlServer(address)
}

func (s *AppService) openControlServer(address string) error {
	server := control.New(func(context.Context) Overview { return s.Overview() }, s.devices, s.appServer)
	if err := server.Start(address); err != nil {
		return err
	}
	s.mu.Lock()
	s.control, s.controlURL, s.controlAddr = server, server.Addr(), server.LANAddr()
	s.mu.Unlock()
	return nil
}

func (s *AppService) SetListenAddr(address string) (Overview, error) {
	address, err := validateListenAddr(address)
	if err != nil {
		return s.Overview(), err
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.mu.RLock()
	oldAddress, oldServer := s.listenAddr, s.control
	s.mu.RUnlock()
	if address == oldAddress {
		return s.Overview(), saveListenAddr(s.settingsPath, address)
	}
	_ = s.tunnel.Stop()
	if oldServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = oldServer.Close(ctx)
		cancel()
	}
	s.mu.Lock()
	s.control = nil
	s.mu.Unlock()
	if err := s.openControlServer(address); err != nil {
		restoreErr := s.openControlServer(oldAddress)
		return s.Overview(), errors.Join(err, restoreErr)
	}
	s.mu.Lock()
	s.listenAddr = address
	s.mu.Unlock()
	if err := saveListenAddr(s.settingsPath, address); err != nil {
		return s.Overview(), err
	}
	return s.Overview(), nil
}

func (s *AppService) AppServerState() appserver.State {
	return s.appServer.State()
}

func (s *AppService) StartAppServer() (appserver.State, error) {
	status := s.RefreshStatus()
	if !status.Codex.Installed {
		return s.appServer.State(), errors.New("codex CLI is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.appServer.Start(ctx, status.Codex.Path)
}

func (s *AppService) StopAppServer() (appserver.State, error) {
	if err := s.appServer.Stop(); err != nil {
		return s.appServer.State(), err
	}
	return s.appServer.State(), nil
}

func (s *AppService) Shutdown() error {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.mu.RLock()
	server := s.control
	s.mu.RUnlock()
	var controlErr error
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		controlErr = server.Close(ctx)
		cancel()
	}
	return errors.Join(s.tunnel.Stop(), s.appServer.Stop(), controlErr)
}

func (s *AppService) TunnelState() tunnel.State { return s.tunnel.State() }

func (s *AppService) StartTunnel() (tunnel.State, error) {
	status := s.RefreshStatus()
	if !status.Cloudflared.Installed {
		return s.tunnel.State(), errors.New("cloudflared is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	s.mu.RLock()
	controlURL := s.controlURL
	s.mu.RUnlock()
	return s.tunnel.Start(ctx, status.Cloudflared.Path, controlURL)
}

func (s *AppService) StopTunnel() (tunnel.State, error) {
	if err := s.tunnel.Stop(); err != nil {
		return s.tunnel.State(), err
	}
	return s.tunnel.State(), nil
}

func (s *AppService) Status() diagnostics.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *AppService) RefreshStatus() diagnostics.Snapshot {
	status := diagnostics.Check(context.Background())
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
	return status
}

func (s *AppService) InstallNode() (diagnostics.Snapshot, error) {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := installer.New(s.setProgress).InstallNode(ctx); err != nil {
		return s.Status(), err
	}
	return s.RefreshStatus(), nil
}

func (s *AppService) InstallCodex() (diagnostics.Snapshot, error) {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := installer.New(s.setProgress).InstallCodex(ctx); err != nil {
		return s.Status(), err
	}
	return s.RefreshStatus(), nil
}

func (s *AppService) InstallCloudflared() (diagnostics.Snapshot, error) {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := installer.New(s.setProgress).InstallCloudflared(ctx); err != nil {
		return s.Status(), err
	}
	return s.RefreshStatus(), nil
}

func (s *AppService) InstallProgress() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.progress
}

func (s *AppService) setProgress(message string) {
	s.mu.Lock()
	s.progress = message
	s.mu.Unlock()
}

func loadListenAddr(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultListenAddr, nil
	}
	if err != nil {
		return "", err
	}
	var settings struct {
		ListenAddr string `json:"listenAddr"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", fmt.Errorf("read settings: %w", err)
	}
	return validateListenAddr(settings.ListenAddr)
}

func saveListenAddr(path, address string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(struct {
		ListenAddr string `json:"listenAddr"`
	}{address}, "", "  ")
	return os.WriteFile(path, data, 0o600)
}

func validateListenAddr(address string) (string, error) {
	address = strings.TrimSpace(address)
	host, portText, err := net.SplitHostPort(address)
	port, portErr := strconv.Atoi(portText)
	if err != nil || net.ParseIP(host).To4() == nil || portErr != nil || port < 1 || port > 65535 {
		return "", errors.New("listen address must be an IPv4 address and port, for example 127.0.0.1:11037")
	}
	return net.JoinHostPort(host, portText), nil
}
