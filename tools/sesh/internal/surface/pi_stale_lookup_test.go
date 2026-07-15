package surface_test

import (
	"fmt"
	"testing"

	"sesh/internal/surface"
	"sesh/internal/wire"
)

func TestNewPiDirectLookupDoesNotFalse404BehindStaleProjection(t *testing.T) {
	st, idx, live := openLiveStore(t)
	t.Cleanup(live.Close)
	putFixture(t, st, idx, wire.ToolPi, uuidPiBranched, uuidPiBranched, "pi-branched-session.jsonl", nil)
	if _, ok, err := live.Session(t.Context(), wire.ToolPi, uuidPiBranched); err != nil || !ok {
		t.Fatalf("prime Pi session: ok=%v err=%v", ok, err)
	}

	barrier := newStageBarrier(t, surface.RebuildStart)
	live.SetRebuildHook(barrier.hook)
	newID := "33333333-3333-4333-8333-333333333333"
	body := []byte(fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-07-15T13:00:00Z","cwd":"/new"}`+"\n"+
		`{"type":"message","id":"new-node","parentId":null,"message":{"role":"user","content":"new"}}`+"\n", newID))
	putBytesOwned(t, st, idx, wire.ToolPi, newID, newID, "", body, 0, "")

	_, ok, err := live.Session(t.Context(), wire.ToolPi, newID)
	<-barrier.entered
	barrier.release <- struct{}{}
	live.WaitProjectionIdle()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("new mirrored/indexed Pi session returned lookup miss while stale projection refreshed")
	}
}
