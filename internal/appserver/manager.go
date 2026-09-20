package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/codeoff-server/internal/buildinfo"
)

type State struct {
	Running   bool      `json:"running"`
	Starting  bool      `json:"starting"`
	Stopping  bool      `json:"stopping"`
	StartedAt time.Time `json:"startedAt"`
	CodexHome string    `json:"codexHome,omitempty"`
	UserAgent string    `json:"userAgent,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type HeldThread struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type startClient func(context.Context, string, []string, ...string) (*Client, error)

type Manager struct {
	operationMu sync.Mutex
	mu          sync.RWMutex
	client      *Client
	cancel      context.CancelFunc
	state       State
	events      chan Event
	start       startClient
	executable  string
	terminate   func(context.Context, string, string) error
	onStopped   func()
	environment []string
}

func NewManager() *Manager {
	return newManager(Start)
}

func newManager(start startClient) *Manager {
	return &Manager{
		events:    make(chan Event, 256),
		start:     start,
		terminate: terminateThreadOwner,
	}
}

func (m *Manager) Start(ctx context.Context, executable string, environment []string) (State, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	return m.startLocked(ctx, executable, environment)
}

func (m *Manager) Toggle(ctx context.Context, executable string) (State, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	if m.State().Running {
		err := m.stopLocked()
		return m.State(), err
	}
	if executable == "" {
		return m.State(), errors.New("codex CLI is not installed")
	}
	return m.startLocked(ctx, executable, nil)
}

func (m *Manager) startLocked(ctx context.Context, executable string, environment []string) (State, error) {
	if state := m.State(); state.Running || state.Starting || state.Stopping {
		return state, nil
	}
	m.setState(State{Starting: true})

	processCtx, cancel := context.WithCancel(context.Background())
	client, err := m.start(processCtx, executable, environment, "app-server", "--stdio")
	if err != nil {
		cancel()
		return m.fail(err), err
	}
	initializeCtx, initializeCancel := context.WithTimeout(ctx, 10*time.Second)
	defer initializeCancel()
	initialized, err := client.Initialize(initializeCtx, ClientInfo{
		Name:    "codeoff_server",
		Title:   "Codeoff Server",
		Version: buildinfo.Version,
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
	m.executable = executable
	m.environment = slices.Clone(environment)
	m.mu.Unlock()
	go m.watch(client)
	return state, nil
}

func (m *Manager) Stop() error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	return m.stopLocked()
}

func (m *Manager) SetOnStopped(callback func()) {
	m.mu.Lock()
	m.onStopped = callback
	m.mu.Unlock()
}

func (m *Manager) stopLocked() error {
	m.mu.Lock()
	client, cancel := m.client, m.cancel
	m.client, m.cancel = nil, nil
	m.state = State{Stopping: client != nil || cancel != nil}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if client == nil {
		m.setState(State{})
		return nil
	}
	err := client.Close()
	m.setState(State{})
	return err
}

func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Manager) Events() <-chan Event { return m.events }

func (m *Manager) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
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

func (m *Manager) ResumeThread(ctx context.Context, threadID string) (json.RawMessage, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return nil, errors.New("app-server is not running")
	}
	return resumeOwnedThread(ctx, client, threadID)
}

func (m *Manager) ReleaseThread(ctx context.Context, threadID string) (bool, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.mu.RLock()
	client, executable, environment := m.client, m.executable, slices.Clone(m.environment)
	m.mu.RUnlock()
	if client == nil {
		return false, errors.New("app-server is not running")
	}
	if err := client.Call(ctx, "thread/unsubscribe", map[string]string{"threadId": threadID}, nil); err != nil {
		return false, err
	}
	params, _ := json.Marshal(map[string]string{"threadId": threadID})
	select {
	case m.events <- Event{Method: "thread/released", Params: params}:
	default:
	}
	active, err := hasActiveThreads(ctx, client)
	if err != nil || active {
		return false, err
	}
	stopErr := m.stopLocked()
	_, startErr := m.startLocked(ctx, executable, environment)
	return startErr == nil, errors.Join(stopErr, startErr)
}

func (m *Manager) HeldThreads(ctx context.Context) ([]HeldThread, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return nil, errors.New("app-server is not running")
	}
	return heldThreads(ctx, client)
}

func (m *Manager) TakeOverThread(ctx context.Context, threadID string) (json.RawMessage, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.mu.RLock()
	client, codexHome := m.client, m.state.CodexHome
	m.mu.RUnlock()
	if client == nil {
		return nil, errors.New("app-server is not running")
	}

	params := map[string]string{"threadId": threadID}
	result, err := resumeOwnedThread(ctx, client, threadID)
	if err == nil || !isWriterConflict(err) {
		return result, err
	}
	takeoverCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := m.terminate(takeoverCtx, codexHome, threadID); err != nil {
		return nil, err
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err = resumeThread(takeoverCtx, client, params)
		if err == nil || !isWriterConflict(err) {
			return result, err
		}
		select {
		case <-takeoverCtx.Done():
			return nil, fmt.Errorf("waiting to take over thread: %w", takeoverCtx.Err())
		case <-ticker.C:
		}
	}
}

func resumeOwnedThread(ctx context.Context, client *Client, threadID string) (json.RawMessage, error) {
	params := map[string]string{"threadId": threadID}
	result, err := resumeThread(ctx, client, params)
	if err == nil || !isWriterConflict(err) {
		return result, err
	}
	if unsubscribeErr := client.Call(ctx, "thread/unsubscribe", params, nil); unsubscribeErr != nil {
		return result, err
	}
	return resumeThread(ctx, client, params)
}

func resumeThread(ctx context.Context, client *Client, params any) (json.RawMessage, error) {
	var result json.RawMessage
	if err := client.Call(ctx, "thread/resume", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func isWriterConflict(err error) bool {
	rpcError, ok := errors.AsType[*RPCError](err)
	return ok && rpcError.Code == -32600
}

func hasActiveThreads(ctx context.Context, client *Client) (bool, error) {
	var cursor string
	for {
		var loaded struct {
			Data       []string `json:"data"`
			NextCursor string   `json:"nextCursor"`
		}
		params := map[string]string{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := client.Call(ctx, "thread/loaded/list", params, &loaded); err != nil {
			return false, err
		}
		for _, id := range loaded.Data {
			var result struct {
				Thread struct {
					Status struct {
						Type string `json:"type"`
					} `json:"status"`
				} `json:"thread"`
			}
			if err := client.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &result); err != nil {
				return false, err
			}
			if result.Thread.Status.Type == "active" {
				return true, nil
			}
		}
		if loaded.NextCursor == "" || loaded.NextCursor == cursor {
			return false, nil
		}
		cursor = loaded.NextCursor
	}
}

func heldThreads(ctx context.Context, client *Client) ([]HeldThread, error) {
	var cursor string
	var threads []HeldThread
	for {
		var loaded struct {
			Data       []string `json:"data"`
			NextCursor string   `json:"nextCursor"`
		}
		params := map[string]string{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := client.Call(ctx, "thread/loaded/list", params, &loaded); err != nil {
			return nil, err
		}
		for _, id := range loaded.Data {
			var result struct {
				Thread struct {
					ID          string `json:"id"`
					Name        string `json:"name"`
					SessionName string `json:"sessionName"`
					Title       string `json:"title"`
					Preview     string `json:"preview"`
					Status      struct {
						Type string `json:"type"`
					} `json:"status"`
				} `json:"thread"`
			}
			if err := client.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &result); err != nil {
				return nil, err
			}
			name := id
			for _, candidate := range []string{result.Thread.Name, result.Thread.SessionName, result.Thread.Title, result.Thread.Preview} {
				if value := strings.TrimSpace(candidate); value != "" {
					name = value
					break
				}
			}
			threads = append(threads, HeldThread{ID: id, Name: name, Status: result.Thread.Status.Type})
		}
		if loaded.NextCursor == "" || loaded.NextCursor == cursor {
			return threads, nil
		}
		cursor = loaded.NextCursor
	}
}

func (m *Manager) Respond(id json.RawMessage, result any, rpcError *RPCError) error {
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
	if m.client != client {
		m.mu.Unlock()
		return
	}
	m.client, m.cancel = nil, nil
	m.state.Running = false
	if err := client.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		m.state.Error = err.Error()
	}
	callback := m.onStopped
	m.mu.Unlock()
	if callback != nil {
		callback()
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
	callback := m.onStopped
	m.mu.Unlock()
	if !state.Running && !state.Starting && !state.Stopping && callback != nil {
		callback()
	}
}
