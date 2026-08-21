package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

const maxMessageSize = 16 << 20

type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type InitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type Event struct {
	ID     *int64
	Method string
	Params json.RawMessage
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("app-server error %d: %s", e.Code, e.Message)
}

type message struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    error
}

type Client struct {
	transport io.ReadWriteCloser
	encoder   *json.Encoder
	writeMu   sync.Mutex
	nextID    atomic.Int64
	pendingMu sync.Mutex
	pending   map[int64]chan response
	events    chan Event
	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.RWMutex
	err       error
	stop      func()
}

func Start(ctx context.Context, executable string, args ...string) (*Client, error) {
	processCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(processCtx, executable, args...)
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := command.Start(); err != nil {
		cancel()
		return nil, err
	}

	client := New(&stdioTransport{Reader: stdout, Writer: stdin})
	client.stop = cancel
	go func() {
		client.shutdown(command.Wait())
	}()
	return client, nil
}

func New(transport io.ReadWriteCloser) *Client {
	client := &Client{
		transport: transport,
		encoder:   json.NewEncoder(transport),
		pending:   make(map[int64]chan response),
		events:    make(chan Event, 64),
		done:      make(chan struct{}),
	}
	go client.readLoop()
	return client
}

func (c *Client) Initialize(ctx context.Context, info ClientInfo) (InitializeResponse, error) {
	var result InitializeResponse
	err := c.Call(ctx, "initialize", map[string]any{"clientInfo": info}, &result)
	if err != nil {
		return result, err
	}
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	responseChannel := make(chan response, 1)
	c.pendingMu.Lock()
	c.pending[id] = responseChannel
	c.pendingMu.Unlock()

	if err := c.send(struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{id, method, params}); err != nil {
		c.removePending(id)
		return err
	}

	select {
	case response := <-responseChannel:
		if response.err != nil {
			return response.err
		}
		if result == nil || len(response.result) == 0 {
			return nil
		}
		return json.Unmarshal(response.result, result)
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
}

func (c *Client) Notify(method string, params any) error {
	return c.send(struct {
		Method string `json:"method"`
		Params any    `json:"params"`
	}{method, params})
}

func (c *Client) Respond(id int64, result any, rpcError *RPCError) error {
	return c.send(struct {
		ID     int64     `json:"id"`
		Result any       `json:"result,omitempty"`
		Error  *RPCError `json:"error,omitempty"`
	}{id, result, rpcError})
}

func (c *Client) Events() <-chan Event { return c.events }

func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Err() error {
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.err
}

func (c *Client) Close() error {
	if c.stop != nil {
		c.stop()
	}
	return c.transport.Close()
}

func (c *Client) send(value any) error {
	select {
	case <-c.done:
		return c.Err()
	default:
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(value)
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.transport)
	scanner.Buffer(make([]byte, 64<<10), maxMessageSize)
	for scanner.Scan() {
		var incoming message
		if err := json.Unmarshal(scanner.Bytes(), &incoming); err != nil {
			c.shutdown(fmt.Errorf("decode app-server message: %w", err))
			return
		}
		if incoming.Method != "" {
			select {
			case c.events <- Event{ID: incoming.ID, Method: incoming.Method, Params: incoming.Params}:
			case <-c.done:
				return
			}
			continue
		}
		if incoming.ID != nil {
			var responseErr error
			if incoming.Error != nil {
				responseErr = incoming.Error
			}
			c.deliver(*incoming.ID, response{result: incoming.Result, err: responseErr})
		}
	}
	c.shutdown(scanner.Err())
}

func (c *Client) deliver(id int64, value response) {
	c.pendingMu.Lock()
	channel := c.pending[id]
	delete(c.pending, id)
	c.pendingMu.Unlock()
	if channel != nil {
		channel <- value
	}
}

func (c *Client) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) shutdown(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}
		c.errMu.Lock()
		c.err = err
		c.errMu.Unlock()
		close(c.done)

		c.pendingMu.Lock()
		for id, channel := range c.pending {
			channel <- response{err: err}
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()
		close(c.events)
	})
}

type stdioTransport struct {
	io.Reader
	io.Writer
}

func (t *stdioTransport) Close() error {
	var errs []error
	if closer, ok := t.Reader.(io.Closer); ok {
		errs = append(errs, closer.Close())
	}
	if closer, ok := t.Writer.(io.Closer); ok {
		errs = append(errs, closer.Close())
	}
	return errors.Join(errs...)
}
