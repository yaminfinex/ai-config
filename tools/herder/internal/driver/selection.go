package driver

import (
	"os"
	"os/exec"

	"ai-config/tools/herder/internal/registry"
)

type Transport string

const (
	TransportHerdr Transport = "herdr"
	TransportHcom  Transport = "hcom"
)

type Selection struct {
	Transport Transport
	Herdr     *Herdr
	Hcom      *Hcom
}

func NewSelection() *Selection {
	return &Selection{
		Herdr: &Herdr{},
		Hcom:  &Hcom{},
	}
}

func (s *Selection) Select(target string) Transport {
	bus := os.Getenv("HERDER_BUS")
	switch bus {
	case "herdr":
		return TransportHerdr
	case "hcom":
		return TransportHcom
	}

	rec, found := registryRecordFor(registry.DefaultPath(), target)
	if found && rec.HcomName != "" && rec.HcomName != "null" {
		if _, err := exec.LookPath("hcom"); err == nil {
			return TransportHcom
		}
	}
	return TransportHerdr
}

func registryRecordFor(path, target string) (registry.Record, bool) {
	recs, err := registry.Load(path)
	if err != nil {
		return registry.Record{}, false
	}
	rec := registry.Resolve(recs, target)
	if rec == nil {
		return registry.Record{}, false
	}
	return *rec, true
}
