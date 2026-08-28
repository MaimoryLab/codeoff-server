package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/codeoff-server/internal/appserver"
	"github.com/MaimoryLab/codeoff-server/internal/awake"
	"github.com/MaimoryLab/codeoff-server/internal/control"
	"github.com/MaimoryLab/codeoff-server/internal/daemon"
	"github.com/MaimoryLab/codeoff-server/internal/devices"
	"github.com/MaimoryLab/codeoff-server/internal/diagnostics"
	"github.com/MaimoryLab/codeoff-server/internal/installer"
	"github.com/MaimoryLab/codeoff-server/internal/tunnel"
)

const defaultListenAddr = "127.0.0.1:11037"

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type AppService struct {
	mu           sync.RWMutex
	installMu    sync.Mutex
	status       diagnostics.Snapshot
	progress     string
	appServer    *appserver.Manager
	keepAwake    *awake.Inhibitor
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
	ListenAddr       string   `json:"listenAddr"`
	AppServerEnabled bool     `json:"appServerEnabled"`
	TunnelEnabled    bool     `json:"tunnelEnabled"`
	TunnelURL        string   `json:"tunnelUrl,omitempty"`
	PreventSleep     bool     `json:"preventSleep"`
	CodexEnvironment []string `json:"codexEnvironment,omitempty"`
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
	if err := daemon.MigrateLegacyConfig(); err != nil {
		return nil, fmt.Errorf("migrate legacy config: %w", err)
	}
	devicePath, err := devices.DefaultPath()
	if err != nil {
		return nil, err
	}
	deviceStore, err := devices.Open(devicePath)
	if err != nil {
		return nil, err
	}
	settingsPath, err := daemon.DefaultSettingsPath()
	if err != nil {
		return nil, err
	}
	loadedSettings, err := loadSettings(settingsPath)
	if err != nil {
		return nil, err
	}
	manager := appserver.NewManager()
	keepAwake := awake.New()
	service := &AppService{appServer: manager, keepAwake: keepAwake, devices: deviceStore, tunnel: tunnel.NewManager(), listenAddr: loadedSettings.ListenAddr, settingsPath: settingsPath, settings: loadedSettings}
	manager.SetOnStopped(func() {
		if err := service.syncPreventSleep(); err != nil {
			log.Printf("sync sleep inhibitor: %v", err)
		}
	})
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
		Tunnel:       s.TunnelState(),
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
	s.mu.RLock()
	environment := slices.Clone(s.settings.CodexEnvironment)
	s.mu.RUnlock()
	state, err := s.appServer.Start(ctx, status.Codex.Path, environment)
	return state, errors.Join(err, s.syncPreventSleep())
}

func (s *AppService) CodexEnvironment() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.settings.CodexEnvironment)
}

func (s *AppService) SetCodexEnvironment(environment []string) (appserver.State, error) {
	if err := validateEnvironment(environment); err != nil {
		return s.appServer.State(), err
	}
	if err := s.updateSettings(func(settings *settings) { settings.CodexEnvironment = slices.Clone(environment) }); err != nil {
		return s.appServer.State(), err
	}
	if !s.appServer.State().Running {
		return s.appServer.State(), nil
	}
	if err := s.appServer.Stop(); err != nil {
		return s.appServer.State(), err
	}
	return s.startAppServer()
}

func (s *AppService) StopAppServer() (appserver.State, error) {
	stopErr := s.appServer.Stop()
	awakeErr := s.keepAwake.Set(false)
	settingsErr := s.updateSettings(func(settings *settings) { settings.AppServerEnabled = false })
	return s.appServer.State(), errors.Join(stopErr, awakeErr, settingsErr)
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
	return errors.Join(s.tunnel.Stop(), s.appServer.Stop(), s.keepAwake.Set(false), controlErr)
}

func (s *AppService) TunnelState() tunnel.State {
	state := s.tunnel.State()
	s.mu.RLock()
	configuredURL := s.settings.TunnelURL
	s.mu.RUnlock()
	if configuredURL != "" && !state.Running && !state.Starting && !state.Stopping {
		state.External = true
		state.URL = configuredURL
	}
	return state
}

func (s *AppService) SetTunnelURL(raw string) (tunnel.State, error) {
	configuredURL, err := validateTunnelURL(raw)
	if err != nil {
		return s.TunnelState(), err
	}
	stopErr := s.tunnel.Stop()
	settingsErr := s.updateSettings(func(settings *settings) {
		settings.TunnelURL = configuredURL
		settings.TunnelEnabled = false
	})
	return s.TunnelState(), errors.Join(stopErr, settingsErr)
}

func (s *AppService) StartTunnel() (tunnel.State, error) {
	settingsErr := s.updateSettings(func(settings *settings) { settings.TunnelEnabled = true })
	state, err := s.startTunnel()
	return state, errors.Join(settingsErr, err)
}

func (s *AppService) startTunnel() (tunnel.State, error) {
	s.mu.RLock()
	configuredURL := s.settings.TunnelURL
	s.mu.RUnlock()
	if configuredURL != "" {
		return s.TunnelState(), nil
	}
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

func (s *AppService) preventSleepEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings.PreventSleep
}

func (s *AppService) setPreventSleep(enabled bool) error {
	settingsErr := s.updateSettings(func(settings *settings) { settings.PreventSleep = enabled })
	return errors.Join(settingsErr, s.syncPreventSleep())
}

func (s *AppService) syncPreventSleep() error {
	enabled := s.preventSleepEnabled() && s.appServer.State().Running
	if err := s.keepAwake.Set(enabled); err != nil || !enabled {
		return err
	}
	if !s.appServer.State().Running {
		return s.keepAwake.Set(false)
	}
	return nil
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
	if err := validateEnvironment(loaded.CodexEnvironment); err != nil {
		return loaded, err
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

func validateTunnelURL(raw string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if value == "" {
		return "", nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("tunnel URL must be an http(s) origin without a path")
	}
	return value, nil
}

func validateEnvironment(environment []string) error {
	seen := make(map[string]struct{}, len(environment))
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !environmentName.MatchString(name) || strings.ContainsRune(entry, '\x00') {
			return fmt.Errorf("invalid environment variable %q; expected NAME=value", entry)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate environment variable %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}
