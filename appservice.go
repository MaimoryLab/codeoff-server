package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MaimoryLab/codex-server/internal/appserver"
	"github.com/MaimoryLab/codex-server/internal/devices"
	"github.com/MaimoryLab/codex-server/internal/diagnostics"
	"github.com/MaimoryLab/codex-server/internal/installer"
	"github.com/MaimoryLab/codex-server/internal/tunnel"
)

type AppService struct {
	mu         sync.RWMutex
	installMu  sync.Mutex
	status     diagnostics.Snapshot
	progress   string
	appServer  *appserver.Manager
	devices    *devices.Store
	tunnel     *tunnel.Manager
	controlURL string
}

type Overview struct {
	Environment diagnostics.Snapshot `json:"environment"`
	AppServer   appserver.State      `json:"appServer"`
	Tunnel      tunnel.State         `json:"tunnel"`
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
	service := &AppService{appServer: appserver.NewManager(), devices: deviceStore, tunnel: tunnel.NewManager()}
	service.RefreshStatus()
	return service, nil
}

func (s *AppService) NewPairing() (devices.Pairing, error) { return s.devices.NewPairing() }

func (s *AppService) Devices() []devices.Device { return s.devices.List() }

func (s *AppService) RevokeDevice(id string) error { return s.devices.Revoke(id) }

func (s *AppService) Overview() Overview {
	return Overview{Environment: s.Status(), AppServer: s.appServer.State(), Tunnel: s.tunnel.State()}
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
	if err := s.tunnel.Stop(); err != nil {
		return err
	}
	return s.appServer.Stop()
}

func (s *AppService) SetControlURL(url string) { s.controlURL = url }

func (s *AppService) TunnelState() tunnel.State { return s.tunnel.State() }

func (s *AppService) StartTunnel() (tunnel.State, error) {
	status := s.RefreshStatus()
	if !status.Cloudflared.Installed {
		return s.tunnel.State(), errors.New("cloudflared is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	return s.tunnel.Start(ctx, status.Cloudflared.Path, s.controlURL)
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
