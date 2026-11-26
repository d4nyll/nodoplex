package changefeed

import "sync"

// Hub signals the open subscriptions of a Product (B9). A signal holds no
// Change Entry. Each subscription has one pending signal at most, so the Hub
// keeps no buffer of Change Entries.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]bool
}

func NewHub() *Hub { return &Hub{subs: map[string]map[chan struct{}]bool{}} }

// Subscribe returns a channel that receives a signal after each Signal for the
// slug, and a function that ends the subscription. If a signal comes while
// nobody receives from the channel, it stays pending until the next receive.
func (h *Hub) Subscribe(slug string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[slug] == nil {
		h.subs[slug] = map[chan struct{}]bool{}
	}
	h.subs[slug][ch] = true
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[slug], ch)
		if len(h.subs[slug]) == 0 {
			delete(h.subs, slug)
		}
		h.mu.Unlock()
	}
}

// Signal wakes each subscription of the slug.
func (h *Hub) Signal(slug string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[slug] {
		select {
		case ch <- struct{}{}:
		default: // a signal is already pending
		}
	}
}
