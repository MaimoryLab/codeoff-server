package control

import (
	"sync"

	"github.com/MaimoryLab/codex-server/internal/appserver"
)

type eventHub struct {
	mu          sync.Mutex
	subscribers map[chan appserver.Event]struct{}
}

func newEventHub(source <-chan appserver.Event) *eventHub {
	hub := &eventHub{subscribers: make(map[chan appserver.Event]struct{})}
	go hub.forward(source)
	return hub
}

func (h *eventHub) subscribe() (<-chan appserver.Event, func()) {
	channel := make(chan appserver.Event, 32)
	h.mu.Lock()
	h.subscribers[channel] = struct{}{}
	h.mu.Unlock()
	return channel, func() {
		h.mu.Lock()
		if _, ok := h.subscribers[channel]; ok {
			delete(h.subscribers, channel)
			close(channel)
		}
		h.mu.Unlock()
	}
}

func (h *eventHub) forward(source <-chan appserver.Event) {
	for event := range source {
		h.mu.Lock()
		for subscriber := range h.subscribers {
			select {
			case subscriber <- event:
			default:
				// ponytail: drop slow subscribers; reconnect for a fresh live stream.
			}
		}
		h.mu.Unlock()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for subscriber := range h.subscribers {
		close(subscriber)
		delete(h.subscribers, subscriber)
	}
}
