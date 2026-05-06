package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPMailer talks to a vanilla SMTP server over either STARTTLS (587) or
// implicit TLS (465/2465). Auth is skipped when User is empty (typical for
// local Mailhog). Production transports (Postmark, Resend) plug in by env
// alone — no code change.
type SMTPMailer struct {
	Host string
	Port int
	User string
	Pass string
	From string // RFC 5322 sender, e.g. `JournAI <hello@journai.local>`
}

func NewSMTPMailer(host string, port int, user, pass, from string) *SMTPMailer {
	return &SMTPMailer{Host: host, Port: port, User: user, Pass: pass, From: from}
}

const (
	dialTimeout  = 10 * time.Second
	totalTimeout = 15 * time.Second
)

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	body := buildMIME(m.From, msg)

	// net/smtp doesn't accept a context; we run in a goroutine so the caller
	// can cancel via ctx, and fall back to a hard wall-clock timeout in case
	// the SMTP server hangs mid-handshake.
	done := make(chan error, 1)
	go func() { done <- m.send(msg.To, body) }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	case <-time.After(totalTimeout):
		return fmt.Errorf("smtp: send timed out after %s", totalTimeout)
	}
}

// send opens a connection, optionally upgrades to TLS, authenticates, and
// dispatches one message. Implicit-TLS ports (465, 2465) get tls.Dial up
// front; everything else dials plaintext and tries STARTTLS if offered.
func (m *SMTPMailer) send(to string, body []byte) error {
	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	tlsConfig := &tls.Config{ServerName: m.Host}

	var conn net.Conn
	var err error
	if isImplicitTLSPort(m.Port) {
		dialer := &net.Dialer{Timeout: dialTimeout}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", addr, dialTimeout)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	// Bound every subsequent read/write so a half-open server can't park us.
	_ = conn.SetDeadline(time.Now().Add(totalTimeout))

	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if err := c.Hello("localhost"); err != nil {
		return fmt.Errorf("smtp EHLO: %w", err)
	}

	// On STARTTLS ports, upgrade if the server advertises it. Servers we
	// care about always do; we still skip silently for Mailhog (no TLS).
	if !isImplicitTLSPort(m.Port) {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("smtp STARTTLS: %w", err)
			}
		}
	}

	if m.User != "" {
		auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := c.Mail(fromAddr(m.From)); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close body: %w", err)
	}
	return c.Quit()
}

// isImplicitTLSPort returns true for the canonical (465) and Resend's
// alternate (2465) implicit-TLS submission ports.
func isImplicitTLSPort(p int) bool {
	return p == 465 || p == 2465
}

// buildMIME emits a multipart/alternative message when HTML is provided so
// clients that prefer rich rendering still get it without dropping plaintext.
func buildMIME(from string, m Message) []byte {
	var b strings.Builder
	headers := func(k, v string) {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	headers("From", from)
	headers("To", m.To)
	headers("Subject", m.Subject)
	headers("MIME-Version", "1.0")

	if m.HTML == "" {
		headers("Content-Type", `text/plain; charset="utf-8"`)
		b.WriteString("\r\n")
		b.WriteString(m.Text)
		return []byte(b.String())
	}

	const boundary = "journai-magic-link-mime"
	headers("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString(`Content-Type: text/plain; charset="utf-8"` + "\r\n\r\n")
	b.WriteString(m.Text)
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString(`Content-Type: text/html; charset="utf-8"` + "\r\n\r\n")
	b.WriteString(m.HTML)
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// fromAddr extracts the bare email out of `Name <email@host>` for the SMTP
// envelope sender. Falls back to the input as-is if there's no `<...>`.
func fromAddr(from string) string {
	if i := strings.Index(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j >= 0 {
			return from[i+1 : i+j]
		}
	}
	return from
}
