package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// ChatIdleSweeper detects sessions that have gone quiet beyond the
// configured idle threshold and schedules an extraction for each. The
// sweeper itself is not a River worker — it runs from the dispatcher
// tick, claiming under FOR UPDATE SKIP LOCKED and writing
// chat_extraction_jobs rows the dispatcher will then drain on the next
// tick.
type ChatIdleSweeper struct {
	Sessions       *store.ChatSessionStore
	Jobs           *store.ChatExtractionJobStore
	IdleAfter      time.Duration
	Logger         *slog.Logger
}

// Sweep runs one pass: claims up to `limit` idle sessions, advances
// each to wrapping_up, and inserts a chat_extraction_jobs row per
// session. Returns the number of sessions claimed.
//
// Idempotent — the chat_extraction_jobs UNIQUE (session_id) constraint
// keeps a re-run from creating duplicate jobs. AdvancePhase tolerates
// "already wrapping_up" gracefully.
func (s *ChatIdleSweeper) Sweep(ctx context.Context, limit int) int {
	if s.IdleAfter <= 0 {
		s.IdleAfter = 20 * time.Minute
	}
	cutoff := time.Now().Add(-s.IdleAfter)
	ids, err := s.Sessions.ClaimIdleForFinalize(ctx, cutoff, limit)
	if err != nil {
		s.Logger.Warn("idle sweep claim", "err", err)
		return 0
	}
	if len(ids) == 0 {
		return 0
	}
	scheduled := 0
	for _, id := range ids {
		// Load the session to read user_id.
		session, err := s.Sessions.GetByIDForWorker(ctx, id)
		if err != nil {
			s.Logger.Warn("idle sweep load session", "err", err, "session_id", id)
			continue
		}
		// Skip if the session is in fact already finalized — race with
		// an explicit-finalize click that just landed.
		if session.Phase == domain.ChatPhaseFinalized {
			continue
		}
		// Mark wrapping_up (idempotent if already there). Set status to
		// pending so the polling endpoint reflects the impending run.
		if _, err := s.Sessions.AdvancePhase(ctx, session.ID, domain.ChatPhaseWrappingUp); err != nil {
			// Tolerate "already wrapping_up" via the idempotent path
			// inside AdvancePhase; only log unexpected errors.
			s.Logger.Warn("idle sweep advance phase", "err", err, "session_id", session.ID)
		}
		if err := s.Sessions.SetExtractionStatus(ctx, session.ID, domain.ChatExtractionPending, nil); err != nil {
			s.Logger.Warn("idle sweep set status pending", "err", err, "session_id", session.ID)
		}
		if _, err := s.Jobs.Schedule(ctx, session.ID, session.UserID); err != nil {
			s.Logger.Warn("idle sweep schedule", "err", err, "session_id", session.ID)
			continue
		}
		scheduled++
	}
	if scheduled > 0 {
		s.Logger.Info("idle sweep scheduled extractions",
			"count", scheduled, "claimed", len(ids))
	}
	return scheduled
}
