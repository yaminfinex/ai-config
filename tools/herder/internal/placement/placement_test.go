package placement

import "testing"

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		flags   Flags
		want    Decision
		wantErr bool
	}{
		{name: "default fresh tab", flags: Flags{}, want: Decision{Split: "right", NewTab: true}},
		{name: "explicit right split", flags: Flags{Split: "right", SplitExplicit: true}, want: Decision{Split: "right"}},
		{name: "explicit down split", flags: Flags{Split: "down", SplitExplicit: true}, want: Decision{Split: "down"}},
		{name: "explicit fresh tab", flags: Flags{NewTab: true}, want: Decision{Split: "right", NewTab: true}},
		{name: "existing tab", flags: Flags{ExistingTab: "tab_4"}, want: Decision{Split: "right"}},
		{name: "worktree native placement", flags: Flags{Worktree: true}, want: Decision{Split: "right"}},
		{name: "worktree explicit split", flags: Flags{Worktree: true, Split: "down", SplitExplicit: true}, want: Decision{Split: "down"}},
		{name: "fresh tab conflicts with split", flags: Flags{NewTab: true, SplitExplicit: true}, wantErr: true},
		{name: "fresh tab conflicts with existing tab", flags: Flags{NewTab: true, ExistingTab: "tab_4"}, wantErr: true},
		{name: "invalid split", flags: Flags{Split: "left", SplitExplicit: true}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.flags)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
