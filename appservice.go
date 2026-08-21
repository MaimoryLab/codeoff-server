package main

import (
	"context"
	"sync"

	"github.com/MaimoryLab/codex-server/internal/diagnostics"
)

type AppService struct {
	mu     sync.RWMutex
	status diagnostics.Snapshot
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
