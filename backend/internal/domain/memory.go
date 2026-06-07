package domain

import "time"

// Memory is one durable fact about the user's life ("sister Mara lives
// nearby", "works as a nurse on night shifts"), accumulated by the
// per-day reconciliation pass over the canonical journal record and
// injected into chat system prompts. See migration 0023_user_memories.sql
// for the lifecycle (active / superseded / deleted) and the pinned
// manual-wins contract.
type Memory struct {
	ID       string `json:"id"`
	UserID   string `json:"-"`
	Category string `json:"category"` // one of MemoryCategories
	Content  string `json:"content"`
	Status   string `json:"status"` // "active" | "superseded" | "deleted"
	// Pinned means user-edited or user-created: the reconciliation
	// worker must never update or delete this row.
	Pinned       bool    `json:"pinned"`
	Source       string  `json:"source"` // "extraction" | "user"
	SupersededBy *string `json:"-"`
	// SourceLocalDate is the user-local day (YYYY-MM-DD) whose journal
	// record produced / last touched this memory. Nil for user-created.
	SourceLocalDate *string   `json:"source_local_date,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Memory status values match the user_memories.status CHECK constraint.
const (
	MemoryStatusActive     = "active"
	MemoryStatusSuperseded = "superseded"
	MemoryStatusDeleted    = "deleted"
)

// Memory source values match the user_memories.source CHECK constraint.
const (
	MemorySourceExtraction = "extraction"
	MemorySourceUser       = "user"
)

// MemoryCategories is the canonical category order — used by the CHECK
// constraint, the reconciliation prompt, and the prompt-injection
// grouping. Keep in sync with 0023_user_memories.sql.
var MemoryCategories = []string{
	"identity", "relationships", "work", "health",
	"preferences", "goals", "routines", "other",
}

// IsValidMemoryCategory reports whether c is one of MemoryCategories.
func IsValidMemoryCategory(c string) bool {
	for _, v := range MemoryCategories {
		if v == c {
			return true
		}
	}
	return false
}

// MemoryOp is one validated reconciliation operation, produced by the
// LLM pass (llm/chat.MemoryReconcile) and applied transactionally by
// store.MemoryStore.ApplyExtractionOps. Lives in domain so neither
// package imports the other.
type MemoryOp struct {
	Op       string // "add" | "update" | "delete"
	ID       string // target memory id for update/delete; empty for add
	Category string // for add/update
	Content  string // for add/update
}

// MemoryOp op values.
const (
	MemoryOpAdd    = "add"
	MemoryOpUpdate = "update"
	MemoryOpDelete = "delete"
)

// MemoryExtractionJob mirrors one memory_extraction_jobs row — the
// scheduling source of truth for the per-day reconciliation pass.
// Lifecycle parallels SummaryJob.
type MemoryExtractionJob struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	LocalDate string     `json:"local_date"` // YYYY-MM-DD
	FireAt    time.Time  `json:"fire_at"`
	FiredAt   *time.Time `json:"fired_at,omitempty"`
	Status    string     `json:"status"` // pending|claimed|completed|skipped|failed
	Attempts  int        `json:"attempts"`
	LastError *string    `json:"last_error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
