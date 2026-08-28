// Package fileresolver ranks file candidates with fzf's path matching scheme
// and provides mention normalization and root-containment primitives.
//
// The fzf algo.Init function mutates package-global scoring configuration.
// This package initializes the path scheme exactly once during package init
// and must never re-initialize fzf with a different scheme.
package fileresolver
