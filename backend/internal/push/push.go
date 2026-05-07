// Package push wraps SherClockHolmes/webpush-go in the smallest surface
// the worker needs: build a subscription struct from our DB columns,
// send a JSON payload, and classify the response so the worker knows
// whether to delete the row, bump its failed_count, or mark it healthy.
//
// The wider library is intentionally kept out of the worker — every
// caller routes through Send(), so VAPID-key wiring, TTL, and error
// taxonomy live in exactly one file.
package push

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Outcome classifies the response from the push service into the three
// actions the worker can take. Keeping this dispatch in one type keeps
// the worker free of HTTP-status guesswork.
type Outcome int

const (
	// OutcomeDelivered: 2xx — push service accepted the payload.
	OutcomeDelivered Outcome = iota
	// OutcomeGone: 404 / 410 — endpoint is permanently retired. The
	// worker must DELETE the push_subscriptions row.
	OutcomeGone
	// OutcomeRetryable: 5xx, 4xx (other than 404/410), or transport
	// failure. The worker increments failed_count; >= 5 failures and
	// the row is deleted by the worker too.
	OutcomeRetryable
)

// Subscription is the shape Send() needs. Source it from a
// push_subscriptions row.
type Subscription struct {
	Endpoint string
	P256dh   string // base64url-encoded ECDH public key
	Auth     string // base64url-encoded auth secret
}

// Sender is the worker's view of the push service. The interface lets
// tests stub the network without webpush-go in the hot path.
type Sender interface {
	Send(ctx context.Context, sub Subscription, payload []byte) (Outcome, error)
}

// Config carries the VAPID material parsed from env once at start-up.
type Config struct {
	PublicKey  string
	PrivateKey string
	Subject    string // "mailto:..." or https URL — push services require one
	TTLSeconds int    // 0 = library default
}

// New builds a Sender. Returns ErrMissingVAPID when keys aren't set —
// the api binary calls New() too (so /api/push/vapid-public-key works
// even before reminders are wired) and gates subscribe on it.
func New(cfg Config) (*HTTPClient, error) {
	if cfg.PublicKey == "" || cfg.PrivateKey == "" {
		return nil, ErrMissingVAPID
	}
	if cfg.Subject == "" {
		return nil, errors.New("push: VAPID subject required (e.g. mailto:you@example.com)")
	}
	return &HTTPClient{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}, nil
}

// ErrMissingVAPID is returned when the keys haven't been generated.
// The api surfaces 503 on /api/push/* so the FE can render an
// onboarding hint instead of a stack trace.
var ErrMissingVAPID = errors.New("push: VAPID keys not configured")

// HTTPClient is the production Sender. Wrapping webpush-go directly
// would couple us to its error shapes; this thin layer lets us reshape.
type HTTPClient struct {
	cfg  Config
	http *http.Client
}

// Send POSTs the encrypted payload to the push service. Returns the
// outcome class plus the underlying error (kept for last_error).
//
// Retryable errors include the transport status (e.g. "push: 503") so
// last_error in the DB tells the operator what failed without grepping
// logs.
func (c *HTTPClient) Send(ctx context.Context, sub Subscription, payload []byte) (Outcome, error) {
	if c == nil {
		return OutcomeRetryable, ErrMissingVAPID
	}
	wsub := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}
	opts := &webpush.Options{
		Subscriber:      c.cfg.Subject,
		VAPIDPublicKey:  c.cfg.PublicKey,
		VAPIDPrivateKey: c.cfg.PrivateKey,
		HTTPClient:      c.http,
		TTL:             c.cfg.TTLSeconds,
	}
	if opts.TTL <= 0 {
		opts.TTL = 60 * 60 * 24 // 24h — library default; reminders are time-bound
	}

	resp, err := webpush.SendNotificationWithContext(ctx, payload, wsub, opts)
	if err != nil {
		return OutcomeRetryable, fmt.Errorf("push send: %w", err)
	}
	defer resp.Body.Close()
	// Drain so HTTP keep-alive can reuse the connection. Limit to a
	// few KB — push services don't send bodies of consequence.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return OutcomeDelivered, nil
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return OutcomeGone, fmt.Errorf("push: %d %s", resp.StatusCode, resp.Status)
	default:
		return OutcomeRetryable, fmt.Errorf("push: %d %s: %s", resp.StatusCode, resp.Status, truncate(string(body), 200))
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// GenerateVAPIDKeys returns a fresh (private, public) base64url-encoded
// pair. Call it once via cmd/genvapid and persist into .env — never
// auto-generate on boot, since losing the keys silently invalidates
// every subscription on every restart. Thin re-export of the upstream
// helper so callers don't have to import webpush-go directly.
func GenerateVAPIDKeys() (privateKey, publicKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}
