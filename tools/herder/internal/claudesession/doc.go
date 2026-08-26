// Package claudesession resolves and reads Claude Code session JSONL files.
//
// By owner ruling 2026-08-25, this package amends hcomtranscript's former
// doctrine that herder never opens transcript files itself: the Claude
// backend is resolved and parsed here. hcomtranscript remains unchanged for
// non-Claude fallbacks.
package claudesession
