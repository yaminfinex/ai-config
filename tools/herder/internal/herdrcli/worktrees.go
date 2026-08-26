package herdrcli

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type worktreeListEnvelope struct {
	Result struct {
		Source struct {
			WorkspaceID string `json:"source_workspace_id"`
		} `json:"source"`
		Worktrees []struct {
			Linked      bool   `json:"is_linked_worktree"`
			WorkspaceID string `json:"open_workspace_id"`
		} `json:"worktrees"`
	} `json:"result"`
}

// WorktreeParents asks herdr for workspace linkage and returns only explicit
// open linked-worktree relationships. It never infers a parent from paths.
func WorktreeParents(workspaces []Workspace) (map[string]string, error) {
	return worktreeParents(workspaces, func(workspaceID string) ([]byte, error) {
		return exec.Command("herdr", "worktree", "list", "--workspace", workspaceID).Output()
	})
}

func worktreeParents(workspaces []Workspace, list func(string) ([]byte, error)) (map[string]string, error) {
	parents := make(map[string]string)
	for _, workspace := range workspaces {
		if workspace.Worktree == nil || !workspace.Worktree.IsLinkedWorktree {
			continue
		}
		out, err := list(workspace.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("herdr worktree list --workspace %s failed: %w", workspace.WorkspaceID, err)
		}
		var envelope worktreeListEnvelope
		if err := json.Unmarshal(out, &envelope); err != nil {
			return nil, fmt.Errorf("decode herdr worktree list for %s: %w", workspace.WorkspaceID, err)
		}
		for _, worktree := range envelope.Result.Worktrees {
			if worktree.WorkspaceID == workspace.WorkspaceID && worktree.Linked && envelope.Result.Source.WorkspaceID != "" && envelope.Result.Source.WorkspaceID != workspace.WorkspaceID {
				parents[workspace.WorkspaceID] = envelope.Result.Source.WorkspaceID
				break
			}
		}
	}
	return parents, nil
}
