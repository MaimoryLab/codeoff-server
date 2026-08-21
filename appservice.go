package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MaimoryLab/codex-server/internal/appserver"
	"github.com/MaimoryLab/codex-server/internal/diagnostics"
	"github.com/MaimoryLab/codex-server/internal/installer"
)

type AppService struct {
	mu        sync.RWMutex
	installMu sync.Mutex
	status    diagnostics.Snapshot
	progress  string
	appServer *appserver.Manager
}

type Overview struct {
	Environment diagnostics.Snapshot `json:"environment"`
	AppServer   appserver.State      `json:"appServer"`
}

func NewAppService() *AppService {
	service := &AppService{appServer: appserver.NewManager()}
	service.RefreshStatus()
	return service
}

func (s *AppService) Overview() Overview {
	return Overview{Environment: s.Status(), AppServer: s.appServer.State()}
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
	return s.appServer.Stop()
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
