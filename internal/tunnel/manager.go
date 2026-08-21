package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

var quickTunnelURL = regexp.MustCompile(`https://[A-Za-z0-9.-]+\.trycloudflare\.com`)

type State struct {
	Running   bool      `json:"running"`
	Starting  bool      `json:"starting"`
	URL       string    `json:"url,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	Error     string    `json:"error,omitempty"`
}

type Manager struct {
	operationMu sync.Mutex
	mu          sync.RWMutex
	command     *exec.Cmd
	cancel      context.CancelFunc
	state       State
}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) Start(ctx context.Context, executable, origin string) (State, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	if state := m.State(); state.Running || state.Starting {
		return state, nil
	}
	if origin == "" {
		return m.fail(errors.New("control API origin is not configured")), errors.New("control API origin is not configured")
	}
	m.setState(State{Starting: true})

	processCtx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(processCtx, executable, "tunnel", "--no-autoupdate", "--url", origin, "--output", "json")
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return m.fail(err), err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		cancel()
		return m.fail(err), err
	}
	if err := command.Start(); err != nil {
		cancel()
		return m.fail(err), err
	}

	urlFound := make(chan string, 1)
	for _, stream := range []io.Reader{stdout, stderr} {
		go findURL(stream, urlFound)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	startupCtx, startupCancel := context.WithTimeout(ctx, 20*time.Second)
	defer startupCancel()
	select {
	case url := <-urlFound:
		state := State{Running: true, URL: url, StartedAt: time.Now().UTC()}
		m.mu.Lock()
		m.command, m.cancel, m.state = command, cancel, state
		m.mu.Unlock()
		go m.monitor(command, wait)
		return state, nil
	case err := <-wait:
		cancel()
		if err == nil {
			err = errors.New("cloudflared exited before publishing a tunnel URL")
		}
		return m.fail(err), err
	case <-startupCtx.Done():
		cancel()
		return m.fail(fmt.Errorf("start tunnel: %w", startupCtx.Err())), startupCtx.Err()
	}
}

func (m *Manager) Stop() error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.mu.Lock()
	cancel, command := m.cancel, m.command
	m.cancel, m.command, m.state = nil, nil, State{}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if command != nil {
		return command.Process.Kill()
	}
	return nil
}

func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Manager) monitor(command *exec.Cmd, wait <-chan error) {
	err := <-wait
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.command != command {
		return
	}
	m.command, m.cancel = nil, nil
	m.state.Running = false
	if err != nil {
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

func findURL(reader io.Reader, result chan<- string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if url := quickTunnelURL.FindString(scanner.Text()); url != "" {
			select {
			case result <- url:
			default:
			}
			return
		}
	}
}
