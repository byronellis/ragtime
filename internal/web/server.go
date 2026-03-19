package web

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/byronellis/ragtime/internal/session"
)

// EventBroadcaster is the subscription manager's web-facing interface.
type EventBroadcaster interface {
	RegisterSSE() (int, <-chan *protocol.StreamEvent)
	UnregisterSSE(id int)
	Snapshot() protocol.SubscribeResponse
}

// InteractionHandler routes interaction responses back to pending prompts.
type InteractionHandler interface {
	HandleResponse(resp protocol.InteractionResponse)
}

// SessionLister provides access to in-memory sessions.
type SessionLister interface {
	List() []*session.Session
	Get(agent, sessionID string) *session.Session
}

// RAGSearcher provides semantic search (may be nil if RAG is not configured).
type RAGSearcher interface {
	Search(collection, query string, topK int) ([]SearchResult, error)
}

// SearchResult is the web-layer search result type.
type SearchResult struct {
	Content string  `json:"content"`
	Source  string  `json:"source"`
	Score   float32 `json:"score"`
}

// Services bundles everything the web server needs from the daemon.
type Services struct {
	Events       EventBroadcaster
	Interactions InteractionHandler
	Sessions     SessionLister
	RAG          RAGSearcher // may be nil
	DaemonInfo   func() protocol.DaemonInfo
}

// Server is the ragtime HTTP server.
type Server struct {
	addr    string
	svc     Services
	logger  *slog.Logger
	httpSrv *http.Server
}

// NewServer creates a web server bound to the given address.
func NewServer(addr string, svc Services, logger *slog.Logger) *Server {
	s := &Server{addr: addr, svc: svc, logger: logger}

	mux := http.NewServeMux()

	// Static files (SPA fallback to index.html)
	mux.Handle("/", spaHandler())
	mux.HandleFunc("/icon-192.png", serveIcon(192))
	mux.HandleFunc("/icon-512.png", serveIcon(512))

	// API
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/events", s.handleSSE)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/{agent}/{id}/history", s.handleSessionHistory)
	mux.HandleFunc("/api/interactions/{id}/respond", s.handleInteractionRespond)
	mux.HandleFunc("/api/search", s.handleSearch)

	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return s
}

// Start begins listening. Non-blocking.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	go s.httpSrv.Serve(ln)
	return nil
}

// Stop shuts the server down gracefully.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.httpSrv.Shutdown(ctx)
}

// --- SSE ---

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Send initial snapshot
	snap := s.svc.Events.Snapshot()
	if err := writeSSE(w, "connected", snap); err == nil {
		flusher.Flush()
	}

	id, ch := s.svc.Events.RegisterSSE()
	defer s.svc.Events.UnregisterSSE(id)

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSE(w, event.Kind, event); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSE(w http.ResponseWriter, kind string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
	return err
}

// --- REST ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.svc.DaemonInfo())
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.svc.Sessions.List()
	type row struct {
		Agent      string     `json:"agent"`
		SessionID  string     `json:"session_id"`
		StartedAt  time.Time  `json:"started_at"`
		EventCount int        `json:"event_count"`
		LastEvent  *time.Time `json:"last_event,omitempty"`
	}
	rows := make([]row, len(sessions))
	for i, sess := range sessions {
		r := row{
			Agent:      sess.Agent,
			SessionID:  sess.SessionID,
			StartedAt:  sess.StartedAt,
			EventCount: sess.EventCount(),
		}
		if recent := sess.RecentEvents(1); len(recent) > 0 {
			t := recent[0].Timestamp
			r.LastEvent = &t
		}
		rows[i] = r
	}
	writeJSON(w, rows)
}

func (s *Server) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	agent := r.PathValue("agent")
	sid := r.PathValue("id")
	last := 50
	if v := r.URL.Query().Get("last"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			last = n
		}
	}

	sess := s.svc.Sessions.Get(agent, sid)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeJSON(w, sess.RecentEvents(last))
}

func (s *Server) handleInteractionRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	var resp protocol.InteractionResponse
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp.ID = id
	s.svc.Interactions.HandleResponse(resp)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if s.svc.RAG == nil {
		http.Error(w, "RAG not configured", http.StatusServiceUnavailable)
		return
	}

	collection := r.URL.Query().Get("collection")
	query := r.URL.Query().Get("q")
	topK := 10
	if v := r.URL.Query().Get("top_k"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			topK = n
		}
	}
	if collection == "" {
		collection = "sessions"
	}
	if query == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}

	results, err := s.svc.RAG.Search(collection, query, topK)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, results)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// spaHandler serves embedded static files, falling back to index.html.
func spaHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to open the file in the embedded FS
		if r.URL.Path != "/" {
			f, err := sub.Open(strings.TrimPrefix(r.URL.Path, "/"))
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// Fallback: serve index.html for SPA routing
		http.ServeFileFS(w, r, sub, "index.html")
	})
}

// serveIcon generates a music note PNG icon at the requested size.
func serveIcon(size int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		bg := color.RGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xff}
		accent := color.RGBA{R: 0x83, G: 0xa5, B: 0x98, A: 0xff}
		draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
		drawMusicNote(img, size, accent)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		png.Encode(w, img)
	}
}

// drawMusicNote draws a ♪ (eighth note) centered in a size×size image.
func drawMusicNote(img *image.RGBA, size int, c color.RGBA) {
	s := float64(size)

	// Note head: tilted filled ellipse
	hcx := int(0.38 * s)
	hcy := int(0.72 * s)
	hrx := int(0.22 * s)
	hry := int(0.14 * s)
	tilt := -0.4 // radians — tilts right-side down
	cosT, sinT := math.Cos(tilt), math.Sin(tilt)
	for y := hcy - hry - 4; y <= hcy+hry+4; y++ {
		for x := hcx - hrx - 4; x <= hcx+hrx+4; x++ {
			dx := float64(x - hcx)
			dy := float64(y - hcy)
			rx := cosT*dx + sinT*dy
			ry := -sinT*dx + cosT*dy
			if rx*rx/float64(hrx*hrx)+ry*ry/float64(hry*hry) <= 1.0 {
				img.Set(x, y, c)
			}
		}
	}

	// Stem: vertical rectangle on the right edge of the note head
	stemX := hcx + hrx - int(0.04*s)
	stemW := max(int(0.055*s), 2)
	stemTop := int(0.20 * s)
	draw.Draw(img, image.Rect(stemX, stemTop, stemX+stemW, hcy), &image.Uniform{c}, image.Point{}, draw.Src)

	// Flag: curved shape from stem top, sweeping right then back in
	flagH := int(0.36 * s)
	flagMaxW := int(0.26 * s)
	for fy := 0; fy < flagH; fy++ {
		t := float64(fy) / float64(flagH)
		// Outer edge follows a sine arc; tapers back to the stem at the bottom
		w := int(float64(flagMaxW) * math.Sin(t*math.Pi*0.85+0.15) * (1 - t*0.3))
		for fx := 0; fx <= w; fx++ {
			img.Set(stemX+stemW+fx, stemTop+fy, c)
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
