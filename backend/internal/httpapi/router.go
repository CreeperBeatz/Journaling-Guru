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
	"github.com/cosmosthrace/journai/backend/internal/jobs"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/llm/realtime"
	"github.com/cosmosthrace/journai/backend/internal/mail"
	"github.com/cosmosthrace/journai/backend/internal/push"
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
	dailyInputs := store.NewDailyInputStore(db)
	summaries := store.NewSummaryStore(db)
	summaryJobs := store.NewSummaryJobStore(db)
	tags := store.NewTagStore(db)
	dailyEntryTags := store.NewDailyEntryTagStore(db)
	goals := store.NewGoalStore(db)
	goalCheckIns := store.NewGoalCheckInStore(db)
	pushSubs := store.NewPushSubscriptionStore(db)
	reminderJobs := store.NewReminderJobStore(db)
	chatSessions := store.NewChatSessionStore(db)
	chatMessages := store.NewChatMessageStore(db)
	chatExtractionJobs := store.NewChatExtractionJobStore(db)
	weeklyReflections := store.NewWeeklyReflectionStore(db)
	monthlyReflections := store.NewMonthlyReflectionStore(db)
	memories := store.NewMemoryStore(db)
	memoryJobs := store.NewMemoryExtractionJobStore(db)

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

	// Lazy-seed scheduler — invoked from the entries handler on every
	// successful write. ScheduleNext is the worker's responsibility, not
	// the api's, but the api owns the LazySeed lifecycle.
	scheduler := &jobs.Scheduler{
		Jobs:           summaryJobs,
		Users:          users,
		Logger:         logger,
		InactivityDays: cfg.SummaryInactivityDays,
	}

	// Reminder scheduler — invoked from /me PATCH and /push/subscribe.
	// ScheduleNext is the worker's job; the api owns Replan.
	reminderScheduler := &jobs.ReminderScheduler{
		Jobs:   reminderJobs,
		Users:  users,
		Logger: logger,
	}

	// Memory scheduler — arms the per-day memory reconciliation pass on
	// every entry / daily-input write, same lazy-seed contract as the
	// summary scheduler.
	memoryScheduler := &jobs.MemoryScheduler{
		Jobs:   memoryJobs,
		Users:  users,
		Logger: logger,
	}

	// Push sender. Built once if VAPID keys are configured; nil when
	// they aren't, in which case /api/push/* returns 503 instead of
	// silently no-opping. Constructing here (api binary) lets /test
	// run end-to-end without the worker.
	var pushSender push.Sender
	if c, err := push.New(push.Config{
		PublicKey:  cfg.VAPIDPublic,
		PrivateKey: cfg.VAPIDPrivate,
		Subject:    cfg.VAPIDSubject,
	}); err == nil {
		pushSender = c
	} else {
		logger.Warn("push sender not initialized — VAPID keys missing", "err", err)
	}

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
	meH := &handlers.MeHandler{
		Users:     users,
		Logger:    logger,
		Replanner: reminderScheduler,
		// Feed the derived reflection_pending flag (Weekly nav carry-over).
		WeeklyReflections: weeklyReflections,
		DailyInputs:       dailyInputs,
	}
	acctH := &handlers.AccountHandler{
		Users:        users,
		Logger:       logger,
		CookieName:   cfg.SessionCookieName,
		CookieSecure: cfg.CookieSecure,
	}
	questionsH := &handlers.QuestionHandler{Questions: questions, Logger: logger}
	entriesH := &handlers.EntryHandler{
		Entries:           entries,
		Users:             users,
		WeeklyReflections: weeklyReflections,
		Logger:            logger,
		Scheduler:         scheduler,
		MemoryScheduler:   memoryScheduler,
	}
	dailyInputsH := &handlers.DailyInputHandler{
		Inputs:          dailyInputs,
		Users:           users,
		Tags:            tags,
		DailyEntryTags:  dailyEntryTags,
		Logger:          logger,
		Scheduler:       scheduler,
		MemoryScheduler: memoryScheduler,
	}
	memoriesH := &handlers.MemoryHandler{Memories: memories, Logger: logger}
	tagsH := &handlers.TagHandler{Tags: tags, Logger: logger}
	goalsH := &handlers.GoalHandler{
		Goals:          goals,
		CheckIns:       goalCheckIns,
		Users:          users,
		DailyEntryTags: dailyEntryTags,
		DailyInputs:    dailyInputs,
		Logger:         logger,
		// ChatLLM + ChatModel + ClassifyLLM/Model filled in below
		// alongside the chat handler — the shaper streams via the same
		// chat-tier client; suggest uses the cheap classify-tier client.
	}
	summariesH := &handlers.SummaryHandler{
		Summaries:          summaries,
		Jobs:               summaryJobs,
		Users:              users,
		DailyInputs:        dailyInputs,
		DailyEntryTags:     dailyEntryTags,
		Goals:              goals,
		CheckIns:           goalCheckIns,
		WeeklyReflections:  weeklyReflections,
		MonthlyReflections: monthlyReflections,
		ChatSessions:       chatSessions,
		Logger:             logger,
		DevForceMonth:      cfg.DevForceFlags,
	}
	pushH := &handlers.PushHandler{
		Subs:        pushSubs,
		Users:       users,
		Reminders:   reminderJobs,
		Replanner:   reminderScheduler,
		Sender:      pushSender,
		Logger:      logger,
		VAPIDPublic: cfg.VAPIDPublic,
		AppOrigin:   cfg.PublicBaseURL,
	}
	chatLLM := llm.NewOpenRouter(
		cfg.OpenRouterKey, cfg.ChatModel,
		cfg.PublicBaseURL, "Journaling Guru",
	)
	classifyLLM := llm.NewOpenRouter(
		cfg.OpenRouterKey, cfg.ClassifyModel,
		cfg.PublicBaseURL, "Journaling Guru",
	)
	// Realtime client for Phase 6b (Talk). Constructed even when
	// OPENAI_API_KEY is empty — MintEphemeralSecret returns
	// realtime.ErrNoAPIKey at call time, mapped to 503 in the handler,
	// so dev environments without the key still serve text chat.
	realtimeClient := realtime.New(cfg.OpenAIKey, cfg.OpenAIRealtimeModel)
	chatH := &handlers.ChatHandler{
		Sessions:           chatSessions,
		Messages:           chatMessages,
		Jobs:               chatExtractionJobs,
		Questions:          questions,
		Goals:              goals,
		Users:              users,
		DailyInputs:        dailyInputs,
		WeeklyReflections:  weeklyReflections,
		MonthlyReflections: monthlyReflections,
		Summaries:          summaries,
		DailyEntryTags:     dailyEntryTags,
		Memories:           memories,
		ChatLLM:            chatLLM,
		ClassifyLLM:        classifyLLM,
		Realtime:           realtimeClient,
		Logger:             logger,
		ChatModel:          cfg.ChatModel,
		ClassifyModel:      cfg.ClassifyModel,
		RealtimeModel:      cfg.OpenAIRealtimeModel,
		MaxTurns:           cfg.ChatMaxTurns,
		HardCapMinutes:     cfg.ChatHardCapMinutes,
		KeepLastN:          cfg.ChatTranscriptKeepLast,
		ResourcesURL:       cfg.ChatCrisisResourcesURL,
	}
	// SMART shaper streams via the same chat-tier client. The model
	// constant is the chat model unless a per-call override is added
	// later (per-user model preference is a v2.x feature).
	goalsH.ChatLLM = chatLLM
	goalsH.ChatModel = cfg.ChatModel
	goalsH.ClassifyLLM = classifyLLM
	goalsH.ClassifyModel = cfg.ClassifyModel

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
	// Global Timeout removed — applied per route group below so chat's
	// SSE handlers can use a longer ceiling without bouncing the whole
	// request context every 30 s.

	r.Get("/healthz", healthH.Healthz)
	r.Get("/readyz", healthH.Readyz)

	r.Route("/api", func(r chi.Router) {
		// Default request timeout for all non-streaming /api routes.
		// Chat endpoints (mounted further down) override this with 120 s
		// so streaming assistant turns aren't cut at 30 s.
		r.Group(func(r chi.Router) {
			r.Use(chimw.Timeout(30 * time.Second))

			r.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"version":"v2-dev","phase":6}`))
			})

			// Public read: VAPID public key. The PWA fetches this on first
			// subscribe attempt — no session required because a logged-out
			// install screen still needs to set up SW + manifest.
			r.Get("/push/vapid-public-key", pushH.VAPIDKey)

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

					r.Put("/daily/inputs", dailyInputsH.Upsert)
					r.Patch("/daily/inputs/by-date/{date}", dailyInputsH.UpdateByDate)

					r.Post("/tags", tagsH.Create)
					r.Patch("/tags/{id}", tagsH.Update)
					r.Post("/tags/{id}/merge", tagsH.Merge)
					r.Delete("/tags/{id}", tagsH.Archive)

					// /goals routes are mounted on a dedicated subrouter
					// further down (chi requires a single Route block per
					// prefix; the SSE /draft endpoint needs to escape the
					// chimw.Timeout middleware, so we keep them together).

					r.Post("/summaries/regenerate", summariesH.Regenerate)

					// Phase 7 — weekly reflection wizard mutators.
					r.Post("/reflection/this-week/start", summariesH.StartReflection)
					r.Patch("/reflection/this-week", summariesH.PatchReflection)
					r.Post("/reflection/this-week/complete", summariesH.CompleteReflection)
					r.Post("/reflection/this-week/replay", summariesH.ReplayReflection)
					// Monthly reflection — intention + life check-in for the
					// month the current week hosts (404 on plain weeks).
					r.Post("/reflection/this-month/intention", summariesH.SetMonthlyIntention)
					r.Post("/reflection/this-month/ratings", summariesH.SetMonthlyRatings)
					// History view edits — same partial-update shape, scoped
					// to a specific past week.
					r.Patch("/reflection/by-week/{week_start}", summariesH.PatchReflectionByWeek)
					// Weekly reflection chat — create-or-resume the weekly-
					// scoped chat session for step 2 of the wizard.
					r.Post("/reflection/this-week/chat", chatH.CreateOrResumeWeekly)

					r.Post("/push/subscribe", pushH.Subscribe)
					r.Delete("/push/subscribe", pushH.Unsubscribe)
					r.Post("/push/test", pushH.Test)

					// Memory management — user writes pin the row
					// against the reconciliation worker (manual-wins).
					r.Post("/memories", memoriesH.Create)
					r.Patch("/memories/{id}", memoriesH.Update)
					r.Delete("/memories/{id}", memoriesH.Delete)
				})
			})

			// Authenticated reads.
			r.Group(func(r chi.Router) {
				r.Use(mw.RequireAuth(sessionSvc, cfg.SessionCookieName))
				r.Get("/me", meH.Get)
				r.Get("/questions", questionsH.List)
				r.Get("/entries", entriesH.ListByDate)
				r.Get("/entries/dates", entriesH.ListDates)
				r.Get("/questions/{id}/entries", entriesH.ListByQuestion)
				r.Get("/history/heatmap", entriesH.Heatmap)

				r.Get("/daily/inputs", dailyInputsH.Get)

				r.Get("/tags", tagsH.List)

				// /goals reads also live on the dedicated /goals subrouter.

				r.Get("/summaries", summariesH.List)
				r.Get("/summaries/stats", summariesH.Stats)
				r.Get("/summaries/jobs/status", summariesH.JobStatus)
				r.Get("/summaries/{id}", summariesH.Get)

				// Energy Audit three-zone summary surface.
				r.Get("/summary/zone1", summariesH.Zone1)
				r.Get("/summary/zone2", summariesH.Zone2)
				r.Get("/summary/zone3", summariesH.Zone3)

				// Phase 7 — Weekly reflection pattern view.
				r.Get("/reflection/this-week", summariesH.WeeklyReflection)
				r.Get("/reflection/by-week/{week_start}", summariesH.ReflectionByWeek)
				// Weekly reflection chat reads.
				r.Get("/reflection/this-week/chat", chatH.ThisWeekChat)
				r.Get("/reflection/by-week/{week_start}/chat", chatH.ChatByWeek)

				r.Get("/push/state", pushH.State)

				r.Get("/memories", memoriesH.List)
			})
		})

		// /api/goals — single subrouter so chi doesn't have to choose
		// between sibling matchers. The /draft SSE stream skips
		// chimw.Timeout (Flusher issue, same as /api/chat); the rest
		// of the goals routes get the standard 30s timeout.
		r.Route("/goals", func(r chi.Router) {
			r.Use(mw.RequireAuth(sessionSvc, cfg.SessionCookieName))

			// Streaming endpoint — no Timeout.
			r.Group(func(r chi.Router) {
				r.Use(mw.CSRF)
				r.Post("/draft", goalsH.Draft)
			})

			// Non-streaming endpoints — with Timeout.
			r.Group(func(r chi.Router) {
				r.Use(chimw.Timeout(30 * time.Second))
				r.Get("/", goalsH.List)
				r.Group(func(r chi.Router) {
					r.Use(mw.CSRF)
					r.Post("/", goalsH.Create)
					r.Patch("/{id}", goalsH.Update)
					r.Post("/{id}/check-ins", goalsH.CheckIn)
					r.Post("/suggest", goalsH.Suggest)
				})
			})
		})

		// /api/chat — NO chimw.Timeout here. The Timeout middleware's
		// internal ResponseWriter wrapper does not implement
		// http.Flusher, which kills SSE streaming. Streaming endpoints
		// rely on context cancellation from the client (the request
		// context fires when the browser disconnects); short non-
		// streaming endpoints finish well under the global 30s anyway.
		// All chat routes require auth; mutating verbs go through CSRF.
		r.Route("/chat", func(r chi.Router) {
			r.Use(mw.RequireAuth(sessionSvc, cfg.SessionCookieName))

			// Reads.
			r.Get("/sessions/today", chatH.Today)
			r.Get("/sessions/by-date/{date}", chatH.ByDate)
			r.Get("/sessions/{id}/extraction/status", chatH.ExtractionStatus)
			// Opener is a GET (no body) so EventSource-style clients
			// could in principle subscribe; we use fetch+ReadableStream
			// on the FE for symmetry with POST /messages.
			r.Get("/sessions/{id}/opener", chatH.Opener)

			// Mutators.
			r.Group(func(r chi.Router) {
				r.Use(mw.CSRF)
				r.Post("/sessions", chatH.CreateOrResume)
				r.Post("/sessions/{id}/messages", chatH.StreamMessage)
				r.Post("/sessions/{id}/wrap-up", chatH.WrapUp)
				r.Post("/sessions/{id}/wrap-up/cancel", chatH.CancelWrapUp)
				r.Post("/sessions/{id}/finalize", chatH.Finalize)
				r.Post("/sessions/{id}/reset", chatH.Reset)
				// Inline-card events (e.g. user_accepted_goal) the FE
				// injects so the next assistant turn sees the user's
				// decision in the transcript.
				r.Post("/sessions/{id}/system-event", chatH.AppendSystemEvent)
				// Phase 6b — Talk. /voice/start mints an OpenAI Realtime
				// ephemeral client_secret; /voice/transcript persists
				// finalized transcript turns the browser receives off the
				// data channel. Audio never traverses this server.
				r.Post("/sessions/{id}/voice/start", chatH.StartVoice)
				r.Post("/sessions/{id}/voice/transcript", chatH.AppendVoiceTranscript)
			})
		})
	})

	return r
}
