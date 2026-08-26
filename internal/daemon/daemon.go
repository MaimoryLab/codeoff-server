package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/codeoff-server/internal/appserver"
	"github.com/MaimoryLab/codeoff-server/internal/control"
	"github.com/MaimoryLab/codeoff-server/internal/devices"
	"github.com/MaimoryLab/codeoff-server/internal/diagnostics"
	"github.com/MaimoryLab/codeoff-server/internal/tunnel"
)

const (
	DefaultListenAddr = "127.0.0.1:11037"
	DefaultTunnelMode = "quick"
)

type Config struct {
	ListenAddr      string
	CFTunnel        bool
	CFTunnelMode    string
	CFTunnelURL     string
	StatePath       string
	CodexPath       string
	CloudflaredPath string
}

type StateFile struct {
	PID         int       `json:"pid"`
	ControlAddr string    `json:"controlAddr"`
	TunnelURL   string    `json:"tunnelUrl,omitempty"`
	AdminToken  string    `json:"adminToken"`
	StartedAt   time.Time `json:"startedAt"`
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

type Service struct {
	mu        sync.RWMutex
	config    Config
	status    diagnostics.Snapshot
	devices   *devices.Store
	app       *appserver.Manager
	tunnel    *tunnel.Manager
	control   *control.Server
	adminTok  string
	state     StateFile
	runCancel context.CancelFunc
	closeMu   sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

func DefaultStatePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "codex-remote", "daemon.json"), nil
}

func New(config Config) (*Service, error) {
	if config.ListenAddr == "" {
		config.ListenAddr = DefaultListenAddr
	}
	if config.CFTunnelMode == "" {
		config.CFTunnelMode = DefaultTunnelMode
	}
	if config.CFTunnelMode != "quick" && config.CFTunnelMode != "external" {
		return nil, errors.New("cf tunnel mode must be quick or external")
	}
	if config.CFTunnelMode == "external" && config.CFTunnelURL == "" {
		return nil, errors.New("cf tunnel URL is required in external mode")
	}
	if config.CFTunnelURL != "" {
		var err error
		config.CFTunnelURL, err = validateTunnelURL(config.CFTunnelURL)
		if err != nil {
			return nil, err
		}
	}
	if config.StatePath == "" {
		var err error
		config.StatePath, err = DefaultStatePath()
		if err != nil {
			return nil, err
		}
	}
	address, err := validateListenAddr(config.ListenAddr)
	if err != nil {
		return nil, err
	}
	config.ListenAddr = address
	devicePath, err := devices.DefaultPath()
	if err != nil {
		return nil, err
	}
	store, err := devices.Open(devicePath)
	if err != nil {
		return nil, err
	}
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	return &Service{
		config:   config,
		devices:  store,
		app:      appserver.NewManager(),
		tunnel:   tunnel.NewManager(),
		adminTok: token,
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	s.status = diagnostics.Check(ctx)
	s.mu.RLock()
	config := s.config
	s.mu.RUnlock()
	if config.CodexPath == "" {
		config.CodexPath = s.status.Codex.Path
	}
	if config.CodexPath == "" {
		return errors.New("codex CLI is not installed")
	}
	server := control.NewWithOptions(func(context.Context) Overview { return s.Overview() }, s.devices, control.Options{
		AppServers: []control.AppServer{s.app},
		Admin:      s,
		AdminToken: s.adminTok,
	})
	if err := server.Start(config.ListenAddr); err != nil {
		return err
	}
	s.mu.Lock()
	s.control = server
	s.state = StateFile{PID: os.Getpid(), ControlAddr: server.Addr(), AdminToken: s.adminTok, StartedAt: time.Now().UTC()}
	s.mu.Unlock()
	if err := s.saveState(); err != nil {
		_ = server.Close(context.Background())
		return err
	}
	if _, err := s.app.Start(ctx, config.CodexPath); err != nil {
		_ = s.Shutdown()
		return fmt.Errorf("start app-server: %w", err)
	}
	if config.CFTunnel {
		if err := s.startTunnel(ctx); err != nil {
			_ = s.Shutdown()
			return err
		}
	}
	return nil
}

func (s *Service) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.runCancel = cancel
	s.mu.Unlock()
	if err := s.Start(runCtx); err != nil {
		cancel()
		return err
	}
	<-runCtx.Done()
	return s.Shutdown()
}

func (s *Service) Shutdown() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	s.mu.RLock()
	cancel := s.runCancel
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	s.closeOnce.Do(func() {
		s.mu.RLock()
		server := s.control
		statePath := s.config.StatePath
		s.mu.RUnlock()
		var serverErr error
		if server != nil {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
			serverErr = server.Close(closeCtx)
			closeCancel()
		}
		removeErr := os.Remove(statePath)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		s.closeErr = errors.Join(serverErr, s.tunnel.Stop(), s.app.Stop(), removeErr)
	})
	return s.closeErr
}

func (s *Service) Status(context.Context) any { return s.Overview() }

func (s *Service) Overview() Overview {
	s.mu.RLock()
	status, config, server := s.status, s.config, s.control
	s.mu.RUnlock()
	controlAddr := ""
	if server != nil {
		controlAddr = server.Addr()
	}
	return Overview{
		Environment:  status,
		AppServer:    s.app.State(),
		Tunnel:       s.tunnelState(),
		ControlAddr:  controlAddr,
		ControlAddrs: controlAddresses(config.ListenAddr),
		ListenAddr:   config.ListenAddr,
		ServerUUID:   s.devices.Server().ID,
	}
}

func (s *Service) NewPairing() (devices.Pairing, error) { return s.devices.NewPairing() }
func (s *Service) Devices() []devices.Device            { return s.devices.List() }
func (s *Service) RevokeDevice(id string) error         { return s.devices.Revoke(id) }

func (s *Service) RestartAppServer() error {
	if err := s.app.Stop(); err != nil {
		return err
	}
	s.mu.RLock()
	executable := s.config.CodexPath
	status := s.status
	s.mu.RUnlock()
	if executable == "" {
		executable = status.Codex.Path
	}
	if executable == "" {
		return errors.New("codex CLI is not installed")
	}
	_, err := s.app.Start(context.Background(), executable)
	return err
}

func (s *Service) RestartTunnel() error {
	if err := s.tunnel.Stop(); err != nil {
		return err
	}
	s.mu.RLock()
	config := s.config
	s.mu.RUnlock()
	if !config.CFTunnel || config.CFTunnelMode == "external" {
		return nil
	}
	return s.startTunnel(context.Background())
}

func (s *Service) startTunnel(ctx context.Context) error {
	s.mu.RLock()
	config := s.config
	status := s.status
	server := s.control
	s.mu.RUnlock()
	if config.CFTunnelMode == "external" {
		return nil
	}
	if server == nil {
		return errors.New("control API is not running")
	}
	executable := config.CloudflaredPath
	if executable == "" {
		executable = status.Cloudflared.Path
	}
	if executable == "" {
		return errors.New("cloudflared is not installed")
	}
	startCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	_, err := s.tunnel.Start(startCtx, executable, server.Addr())
	return err
}

func (s *Service) tunnelState() tunnel.State {
	state := s.tunnel.State()
	s.mu.RLock()
	config := s.config
	s.mu.RUnlock()
	if config.CFTunnel && config.CFTunnelMode == "external" {
		state = tunnel.State{Running: true, External: true, URL: config.CFTunnelURL}
	}
	return state
}

func (s *Service) saveState() error {
	s.mu.RLock()
	state, path := s.state, s.config.StatePath
	s.mu.RUnlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func LoadState(path string) (StateFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return StateFile{}, err
	}
	var state StateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return StateFile{}, fmt.Errorf("read daemon state: %w", err)
	}
	if state.ControlAddr == "" || state.AdminToken == "" {
		return StateFile{}, errors.New("daemon state is incomplete")
	}
	return state, nil
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func validateListenAddr(address string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || net.ParseIP(host) == nil || port == "" {
		return "", errors.New("listen address must be an IP address and port, for example 127.0.0.1:11037")
	}
	return net.JoinHostPort(host, port), nil
}

func validateTunnelURL(raw string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("cf tunnel URL must be an http(s) origin without a path")
	}
	return value, nil
}

func controlAddresses(address string) []string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	}
	if host != "0.0.0.0" {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	addresses := make([]string, 0)
	if interfaceAddresses, err := net.InterfaceAddrs(); err == nil {
		for _, address := range interfaceAddresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil {
				addresses = append(addresses, "http://"+net.JoinHostPort(ip.String(), port))
			}
		}
	}
	slices.Sort(addresses)
	return slices.Compact(addresses)
}
