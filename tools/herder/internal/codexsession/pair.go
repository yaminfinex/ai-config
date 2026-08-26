package codexsession

import "ai-config/tools/herder/internal/claudesession"

// Pair returns the shared pure consumer-side pairing view.
func Pair(entries []Entry) []PairedEntry { return claudesession.Pair(entries) }
