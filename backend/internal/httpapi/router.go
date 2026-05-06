package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/auth"
	"github.com/cosmosthrace/journai/backend/internal/config"
	"github.com/cosmosthrace/journai/backend/internal/httpapi/handlers"
	mw "github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/mail"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// NewRouter wires the HTTP surface for the api binary.
//
// Construction order: stores → services → handlers → routes. Anything that
// needs the DB pool or external creds is built once here and reused across
// requests; chi composes the per-route middleware stack (CSRF + RequireAuth)
// on top.
func NewRouter(cfg *config.Config, db *pgxpool.Pool, logger *slog.Logger) http.Handler {
	users := store.NewUserStore(db)
	links := store.NewMagicLinkStore(db)
	sessions := store.NewSessionStore(db)
	questions := store.NewQuestionStore(db)
	entries := store.NewEntryStore(db)

	magicSvc := auth.NewMagicLinkService(auth.MagicLinkConfig{
		TTL:         cfg.MagicLinkTTL(),
		PerEmail15m: cfg.MagicLinkPerEmail15m,
		PerEmailDay: cfg.MagicLinkPerEmailDay,
		PerIPHour:   cfg.MagicLinkPerIPHour,
	}, users, links)
	sessionSvc := auth.NewSessionService(auth.SessionConfig{
		CookieName:   cfg.SessionCookieName,
		TTL:          cfg.SessionTTL(),
		CookieSecure: cfg.CookieSecure,
	}, sessions)

	mailer := mail.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom)

	authH := &handlers.AuthHandler{
		Magic:         magicSvc,
		Sessions:      sessionSvc,
		Mailer:        mailer,
		Logger:        logger,
		PublicBaseURL: cfg.PublicBaseURL,
		CookieName:    cfg.SessionCookieName,
		CookieSecure:  cfg.CookieSecure,
		CookieTTL:     cfg.SessionTTL(),
		MagicTTL:      cfg.MagicLinkTTL(),
	}
	meH := &handlers.MeHandler{Users: users, Logger: logger}
	acctH := &handlers.AccountHandler{
		Users:        users,
		Logger:       logger,
		CookieName:   cfg.SessionCookieName,
		CookieSecure: cfg.CookieSecure,
	}
	questionsH := &handlers.QuestionHandler{Questions: questions, Logger: logger}
	entriesH := &handlers.EntryHandler{Entries: entries, Users: users, Logger: logger}
	healthH := handlers.NewHealth(db)

	r := chi.NewRouter()

	// AllowCredentials=true because the cookie carries the session; that
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

	r.Get("/healthz", healthH.Healthz)
	r.Get("/readyz", healthH.Readyz)

	r.Route("/api", func(r chi.Router) {
		r.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"v2-dev","phase":3}`))
		})

		// Mutating endpoints: CSRF gate (X-Requested-With required).
		r.Group(func(r chi.Router) {
			r.Use(mw.CSRF)

			// Public auth endpoints — no session required.
			r.Post("/auth/magic-link", authH.MagicLinkRequest)
			r.Post("/auth/verify", authH.MagicLinkVerify)
			r.Post("/auth/logout", authH.Logout)

			// Authenticated mutators.
			r.Group(func(r chi.Router) {
				r.Use(mw.RequireAuth(sessionSvc, cfg.SessionCookieName))
				r.Delete("/account", acctH.Delete)
				r.Patch("/me", meH.Update)

				r.Post("/questions", questionsH.Create)
				r.Patch("/questions/{id}", questionsH.Update)
				r.Delete("/questions/{id}", questionsH.Archive)
				r.Post("/questions/reorder", questionsH.Reorder)

				r.Put("/entries", entriesH.Upsert)
				r.Patch("/entries/{id}", entriesH.UpdateByID)
			})
		})

		// Authenticated reads.
		r.Group(func(r chi.Router) {
			r.Use(mw.RequireAuth(sessionSvc, cfg.SessionCookieName))
			r.Get("/me", meH.Get)
			r.Get("/questions", questionsH.List)
			r.Get("/entries", entriesH.ListByDate)
			r.Get("/entries/dates", entriesH.ListDates)
		})
	})

	return r
}
