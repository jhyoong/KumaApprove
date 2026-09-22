package relay

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type sessionEntry struct {
	code    string
	created time.Time
}

type Handler struct {
	mu       sync.Mutex
	sessions map[string]sessionEntry
}

func NewHandler() *Handler {
	h := &Handler{
		sessions: make(map[string]sessionEntry),
	}
	go h.sweepLoop()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/callback":
		h.handleCallback(w, r)
	case "/poll":
		h.handlePoll(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "missing state parameter", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")

	h.mu.Lock()
	h.sessions[state] = sessionEntry{code: code, created: time.Now()}
	h.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<html><body><p>Authorization successful. You may close this tab.</p></body></html>"))
}

func (h *Handler) handlePoll(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		http.Error(w, "missing session parameter", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	entry, ok := h.sessions[session]
	if ok {
		delete(h.sessions, session)
	}
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if ok && entry.code != "" {
		json.NewEncoder(w).Encode(map[string]string{
			"status": "complete",
			"code":   entry.code,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": "pending",
	})
}

func (h *Handler) sweepLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		h.sweep(10 * time.Minute)
	}
}

func (h *Handler) sweep(maxAge time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, entry := range h.sessions {
		if entry.created.Before(cutoff) {
			delete(h.sessions, id)
		}
	}
}
