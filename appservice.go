package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
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
	settings     settings
	controlMu    sync.Mutex
}

type settings struct {
	ListenAddr       string `json:"listenAddr"`
	AppServerEnabled bool   `json:"appServerEnabled"`
	TunnelEnabled    bool   `json:"tunnelEnabled"`
	PreventSleep     bool   `json:"preventSleep"`
}

type Overview struct {
	Environment  diagnostics.Snapshot `json:"environment"`
	AppServer    appserver.State      `json:"appServer"`
	Tunnel       tunnel.State         `json:"tunnel"`
	ControlAddr  string               `json:"controlAddr"`
	ControlAddrs []string             `json:"controlAddrs"`
	ListenAddr   string               `json:"listenAddr"`
	ServerUUID   string               `json:"serverUuid"`
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
	loadedSettings, err := loadSettings(settingsPath)
	if err != nil {
		return nil, err
	}
	service := &AppService{appServer: appserver.NewManager(), devices: deviceStore, tunnel: tunnel.NewManager(), listenAddr: loadedSettings.ListenAddr, settingsPath: settingsPath, settings: loadedSettings}
	service.RefreshStatus()
	return service, nil
}

func (s *AppService) NewPairing() (devices.Pairing, error) { return s.devices.NewPairing() }

func (s *AppService) PairingActive() bool { return s.devices.PairingActive() }

func (s *AppService) Devices() []devices.Device { return s.devices.List() }

func (s *AppService) RevokeDevice(id string) error { return s.devices.Revoke(id) }

func (s *AppService) Overview() Overview {
	s.mu.RLock()
	status, controlAddr, listenAddr := s.status, s.controlAddr, s.listenAddr
	s.mu.RUnlock()
	return Overview{
		Environment:  status,
		AppServer:    s.appServer.State(),
		Tunnel:       s.tunnel.State(),
		ControlAddr:  controlAddr,
		ControlAddrs: controlAddresses(listenAddr),
		ListenAddr:   listenAddr,
		ServerUUID:   s.devices.Server().ID,
	}
}

func controlAddresses(address string) []string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	}
	if host != "0.0.0.0" {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	interfaceAddresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	addresses := make([]string, 0, len(interfaceAddresses))
	for _, address := range interfaceAddresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil {
			addresses = append(addresses, "http://"+net.JoinHostPort(ip.String(), port))
		}
	}
	slices.Sort(addresses)
	return slices.Compact(addresses)
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
		return s.Overview(), s.updateSettings(func(settings *settings) { settings.ListenAddr = address })
	}
	_ = s.tunnel.Stop()
	if err := s.updateSettings(func(settings *settings) { settings.TunnelEnabled = false }); err != nil {
		return s.Overview(), err
	}
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
	if err := s.updateSettings(func(settings *settings) { settings.ListenAddr = address }); err != nil {
		return s.Overview(), err
	}
	return s.Overview(), nil
}

func (s *AppService) AppServerState() appserver.State {
	return s.appServer.State()
}

func (s *AppService) StartAppServer() (appserver.State, error) {
	settingsErr := s.updateSettings(func(settings *settings) { settings.AppServerEnabled = true })
	state, err := s.startAppServer()
	return state, errors.Join(settingsErr, err)
}

func (s *AppService) startAppServer() (appserver.State, error) {
	status := s.RefreshStatus()
	if !status.Codex.Installed {
		return s.appServer.State(), errors.New("codex CLI is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.appServer.Start(ctx, status.Codex.Path)
}

func (s *AppService) StopAppServer() (appserver.State, error) {
	stopErr := s.appServer.Stop()
	settingsErr := s.updateSettings(func(settings *settings) { settings.AppServerEnabled = false })
	return s.appServer.State(), errors.Join(stopErr, settingsErr)
}

func (s *AppService) ToggleAppServer() (appserver.State, error) {
	if state := s.appServer.State(); state.Running || state.Starting {
		return s.StopAppServer()
	}
	return s.StartAppServer()
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
	settingsErr := s.updateSettings(func(settings *settings) { settings.TunnelEnabled = true })
	state, err := s.startTunnel()
	return state, errors.Join(settingsErr, err)
}

func (s *AppService) startTunnel() (tunnel.State, error) {
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
	stopErr := s.tunnel.Stop()
	settingsErr := s.updateSettings(func(settings *settings) { settings.TunnelEnabled = false })
	return s.tunnel.State(), errors.Join(stopErr, settingsErr)
}

func (s *AppService) ToggleTunnel() (tunnel.State, error) {
	if state := s.tunnel.State(); state.Running || state.Starting {
		return s.StopTunnel()
	}
	return s.StartTunnel()
}

func (s *AppService) restore() error {
	s.mu.RLock()
	settings := s.settings
	s.mu.RUnlock()
	var appServerErr, tunnelErr error
	if settings.AppServerEnabled {
		_, appServerErr = s.startAppServer()
	}
	if settings.TunnelEnabled {
		_, tunnelErr = s.startTunnel()
	}
	return errors.Join(appServerErr, tunnelErr)
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

func loadSettings(path string) (settings, error) {
	loaded := settings{ListenAddr: defaultListenAddr}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return loaded, nil
	}
	if err != nil {
		return loaded, err
	}
	if err := json.Unmarshal(data, &loaded); err != nil {
		return loaded, fmt.Errorf("read settings: %w", err)
	}
	loaded.ListenAddr, err = validateListenAddr(loaded.ListenAddr)
	return loaded, err
}

func saveSettings(path string, settings settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	return os.WriteFile(path, data, 0o600)
}

func (s *AppService) updateSettings(update func(*settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(&s.settings)
	return saveSettings(s.settingsPath, s.settings)
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
