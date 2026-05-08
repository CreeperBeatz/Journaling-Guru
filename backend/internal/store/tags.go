package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// TagStore reads and writes the per-user tag list. Tag IDs are permanent
// — a rename updates `label` but never the row identity, so historical
// daily_entry_tags rows still resolve to the renamed label.
type TagStore struct {
	DB *pgxpool.Pool
}

func NewTagStore(db *pgxpool.Pool) *TagStore { return &TagStore{DB: db} }

const tagColumns = `id, user_id, label, normalized_label, valence, status,
    merged_into_tag_id, created_at, updated_at`

func scanTag(row pgx.Row) (*domain.Tag, error) {
	var t domain.Tag
	if err := row.Scan(
		&t.ID, &t.UserID, &t.Label, &t.NormalizedLabel,
		&t.Valence, &t.Status, &t.MergedIntoTagID,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

// NormalizeTagLabel produces the dedup key used by UpsertByLabel. Lower
// case + trim collapsed whitespace. Single source of truth: callers must
// not pre-lower the label; this function owns the rule.
func NormalizeTagLabel(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), " "))
}

// UpsertByLabel returns the existing active tag matching the normalized
// label, or creates one if absent. Re-running is idempotent: chat
// extraction can blast the same labels every session without creating
// duplicates. Valence on conflict is left alone — once a tag is filed
// as a drainer ('negative') we don't flip it just because the LLM
// happened to mention it on the charger side this time.
func (s *TagStore) UpsertByLabel(
	ctx context.Context, userID, label, valence string,
) (*domain.Tag, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil, errors.New("empty tag label")
	}
	normalized := NormalizeTagLabel(label)
	const q = `
		INSERT INTO tags (user_id, label, normalized_label, valence)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, normalized_label) DO UPDATE
		   SET updated_at = now()
		RETURNING ` + tagColumns
	return scanTag(s.DB.QueryRow(ctx, q, userID, label, normalized, valence))
}

// GetByID returns a tag scoped to userID, or nil if not found / wrong tenant.
func (s *TagStore) GetByID(ctx context.Context, userID, id string) (*domain.Tag, error) {
	const q = `SELECT ` + tagColumns + `
	             FROM tags
	            WHERE id = $1 AND user_id = $2`
	t, err := scanTag(s.DB.QueryRow(ctx, q, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// ListActiveByValence returns all status='active' tags for the user with
// the given valence ("positive" or "negative" — neutral has no UI yet).
// Used by the Manual-tab tag picker.
func (s *TagStore) ListActiveByValence(
	ctx context.Context, userID, valence string,
) ([]domain.Tag, error) {
	const q = `SELECT ` + tagColumns + `
	             FROM tags
	            WHERE user_id = $1 AND valence = $2 AND status = 'active'
	            ORDER BY label ASC`
	rows, err := s.DB.Query(ctx, q, userID, valence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Tag, 0)
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// Rename updates the label (and re-derives normalized_label) on a tag.
// Conflicts with another tag's normalized_label return ErrTagDuplicate
// — the caller can offer to merge instead.
var ErrTagDuplicate = errors.New("tag with that label already exists")

func (s *TagStore) Rename(ctx context.Context, userID, id, newLabel string) (*domain.Tag, error) {
	newLabel = strings.TrimSpace(newLabel)
	if newLabel == "" {
		return nil, errors.New("empty tag label")
	}
	normalized := NormalizeTagLabel(newLabel)
	const q = `
		UPDATE tags
		   SET label = $3,
		       normalized_label = $4,
		       updated_at = now()
		 WHERE id = $1 AND user_id = $2
		RETURNING ` + tagColumns
	t, err := scanTag(s.DB.QueryRow(ctx, q, id, userID, newLabel, normalized))
	if err != nil && strings.Contains(err.Error(), "tags_user_normalized_unique") {
		return nil, ErrTagDuplicate
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// Merge folds src into dst: every daily_entry_tags row pointing at src
// is rewritten to dst (ON CONFLICT DO NOTHING handles the case where the
// day already had both tags), then src is marked status='merged' with
// merged_into_tag_id=dst. Both tags must belong to userID.
//
// Done in a transaction so a partial failure (e.g. Postgres restart)
// doesn't leave a tag without its history.
func (s *TagStore) Merge(ctx context.Context, userID, srcID, dstID string) error {
	if srcID == dstID {
		return errors.New("cannot merge a tag into itself")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Both tags scoped to user — guards against a malicious client trying
	// to merge into another tenant's tag.
	var ok int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*)::int FROM tags
		   WHERE user_id = $1 AND id = ANY($2)`,
		userID, []string{srcID, dstID},
	).Scan(&ok); err != nil {
		return err
	}
	if ok != 2 {
		return errors.New("tag not found or wrong tenant")
	}

	// Rewrite link rows. The (user_id, local_date, tag_id, role) PK
	// means a day that had *both* tags would conflict — skip those.
	if _, err := tx.Exec(ctx, `
		INSERT INTO daily_entry_tags (user_id, local_date, tag_id, role, created_at)
		SELECT user_id, local_date, $2, role, created_at
		  FROM daily_entry_tags
		 WHERE user_id = $1 AND tag_id = $3
		ON CONFLICT DO NOTHING`,
		userID, dstID, srcID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM daily_entry_tags
		  WHERE user_id = $1 AND tag_id = $2`,
		userID, srcID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tags
		   SET status = 'merged',
		       merged_into_tag_id = $2,
		       updated_at = now()
		 WHERE id = $1`,
		srcID, dstID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Archive marks a tag inactive. History rows in daily_entry_tags are
// preserved — the tag still resolves for past-day reads, just won't
// surface in the picker or in Zone-2 totals.
func (s *TagStore) Archive(ctx context.Context, userID, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE tags SET status = 'archived', updated_at = now()
		   WHERE id = $1 AND user_id = $2 AND status = 'active'`,
		id, userID,
	)
	return err
}
