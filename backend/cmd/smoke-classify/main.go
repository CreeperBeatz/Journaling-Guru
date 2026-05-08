// smoke-classify is a one-off CLI for verifying the post-turn coverage
// classifier against real session data. Reads the most recent chat
// session, loads its transcript + active questions, and invokes
// chat.Classify directly so we can see the raw LLM output and the
// post-validation filtered set.
//
// Usage: DATABASE_URL=... OPENROUTER_API_KEY=... CLASSIFY_MODEL=...
//        go run ./cmd/smoke-classify [session-id]
//
// If no session id is passed, the most recent chat_sessions row is used.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/llm/chat"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL required")
	}
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENROUTER_API_KEY required")
	}
	model := os.Getenv("CLASSIFY_MODEL")
	if model == "" {
		model = os.Getenv("OPENROUTER_MODEL")
	}
	if model == "" {
		model = "google/gemma-4-26b-a4b-it"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	sessions := store.NewChatSessionStore(pool)
	messages := store.NewChatMessageStore(pool)
	questions := store.NewQuestionStore(pool)

	var sessionID string
	if len(os.Args) > 1 {
		sessionID = os.Args[1]
	} else {
		row := pool.QueryRow(ctx, `SELECT id FROM chat_sessions ORDER BY created_at DESC LIMIT 1`)
		if err := row.Scan(&sessionID); err != nil {
			log.Fatalf("pick latest session: %v", err)
		}
	}

	session, err := sessions.GetByIDForWorker(ctx, sessionID)
	if err != nil {
		log.Fatalf("load session %s: %v", sessionID, err)
	}
	transcript, err := messages.ListBySession(ctx, sessionID)
	if err != nil {
		log.Fatalf("load transcript: %v", err)
	}
	qs, err := questions.ListActive(ctx, session.UserID)
	if err != nil {
		log.Fatalf("load questions: %v", err)
	}
	views := chat.QuestionViewsFromDomain(qs)

	fmt.Printf("session_id=%s user_id=%s phase=%s\n", session.ID, session.UserID, session.Phase)
	fmt.Printf("active questions: %d\n", len(views))
	for _, v := range views {
		fmt.Printf("  - (%s) %q\n", v.ID, v.Prompt)
	}
	fmt.Printf("transcript: %d rows (incl. tool/system)\n", len(transcript))
	fmt.Println()

	client := llm.NewOpenRouter(apiKey, model, "https://journai.local", "JournAI smoke-classify")
	fmt.Printf("classify model: %s\n\n", model)

	t0 := time.Now()
	_ = views // questions retained for diagnostic printing only
	covered, err := chat.Classify(ctx, client, chat.CoverageParams{
		Model:    "", // use client default
		Messages: transcript,
	})
	elapsed := time.Since(t0)
	if err != nil {
		fmt.Printf("Classify ERROR after %s: %v\n", elapsed, err)
		os.Exit(2)
	}
	fmt.Printf("Classify returned in %s\n", elapsed)
	fmt.Printf("covered_question_ids (%d): %v\n", len(covered), covered)

	// Echo the JSON form (what the SSE event would carry).
	out, _ := json.Marshal(map[string]any{"covered_question_ids": covered})
	fmt.Printf("SSE payload would be: %s\n", out)
}
