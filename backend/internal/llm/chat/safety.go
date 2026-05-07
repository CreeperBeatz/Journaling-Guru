package chat

import (
	"regexp"
	"strings"
)

// crisisPattern is a deliberately permissive regex tuned for high recall
// at the cost of false-positives. False-positives surface a (dismissible)
// resources card and pause one turn — annoying but safe. False-negatives
// can mean the user types about self-harm and gets a vanilla journal-bot
// reply, which is the failure mode we cannot tolerate.
//
// We escape the literals at startup once. The regex is lower-cased for
// matching; callers don't need to lowercase first (IsCrisis does it).
//
// Phrases drawn from Columbia-SSRS-flavored screening + standard
// chatbot-safety post-mortems. Not a clinical tool — just a first-pass
// gate before the prompt-layer rules also kick in.
var crisisPattern = regexp.MustCompile(`(?i)\b(` +
	// suicidal ideation
	`suicid(e|al)|kill (myself|me)|end (it|my life)|take my (own )?life|don'?t want to live|` +
	`want to die|wanna die|wishing.*dead|better off dead|no reason to live|` +
	// self-harm
	`self[\s-]?harm|hurt myself|cut(ting)? myself|self[\s-]?injur|` +
	// active crisis indicators
	`overdos(e|ing)|jump off|hang myself|shoot myself|` +
	// abuse / acute danger
	`he('?s| is) (hitting|hurting|abusing) me|she('?s| is) (hitting|hurting|abusing) me|` +
	`raped|assaulted me`+
	`)\b`)

// IsCrisis reports whether `text` matches a crisis indicator. Trims
// whitespace and lowercases internally — caller passes raw user input.
//
// This is defensive screening, not detection. It runs before the LLM
// sees the message; on hit the streaming handler short-circuits and the
// UI surfaces a static crisis card with 988 / Crisis Text Line links.
func IsCrisis(text string) bool {
	return crisisPattern.MatchString(strings.ToLower(text))
}
