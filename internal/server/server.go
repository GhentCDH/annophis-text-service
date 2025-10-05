package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Server struct {
	cfg        ServerConfig
	httpClient *http.Client
	cache      *cexCache
}

type cexCache struct {
	mu   sync.RWMutex
	data map[string]cacheEntry
	ttl  time.Duration
}
type cacheEntry struct {
	body []byte
	at   time.Time
}

func LoadConfiguration(file string) (ServerConfig, error) {
	f, err := os.Open(file)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	var cfg ServerConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return ServerConfig{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

func NewServer(cfg ServerConfig) *Server {
	return &Server{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		cache: &cexCache{
			data: make(map[string]cacheEntry),
			ttl:  2 * time.Minute,
		},
	}
}

func (c *cexCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ent, ok := c.data[key]
	if !ok || time.Since(ent.at) > c.ttl {
		return nil, false
	}
	return ent.body, true
}
func (c *cexCache) set(key string, body []byte) {
	c.mu.Lock()
	c.data[key] = cacheEntry{body: body, at: time.Now()}
	c.mu.Unlock()
}

// pickSource: accept file-or-directory cfg.Source, optional CEX path param or ?cex= query; fallback to TestSource.
func pickSource(cfg ServerConfig, cex string, q url.Values) string {
	if cex == "" {
		cex = strings.TrimSpace(q.Get("cex"))
	}
	base := strings.TrimSpace(cfg.Source)
	if strings.HasSuffix(strings.ToLower(base), ".cex") {
		return base
	}
	if base != "" {
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		if cex != "" {
			return base + cex + ".cex"
		}
	}
	return cfg.TestSource
}

// checkSourceReachable tries HEAD first then a 1-byte GET. Live (no cache).
func (s *Server) checkSourceReachable(ctx context.Context, u string) error {
	ua := "annophis-text-service/1.0"
	if req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil); err == nil {
		req.Header.Set("User-Agent", ua)
		resp, err := s.httpClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
		return nil
	}
	return fmt.Errorf("health check status %d", resp.StatusCode)
}

func (s *Server) getContent(ctx context.Context, u string) ([]byte, error) {
	if body, ok := s.cache.get(u); ok {
		return body, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "annophis-text-service/1.0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	s.cache.set(u, body)
	return body, nil
}

func BuildRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer, middleware.Timeout(30*time.Second))

	origins := strings.Split(strings.TrimSpace(os.Getenv("ORIGIN_ALLOWED")), ",")
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "X-Requested-With"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Versions
	r.Get("/", s.handleCiteVersion)
	r.Get("/cite", s.handleCiteVersion)
	r.Get("/texts/version", s.handleTextsVersion)

	// Base (no explicit CEX) — uses pickSource fallback logic
	r.Get("/texts", s.handleWorkURNs)
	r.Get("/texts/catalog", s.handleCatalog)
	r.Get("/texts/first/{URN}", s.handleFirst)
	r.Get("/texts/last/{URN}", s.handleLast)
	r.Get("/texts/previous/{URN}", s.handlePrev)
	r.Get("/texts/next/{URN}", s.handleNext)
	r.Get("/texts/urns/{URN}", s.handleURNs)
	r.Get("/texts/{URN}", s.handlePassage)

	// With {CEX} directory base
	r.Route("/{CEX}", func(r chi.Router) {
		r.Get("/texts", s.handleWorkURNs)
		r.Get("/texts/catalog", s.handleCatalog)
		r.Get("/texts/first/{URN}", s.handleFirst)
		r.Get("/texts/last/{URN}", s.handleLast)
		r.Get("/texts/previous/{URN}", s.handlePrev)
		r.Get("/texts/next/{URN}", s.handleNext)
		r.Get("/texts/urns/{URN}", s.handleURNs)
		r.Get("/texts/{URN}", s.handlePassage)
	})

	// healthz
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		src := pickSource(s.cfg, "", r.URL.Query())
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := s.checkSourceReachable(ctx, src); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status":  "unhealthy",
				"source":  src,
				"message": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ok",
			"source": src,
		})
	})

	return r
}

// ---- small shared helpers (kept here so all handlers can use them) ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func servicePathFirstLast(first bool) string {
	if first {
		return "/texts/first"
	}
	return "/texts/last"
}
