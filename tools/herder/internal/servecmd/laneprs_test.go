package servecmd

import (
	"context"
	"os"
	"reflect"
	"testing"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/githubprs"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/repoctx"
)

func laneFixture(t *testing.T) (*githubprs.Poller, *int) {
	t.Helper()
	fixture, err := os.ReadFile("../githubprs/testdata/repository.json")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	return githubprs.New(func(context.Context, string, map[string]any) ([]byte, error) {
		requests++
		return fixture, nil
	}, t.Logf), &requests
}

func laneBoard() fleetview.Board {
	return fleetview.Board{Lanes: []fleetview.Lane{
		{ID: "lane-a", Name: "a", Lead: "lead-a", Members: []string{"lead-a", "impl-a"}, Repos: []string{}, Branches: []string{}, PRs: []githubprs.PR{}},
		{ID: "lane-b", Name: "b", Lead: "lead-b", Members: []string{"lead-b"}, Repos: []string{}, Branches: []string{}, PRs: []githubprs.PR{}},
	}}
}

var laneRoster = []hcomidentity.Row{{Name: "lead-a", Directory: "/w/a"}, {Name: "impl-a", Directory: "/w/a2"}, {Name: "lead-b", Directory: "/w/b"}}

func laneGit(cwd string) *repoctx.Git {
	return map[string]*repoctx.Git{
		"/w/a":  {Branch: "main", RemoteURL: "git@github.com:Acme/App.git"},
		"/w/a2": {Branch: "branch-314", RemoteURL: "https://github.com/acme/app"},
		"/w/b":  {Branch: "b-work", RemoteURL: "https://gitlab.com/acme/app"},
	}[cwd]
}

func laneNumbers(lane fleetview.Lane) []int {
	out := []int{}
	for _, pr := range lane.PRs {
		out = append(out, pr.Number)
	}
	return out
}

func TestFoldBoardLanesReposBranchesAndPRs(t *testing.T) {
	poller, requests := laneFixture(t)
	proj := &agentstore.Projection{Lanes: map[string]*agentstore.LaneView{
		"lane-a": {ID: "lane-a", PRs: []string{"acme/app#7", "acme/app#309"}},
		"lane-b": {ID: "lane-b", Repos: []string{"acme/app"}},
	}}

	// Before the first poll a link is a bare row and markers find nothing.
	board := laneBoard()
	foldBoardLanes(&board, laneRoster, proj, laneGit, poller)
	a, b := board.Lanes[0], board.Lanes[1]
	if !reflect.DeepEqual(a.Repos, []string{"acme/app"}) || !reflect.DeepEqual(a.Branches, []string{"branch-314", "main"}) {
		t.Fatalf("lane a repos %v branches %v", a.Repos, a.Branches)
	}
	if !reflect.DeepEqual(b.Repos, []string{"acme/app"}) || !reflect.DeepEqual(b.Branches, []string{"b-work"}) {
		t.Fatalf("lane b repos %v branches %v: a non-GitHub origin adds no repo, the explicit one does", b.Repos, b.Branches)
	}
	if got := laneNumbers(a); !reflect.DeepEqual(got, []int{7, 309}) || a.PRs[0].State != "" || !a.PRs[0].Linked || a.PRs[0].URL != "https://github.com/acme/app/pull/7" {
		t.Fatalf("unpolled lane a PRs = %+v", a.PRs)
	}

	poller.Poll(context.Background())
	if *requests != 1 {
		t.Fatalf("requests = %d, want one for the one repo two lanes share", *requests)
	}
	board = laneBoard()
	foldBoardLanes(&board, laneRoster, proj, laneGit, poller)
	a, b = board.Lanes[0], board.Lanes[1]
	if got := laneNumbers(a); !reflect.DeepEqual(got, []int{7, 309, 312, 314}) {
		t.Fatalf("lane a PRs = %v, want links then markers", got)
	}
	if a.PRs[0].State != "MERGED" || !a.PRs[1].Draft || a.PRs[2].Linked {
		t.Errorf("lane a rows = %+v", a.PRs)
	}
	if a.PRs[3].Holder != "impl-a" || a.PRs[2].Holder != "" {
		t.Errorf("holder hint: %+v", a.PRs)
	}
	if got := laneNumbers(b); !reflect.DeepEqual(got, []int{312}) {
		t.Errorf("lane b PRs = %v, want the PR marked for both lanes", got)
	}
	if a.PRNote != "" {
		t.Errorf("PRNote = %q with a token", a.PRNote)
	}

	// Unlink: the row goes on the next board, no poll needed.
	proj.Lanes["lane-a"].PRs = []string{"acme/app#7"}
	board = laneBoard()
	foldBoardLanes(&board, laneRoster, proj, laneGit, poller)
	if got := laneNumbers(board.Lanes[0]); !reflect.DeepEqual(got, []int{7, 312, 314}) {
		t.Errorf("after unlink lane a PRs = %v", got)
	}
}

func TestFoldBoardLanesWithoutToken(t *testing.T) {
	proj := &agentstore.Projection{Lanes: map[string]*agentstore.LaneView{"lane-a": {ID: "lane-a", PRs: []string{"acme/app#7"}}, "lane-b": {ID: "lane-b"}}}
	board := laneBoard()
	noGit := func(string) *repoctx.Git { return nil }
	foldBoardLanes(&board, laneRoster, proj, noGit, githubprs.New(nil, t.Logf))
	if board.Lanes[0].PRNote != githubprs.NoToken {
		t.Errorf("lane a note = %q", board.Lanes[0].PRNote)
	}
	if board.Lanes[1].PRNote != "" {
		t.Errorf("a lane with no repo and no link says %q", board.Lanes[1].PRNote)
	}
}
