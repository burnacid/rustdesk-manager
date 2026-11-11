// Package server wires the client API, the admin API and the web UI into one router.
package server

import (
	"context"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gorm.io/gorm"

	"rustdesk-manager/internal/abook"
	"rustdesk-manager/internal/adminapi"
	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/cleanup"
	"rustdesk-manager/internal/clientapi"
	"rustdesk-manager/internal/config"
	"rustdesk-manager/internal/live"
	"rustdesk-manager/internal/secret"
	"rustdesk-manager/internal/settings"
	"rustdesk-manager/internal/sso"
	"rustdesk-manager/internal/strategy"
	"rustdesk-manager/web"
)

type Server struct {
	cfg      *config.Config
	db       *gorm.DB
	client   *clientapi.API
	admin    *adminapi.API
	settings *settings.Store
}

// StartBackground runs housekeeping (old data cleanup) until ctx ends.
func (s *Server) StartBackground(ctx context.Context) {
	go cleanup.Run(ctx, s.db, s.settings, s.cfg.RecordingsDir())
}

func New(cfg *config.Config, gdb *gorm.DB) *Server {
	state := live.New()
	// One limiter for web and client logins, so failures count across both.
	limiter := auth.NewLimiter(10, 15*time.Minute)
	box := secret.New(cfg.SessionSecret)
	abooks := abook.New(gdb, box)
	strat := strategy.New(gdb)
	store := settings.New(gdb)
	ssoSvc := sso.New(gdb, box, cfg.PublicURL)
	client := clientapi.New(clientapi.Deps{Cfg: cfg, DB: gdb, Live: state, Limiter: limiter, Abooks: abooks,
		Strategies: strat, Box: box, Settings: store, SSO: ssoSvc})
	admin := adminapi.New(adminapi.Deps{Cfg: cfg, DB: gdb, Live: state, Devices: client,
		Limiter: limiter, Abooks: abooks, Strategies: strat, Box: box, Settings: store, SSO: ssoSvc})
	ssoSvc.SetHooks(sso.Hooks{ClientLogin: client.IssueClientLogin, WebLogin: admin.StartWebSession})
	return &Server{cfg: cfg, db: gdb, client: client, admin: admin, settings: store}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// RustDesk client API.
	r.Route("/api", func(r chi.Router) {
		s.client.Routes(r)
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			// The client treats 404 as "feature not supported by this server".
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		})
	})

	// Admin UI API.
	r.Route("/admin/api", s.admin.Routes)

	r.Handle("/*", spaHandler())
	return r
}

// spaHandler serves the embedded React build and falls back to index.html for client-side routes.
func spaHandler() http.Handler {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := dist.Open(p); err == nil {
				_ = f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, dist, "index.html")
	})
}
