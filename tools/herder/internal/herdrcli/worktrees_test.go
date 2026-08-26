package herdrcli

import (
	"fmt"
	"testing"
)

func TestWorktreeParentsUsesOnlyExplicitOpenLinkedRelationship(t *testing.T) {
	workspaces := []Workspace{
		{WorkspaceID: "root", Worktree: &WorkspaceWorktree{}},
		{WorkspaceID: "linked", Worktree: &WorkspaceWorktree{IsLinkedWorktree: true}},
		{WorkspaceID: "untracked"},
	}
	calls := 0
	parents, err := worktreeParents(workspaces, func(id string) ([]byte, error) {
		calls++
		if id != "linked" {
			t.Fatalf("queried non-linked workspace %q", id)
		}
		return []byte(`{"result":{"source":{"source_workspace_id":"root"},"worktrees":[{"is_linked_worktree":true,"open_workspace_id":"linked"}]}}`), nil
	})
	if err != nil || calls != 1 || parents["linked"] != "root" {
		t.Fatalf("parents=%#v calls=%d err=%v", parents, calls, err)
	}
}

func TestWorktreeParentsDoesNotGuessWhenHerdrDoesNotNameOpenWorkspace(t *testing.T) {
	workspaces := []Workspace{{WorkspaceID: "linked", Worktree: &WorkspaceWorktree{IsLinkedWorktree: true}}}
	parents, err := worktreeParents(workspaces, func(string) ([]byte, error) {
		return []byte(`{"result":{"source":{"source_workspace_id":"root"},"worktrees":[{"is_linked_worktree":true,"path":"/same-looking/path"}]}}`), nil
	})
	if err != nil || len(parents) != 0 {
		t.Fatalf("parents=%#v err=%v", parents, err)
	}
	_, err = worktreeParents(workspaces, func(string) ([]byte, error) { return nil, fmt.Errorf("offline") })
	if err == nil {
		t.Fatal("worktree substrate failure was hidden")
	}
}
