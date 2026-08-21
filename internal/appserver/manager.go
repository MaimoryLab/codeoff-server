package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

type State struct {
	Running   bool      `json:"running"`
	Starting  bool      `json:"starting"`
	StartedAt time.Time `json:"startedAt"`
	CodexHome string    `json:"codexHome,omitempty"`
	UserAgent string    `json:"userAgent,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type startClient func(context.Context, string, ...string) (*Client, error)

type Manager struct {
	operationMu sync.Mutex
	mu          sync.RWMutex
	client      *Client
	cancel      context.CancelFunc
	state       State
	events      chan Event
	start       startClient
}

func NewManager() *Manager {
	return newManager(Start)
}

func newManager(start startClient) *Manager {
	return &Manager{events: make(chan Event, 256), start: start}
}

func (m *Manager) Start(ctx context.Context, executable string) (State, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	if state := m.State(); state.Running || state.Starting {
		return state, nil
	}
	m.setState(State{Starting: true})

	processCtx, cancel := context.WithCancel(context.Background())
	client, err := m.start(processCtx, executable, "app-server", "--stdio")
	if err != nil {
		cancel()
		return m.fail(err), err
	}
	initializeCtx, initializeCancel := context.WithTimeout(ctx, 10*time.Second)
	defer initializeCancel()
	initialized, err := client.Initialize(initializeCtx, ClientInfo{
		Name:    "codex_remote",
		Title:   "Codex Remote",
		Version: "0.1.0",
	})
	if err != nil {
		cancel()
		_ = client.Close()
		return m.fail(err), err
	}

	state := State{
		Running:   true,
		StartedAt: time.Now().UTC(),
		CodexHome: initialized.CodexHome,
		UserAgent: initialized.UserAgent,
	}
	m.mu.Lock()
	m.client = client
	m.cancel = cancel
	m.state = state
	m.mu.Unlock()
	go m.watch(client)
	return state, nil
}

func (m *Manager) Stop() error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.mu.Lock()
	client, cancel := m.client, m.cancel
	m.client, m.cancel = nil, nil
	m.state = State{}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if client == nil {
		return nil
	}
	return client.Close()
}

func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Manager) Events() <-chan Event { return m.events }

func (m *Manager) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return nil, errors.New("app-server is not running")
	}
	var result json.RawMessage
	if err := client.Call(ctx, method, params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) Respond(id int64, result any, rpcError *RPCError) error {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return errors.New("app-server is not running")
	}
	return client.Respond(id, result, rpcError)
}

func (m *Manager) watch(client *Client) {
	for event := range client.Events() {
		select {
		case m.events <- event:
		default:
			// ponytail: bounded live events; add persistence when turn streaming is wired.
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != client {
		return
	}
	m.client, m.cancel = nil, nil
	m.state.Running = false
	if err := client.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		m.state.Error = err.Error()
	}
}

func (m *Manager) fail(err error) State {
	state := State{Error: err.Error()}
	m.setState(state)
	return state
}

func (m *Manager) setState(state State) {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
}
