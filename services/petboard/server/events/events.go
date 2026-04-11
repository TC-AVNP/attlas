// Package events is a tiny in-process pub/sub broker that the API
// handlers publish to and the SSE endpoint subscribes from. There is no
// persistence — events are best-effort: a slow client just gets dropped.
//
// Wire shape (each Server-Sent Event):
//
//   event: feature.created
//   data: {"project_slug":"petboard","feature_id":12}
//
// Frontend listens via EventSource('/petboard/api/events') and
// invalidates the matching react-query cache key on each event.
package events

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event is one broadcast message.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Broker is a tiny pub/sub broker. Goroutine-safe.
type Broker struct {
	mu      sync.Mutex
	clients map[chan Event]bool
}

func New() *Broker {
	return &Broker{clients: make(map[chan Event]bool)}
}

// Publish sends e to every currently-connected subscriber. Slow
// subscribers (whose buffered channel is full) get dropped on the
// floor — we never block the API write path on a stuck SSE client.
func (b *Broker) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- e:
		default:
			// drop
		}
	}
}

// Subscribe returns a buffered channel that receives every Event
// published after this call returns. The cleanup function MUST be
// called when the subscriber is done so the broker forgets the channel.
func (b *Broker) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		close(ch)
	}
}

// Handler returns an http.Handler that streams events as SSE. Caller
// is responsible for routing it under whatever path it likes.
func (b *Broker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ch, cleanup := b.Subscribe()
		defer cleanup()

		// Initial comment so the connection is "established" from the
		// client's perspective even before the first real event.
		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		// Heartbeat every 25s so reverse proxies don't time out the
		// idle connection.
		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				fmt.Fprintf(w, ": heartbeat\n\n")
				flusher.Flush()
			case e, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(e.Payload)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
				flusher.Flush()
			}
		}
	})
}
