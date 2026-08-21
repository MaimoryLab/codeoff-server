package main

import (
	"context"
	"sync"
	"time"

	"github.com/MaimoryLab/codex-server/internal/diagnostics"
	"github.com/MaimoryLab/codex-server/internal/installer"
)

type AppService struct {
	mu       sync.RWMutex
	status   diagnostics.Snapshot
	progress string
}

func NewAppService() *AppService {
	service := &AppService{}
	service.RefreshStatus()
	return service
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := installer.New(s.setProgress).InstallNode(ctx); err != nil {
		return s.Status(), err
	}
	return s.RefreshStatus(), nil
}

func (s *AppService) InstallCodex() (diagnostics.Snapshot, error) {
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
