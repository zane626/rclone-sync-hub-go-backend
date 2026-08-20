package events

import (
	"errors"
	"sync"
	"time"
)

var ErrSubscriberLimit = errors.New("event subscriber limit reached")

type Event struct {
	Type       string    `json:"type"`
	TaskID     uint      `json:"task_id,omitempty"`
	Status     string    `json:"status,omitempty"`
	Percent    float64   `json:"percent,omitempty"`
	BytesDone  int64     `json:"bytes_done,omitempty"`
	BytesTotal int64     `json:"bytes_total,omitempty"`
	Speed      int64     `json:"speed,omitempty"`
	Message    string    `json:"message,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

type Hub struct {
	mu             sync.RWMutex
	subscribers    map[uint64]chan Event
	nextID         uint64
	maxSubscribers int
}

func NewHub(maxSubscribers int) *Hub {
	if maxSubscribers <= 0 {
		maxSubscribers = 100
	}
	return &Hub{subscribers: make(map[uint64]chan Event), maxSubscribers: maxSubscribers}
}

func (h *Hub) Subscribe() (<-chan Event, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subscribers) >= h.maxSubscribers {
		return nil, nil, ErrSubscriberLimit
	}
	h.nextID++
	id := h.nextID
	channel := make(chan Event, 64)
	h.subscribers[id] = channel
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers, id)
			close(channel)
			h.mu.Unlock()
		})
	}
	return channel, cancel, nil
}

func (h *Hub) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
			// Progress is a snapshot. Drop an old snapshot for a slow client and keep the producer non-blocking.
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- event:
			default:
			}
		}
	}
}
