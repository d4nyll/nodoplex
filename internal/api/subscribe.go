package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/d4nyll/nodoplex/internal/graph"
)

// keepaliveInterval is the time with no line written after which the server
// writes a keepalive comment (F5).
const keepaliveInterval = 30 * time.Second

func subscribe(h *Handler, rq *request) (int, any, error) {
	var start *int64
	var err error
	if values, ok := rq.r.Header["Last-Event-Id"]; ok {
		// I6: Last-Event-ID gives startChangeId.
		i, ok := textInt(values[0])
		if !ok || i < 0 {
			return 0, nil, invalid("Last-Event-ID must be an integer of 0 or more")
		}
		start = &i
	} else if start, err = rq.queryInt("startChangeId", 0, maxInt); err != nil {
		return 0, nil, err
	}
	groups, err := rq.filterGroups()
	if err != nil {
		return 0, nil, err
	}
	position, err := h.store.CheckSubscribe(rq.r.Context(), rq.slug, start)
	if err != nil {
		return 0, nil, err
	}
	w := rq.w
	w.Header().Set("Content-Type", "text/event-stream")
	if rq.r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return 0, nil, nil
	}
	sub := h.store.Subscribe(rq.slug, position, groups)
	defer sub.Close()

	// The subscription ends when the client closes the connection (F6), or
	// when the server shuts down.
	ctx, cancel := context.WithCancel(rq.r.Context())
	defer cancel()
	stop := context.AfterFunc(h.ctx, cancel)
	defer stop()

	rc := http.NewResponseController(w)
	keepalive := time.NewTimer(keepaliveInterval)
	defer keepalive.Stop()
	// write writes lines to the event stream. It returns false if the server
	// can't write to the connection (F4).
	write := func(lines string) bool {
		if _, err := io.WriteString(w, lines); err != nil {
			return false
		}
		keepalive.Reset(keepaliveInterval)
		return rc.Flush() == nil
	}
	w.WriteHeader(http.StatusOK)
	if rc.Flush() != nil {
		return 0, nil, nil
	}
	for {
		entries, more, err := sub.Next(ctx)
		if err != nil {
			if ctx.Err() == nil {
				// F3, F4: end the subscription with the code of the refusal.
				data, _ := graph.Marshal(asRefusal(err))
				write("event: end\ndata: " + string(data) + "\n\n")
			}
			return 0, nil, nil
		}
		for _, e := range entries {
			if !write("id: " + strconv.FormatInt(e.ChangeID, 10) + "\ndata: " + string(e.JSON) + "\n\n") {
				return 0, nil, nil
			}
		}
		if more {
			continue
		}
	wait:
		for {
			select {
			case <-ctx.Done():
				return 0, nil, nil
			case <-sub.Signals:
				break wait
			case <-keepalive.C:
				if !write(": keepalive\n\n") {
					return 0, nil, nil
				}
			}
		}
	}
}
