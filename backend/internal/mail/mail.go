package mail

import "context"

// Mailer is the interface every transport satisfies. Tests substitute an
// in-memory implementation; prod plugs in SMTP/Postmark.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Message carries everything a transport needs. HTML is optional but
// strongly recommended for client compat; plaintext is the canonical body.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}
