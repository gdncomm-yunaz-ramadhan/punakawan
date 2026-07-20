// Package prompts embeds Punakawan's role prompt templates
// (prompts/<role>/prompt.md), per punakawan-go-typescript-detailed-plan.md
// §28.4 ("Prompts... sourced from prompts/<role>/"). Embedding rather than
// reading from disk at runtime means `punakawan mcp serve` works regardless
// of the caller's working directory.
package prompts

import "embed"

//go:embed semar/prompt.md gareng/prompt.md petruk/prompt.md bagong/prompt.md
var FS embed.FS
