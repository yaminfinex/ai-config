package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	var (
		addr      = flag.String("addr", "127.0.0.1:8390", "listen address")
		journal   = flag.String("journal", defaultJournal(), "thread-state journal (JSONL)")
		hcomBin   = flag.String("hcom", "hcom", "hcom binary (point at the real one, not herder's capture shim)")
		hcomDir   = flag.String("hcom-dir", "", "HCOM_DIR override (empty = live bus)")
		herder    = flag.String("herder", "herder", "herder binary for roster reads")
		user      = flag.String("user", "human-yamen", "default human from-name (Tailscale-User-Login header overrides)")
		seat      = flag.String("seat", "owner", "addressable seat identity mc holds on the bus")
		noSeat    = flag.Bool("no-seat", false, "do not register/keepalive the seat (read-only bus presence)")
		fromStart = flag.Bool("from-start", false, "on a fresh journal, ingest from bus event 0 instead of from now")
		mishBin   = flag.String("mish", "mish", "mish binary for mission resolution (empty disables)")
		missions  = flag.String("missions-repo", os.Getenv("MISSIONS_REPO"), "MISSIONS_REPO for mish resolve")
	)
	flag.Parse()

	store, err := OpenStore(*journal)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	bus := &Bus{Hcom: *hcomBin, Dir: *hcomDir}

	stop := make(chan struct{})
	if !*noSeat {
		if err := bus.EnsureSeat(*seat); err != nil {
			log.Fatalf("seat @%s: %v", *seat, err)
		}
		go bus.SeatKeepalive(*seat, stop)
	}

	// A fresh journal starts ingesting from NOW: replaying the bus's whole
	// history would open a desk thread for every past @mention of the seat.
	if store.Cursor() == 0 && !*fromStart {
		if evs, err := bus.EventsSince(0, 1); err != nil {
			log.Fatalf("cursor init: %v", err)
		} else if len(evs) > 0 {
			if err := store.SetCursor(evs[len(evs)-1].ID); err != nil {
				log.Fatalf("cursor init: %v", err)
			}
			log.Printf("fresh journal: ingest starts at bus event %d", store.Cursor())
		}
	}

	ing := NewIngestor(store, bus, *user, *seat)
	go ing.Run(stop)

	web := NewWeb(store, bus, ing, *user, *seat, *herder, newMissionResolver(*mishBin, *missions))
	busDesc := "LIVE bus"
	if *hcomDir != "" {
		busDesc = "lab bus at " + *hcomDir
	}
	log.Printf("mc: %s · seat @%s · user %s · journal %s · %s", *addr, *seat, *user, *journal, busDesc)
	if err := http.ListenAndServe(*addr, web.Routes()); err != nil {
		log.Fatal(err)
	}
}

func defaultJournal() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "mc-journal.jsonl"
	}
	return filepath.Join(home, ".mc", "journal.jsonl")
}
