package servecmd

import (
	"errors"
	"maps"
	"testing"

	"ai-config/tools/herder/internal/herdrcli"
)

func TestCachedWorktreeParents(t *testing.T) {
	linked := func(path, root string) herdrcli.Workspace {
		return herdrcli.Workspace{WorkspaceID: "linked", Worktree: &herdrcli.WorkspaceWorktree{
			IsLinkedWorktree: true, CheckoutPath: path, RepoRoot: root,
		}}
	}
	base := []herdrcli.Workspace{{WorkspaceID: "source"}, linked("/checkout", "/repo")}
	tests := []struct {
		name       string
		second     []herdrcli.Workspace
		firstError bool
		wantCalls  int
	}{
		{"same set in another order", []herdrcli.Workspace{linked("/checkout", "/repo"), {WorkspaceID: "source"}}, false, 1},
		{"workspace added", append(append([]herdrcli.Workspace(nil), base...), herdrcli.Workspace{WorkspaceID: "other"}), false, 2},
		{"workspace removed", []herdrcli.Workspace{linked("/checkout", "/repo")}, false, 2},
		{"checkout changed", []herdrcli.Workspace{{WorkspaceID: "source"}, linked("/moved", "/repo")}, false, 2},
		{"repo root changed", []herdrcli.Workspace{{WorkspaceID: "source"}, linked("/checkout", "/other")}, false, 2},
		{"error retries", base, true, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			lookup := cachedWorktreeParents(func([]herdrcli.Workspace) (map[string]string, error) {
				calls++
				if tt.firstError && calls == 1 {
					return nil, errors.New("temporary failure")
				}
				return map[string]string{"linked": "source"}, nil
			})
			first, err := lookup(base)
			if tt.firstError {
				if err == nil {
					t.Fatal("first lookup succeeded, want error")
				}
			} else if err != nil {
				t.Fatalf("first lookup: %v", err)
			}
			second, err := lookup(tt.second)
			if err != nil {
				t.Fatalf("second lookup: %v", err)
			}
			if calls != tt.wantCalls {
				t.Errorf("inner called %d times, want %d", calls, tt.wantCalls)
			}
			want := map[string]string{"linked": "source"}
			if !maps.Equal(second, want) || (!tt.firstError && !maps.Equal(first, second)) {
				t.Errorf("lookup results: first=%v second=%v, want %v", first, second, want)
			}
		})
	}
}

func TestCachedWorktreeParentsReturnsIndependentMaps(t *testing.T) {
	calls := 0
	lookup := cachedWorktreeParents(func([]herdrcli.Workspace) (map[string]string, error) {
		calls++
		return map[string]string{"linked": "source"}, nil
	})
	first, err := lookup(nil)
	if err != nil {
		t.Fatal(err)
	}
	first["linked"] = "changed"
	second, err := lookup(nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || second["linked"] != "source" {
		t.Errorf("cache was mutated: calls=%d, second=%v", calls, second)
	}
}
