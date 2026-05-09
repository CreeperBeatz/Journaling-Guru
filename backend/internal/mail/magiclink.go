package mail

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

// MagicLinkData is the input to the email template. KeepDuration human-
// readable so the user knows when the link expires without doing math.
type MagicLinkData struct {
	VerifyURL    string
	ExpiresLabel string // e.g. "15 minutes"
}

var magicLinkText = `Sign in to Journaling Guru

Click the link below to finish signing in. The link is good for {{.ExpiresLabel}}
and can only be used once.

  {{.VerifyURL}}

If you didn't ask for this, you can ignore this email — nothing changes
unless you click the link.
`

var magicLinkHTML = `<!doctype html>
<html>
  <body style="font-family: system-ui, sans-serif; line-height: 1.5; color: #111;">
    <h2 style="margin-bottom: 0.5em;">Sign in to Journaling Guru</h2>
    <p>Click the button below to finish signing in. The link is good for
       <strong>{{.ExpiresLabel}}</strong> and can only be used once.</p>
    <p>
      <a href="{{.VerifyURL}}"
         style="display:inline-block;background:#111;color:#fff;
                padding:10px 18px;border-radius:6px;text-decoration:none;">
        Sign in
      </a>
    </p>
    <p style="font-size: 0.9em; color: #555;">Or paste this URL into your browser:<br>
       <span style="word-break: break-all;">{{.VerifyURL}}</span></p>
    <p style="font-size: 0.9em; color: #555;">If you didn't ask for this, you can
       ignore this email.</p>
  </body>
</html>`

var (
	magicLinkTextTmpl = template.Must(template.New("magic-text").Parse(magicLinkText))
	magicLinkHTMLTmpl = template.Must(template.New("magic-html").Parse(magicLinkHTML))
)

// BuildMagicLinkMessage composes the message body for a sign-in email.
// publicBaseURL is the frontend origin (e.g. https://app.journai.com); we
// append /auth/verify?token=... so the link opens the SPA route that POSTs
// to the backend.
func BuildMagicLinkMessage(to, publicBaseURL, rawToken, expiresLabel string) (Message, error) {
	verifyURL := fmt.Sprintf("%s/auth/verify?token=%s",
		strings.TrimRight(publicBaseURL, "/"),
		url.QueryEscape(rawToken),
	)
	data := MagicLinkData{VerifyURL: verifyURL, ExpiresLabel: expiresLabel}

	var textBuf, htmlBuf bytes.Buffer
	if err := magicLinkTextTmpl.Execute(&textBuf, data); err != nil {
		return Message{}, err
	}
	if err := magicLinkHTMLTmpl.Execute(&htmlBuf, data); err != nil {
		return Message{}, err
	}
	return Message{
		To:      to,
		Subject: "Your Journaling Guru sign-in link",
		Text:    textBuf.String(),
		HTML:    htmlBuf.String(),
	}, nil
}
