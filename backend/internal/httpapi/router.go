package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/config"
	"github.com/cosmosthrace/journai/backend/internal/httpapi/handlers"
	mw "github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
)

// NewRouter wires the HTTP surface for the api binary.
//
// Phase 1 ships /healthz and /readyz only; auth/journal/voice/summary/push
// routes are added in their own phases. The middleware stack here is the
// one Phase 2 will hang authenticated routes off of.
func NewRouter(cfg *config.Config, db *pgxpool.Pool, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	// AllowCredentials=true because Phase 2 ships cookie-based sessions; that
	// means AllowedOrigins must be an explicit list, not "*".
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(mw.RequestID)
	r.Use(mw.Recoverer(logger))
	r.Use(mw.AccessLog(logger))
	r.Use(chimw.Timeout(30 * time.Second))

	health := handlers.NewHealth(db)
	r.Get("/healthz", health.Healthz)
	r.Get("/readyz", health.Readyz)

	r.Route("/api", func(r chi.Router) {
		// Auth, journal, voice, summaries, push, and account endpoints land
		// here in subsequent phases. Keep this block minimal until then so
		// that misrouted requests fail loudly with 404 instead of silently
		// matching a half-implemented handler.
		r.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"v2-dev","phase":1}`))
		})
	})

	return r
}
