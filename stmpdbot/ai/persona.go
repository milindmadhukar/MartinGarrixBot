package ai

import _ "embed"

// persona.md is the static "voice guide" distilled offline from a random
// sample of this server's own message history by
// scripts/analyze-fandom-voice. It is folded into the system prompt once at
// startup rather than re-derived per message, so the running bot never pays
// an LLM call for tone -- only for the actual conversation.
//
//go:embed persona.md
var persona string
