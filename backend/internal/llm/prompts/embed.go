// Package prompts holds the four go:embed summary prompt templates. They
// are loaded once at startup and rendered with text/template by the
// summary worker.
package prompts

import "embed"

//go:embed *.tmpl
var FS embed.FS
