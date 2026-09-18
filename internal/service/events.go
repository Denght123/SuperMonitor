package service

import (
	"encoding/json"
	"sync"
	"time"
)

type Event struct {
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type EventHub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func NewEventHub() *EventHub {
	return &EventHub{clients: make(map[chan []byte]struct{})}
}

func (h *EventHub) Subscribe() (<-chan []byte, func()) {
	channel := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[channel] = struct{}{}
	h.mu.Unlock()
	return channel, func() {
		h.mu.Lock()
		if _, ok := h.clients[channel]; ok {
			delete(h.clients, channel)
			close(channel)
		}
		h.mu.Unlock()
	}
}

func (h *EventHub) Publish(event Event) {
	payload, _ := json.Marshal(event)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client <- payload:
		default:
		}
	}
}
