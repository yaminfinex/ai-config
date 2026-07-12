package resolve

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestResolutionOrderFlagBeatsCWDAndMarker(t *testing.T) {
	fsys := newMemFS()
	fsys.addDir("/repo/missions/flag")
	fsys.addDir("/repo/missions/cwd")
	fsys.addDir("/repo/missions/marker")
	fsys.addFile("/repo/missions/cwd/mission.md", "mission: cwd\n")
	fsys.addFile("/repo/missions/cwd/backlog/tasks/.mission", "marker\n")

	got, err := Resolve(Options{
		MissionFlag: "flag",
		CWD:         "/repo/missions/cwd/backlog/tasks",
		Env:         env("/repo"),
		FS:          fsys,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	assertResult(t, got, "flag", "/repo/missions/flag", SourceFlag)
}

func TestResolutionOrderCWDBeatsMarker(t *testing.T) {
	fsys := newMemFS()
	fsys.addDir("/repo/missions/cwd")
	fsys.addDir("/repo/missions/marker")
	fsys.addFile("/repo/missions/cwd/mission.md", "mission: cwd\n")
	fsys.addFile("/repo/missions/cwd/.mission", "marker\n")

	got, err := Resolve(Options{
		CWD: "/repo/missions/cwd/backlog/tasks",
		Env: env("/repo"),
		FS:  fsys,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	assertResult(t, got, "cwd", "/repo/missions/cwd", SourceCWD)
}

func TestResolutionOrderSingleMarkerResolves(t *testing.T) {
	fsys := newMemFS()
	fsys.addDir("/repo/missions/marker")
	fsys.addFile("/work/.mission", "marker\nreserved: ignored\n")

	got, err := Resolve(Options{
		CWD: "/work/sub/leaf",
		Env: env("/repo"),
		FS:  fsys,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	assertResult(t, got, "marker", "/repo/missions/marker", SourceMarker)
	if got.MarkerPath != filepath.Clean("/work/.mission") {
		t.Fatalf("MarkerPath = %q, want /work/.mission", got.MarkerPath)
	}
}

func TestMarkerLineOneIsTrimmedBeforeResolving(t *testing.T) {
	fsys := newMemFS()
	fsys.addDir("/repo/missions/marker")
	fsys.addFile("/work/.mission", " \tmarker \r\nreserved: ignored\n")

	got, err := Resolve(Options{
		CWD: "/work/sub/leaf",
		Env: env("/repo/"),
		FS:  fsys,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	assertResult(t, got, "marker", "/repo/missions/marker", SourceMarker)
}

func TestMultipleMarkersRefuseAndNameBothPaths(t *testing.T) {
	fsys := newMemFS()
	fsys.addFile("/work/.mission", "outer\n")
	fsys.addFile("/work/sub/.mission", "inner\n")

	_, err := Resolve(Options{
		CWD: "/work/sub/leaf",
		Env: env("/repo"),
		FS:  fsys,
	})
	refusal := assertRefusal(t, err, RefusalMultipleMarkers)
	wantPaths := []string{filepath.Clean("/work/sub/.mission"), filepath.Clean("/work/.mission")}
	if !slices.Equal(refusal.Paths, wantPaths) {
		t.Fatalf("paths = %v, want %v", refusal.Paths, wantPaths)
	}
	for _, path := range wantPaths {
		if !strings.Contains(refusal.Reason, path) {
			t.Fatalf("reason missing %q: %s", path, refusal.Reason)
		}
	}
}

func TestInvalidFlagSlugRefusesBeforePathResolution(t *testing.T) {
	for _, slug := range []string{"", "../../etc/secret", "Upper", "a b", "a--b", "x-"} {
		t.Run(slug, func(t *testing.T) {
			fsys := newMemFS()
			fsys.addDir("/etc/secret")
			fsys.addDir("/repo/missions")

			_, err := Resolve(Options{
				MissionFlagSet: true,
				MissionFlag:    slug,
				CWD:            "/work",
				Env:            env("/repo"),
				FS:             fsys,
			})
			refusal := assertRefusal(t, err, RefusalInvalidSlug)
			if refusal.Slug != slug || !strings.Contains(refusal.Reason, strconv.Quote(slug)) {
				t.Fatalf("refusal = %#v, want offending slug named", refusal)
			}
		})
	}
}

func TestInvalidMarkerSlugRefusesBeforePathResolution(t *testing.T) {
	for _, slug := range []string{"", "../../etc/secret", "Upper", "a b", "a--b", "x-"} {
		t.Run(slug, func(t *testing.T) {
			fsys := newMemFS()
			fsys.addDir("/etc/secret")
			fsys.addDir("/repo/missions")
			fsys.addFile("/work/.mission", slug+"\n")

			_, err := Resolve(Options{
				CWD: "/work/sub",
				Env: env("/repo"),
				FS:  fsys,
			})
			refusal := assertRefusal(t, err, RefusalInvalidSlug)
			if refusal.Slug != slug || !strings.Contains(refusal.Reason, strconv.Quote(slug)) {
				t.Fatalf("refusal = %#v, want offending slug named", refusal)
			}
		})
	}
}

func TestInvalidCWDSlugRefusesBeforeReturningContext(t *testing.T) {
	for _, slug := range []string{"a--b", "x-"} {
		t.Run(slug, func(t *testing.T) {
			fsys := newMemFS()
			missionDir := filepath.Join("/repo/missions", slug)
			fsys.addFile(filepath.Join(missionDir, "mission.md"), "mission: "+slug+"\n")

			_, err := Resolve(Options{
				CWD: filepath.Join(missionDir, "backlog/tasks"),
				Env: env("/repo"),
				FS:  fsys,
			})
			refusal := assertRefusal(t, err, RefusalInvalidSlug)
			if refusal.Slug != slug || !strings.Contains(refusal.Reason, strconv.Quote(slug)) {
				t.Fatalf("refusal = %#v, want offending slug named", refusal)
			}
		})
	}
}

func TestFlagNamingMissingMissionRefuses(t *testing.T) {
	fsys := newMemFS()
	fsys.addDir("/repo/missions/other")

	_, err := Resolve(Options{
		MissionFlag: "missing",
		CWD:         "/work",
		Env:         env("/repo"),
		FS:          fsys,
	})
	refusal := assertRefusal(t, err, RefusalMissionNotFound)
	if refusal.Slug != "missing" || !strings.Contains(refusal.Reason, "mission missing not found") {
		t.Fatalf("refusal = %#v", refusal)
	}
}

func TestNoContextRefusesWithAllMechanismsInGuidance(t *testing.T) {
	_, err := Resolve(Options{
		CWD: "/work/sub",
		Env: env("/repo"),
		FS:  newMemFS(),
	})
	refusal := assertRefusal(t, err, RefusalNoContext)
	for _, want := range []string{"--mission", "missions/<slug>", ".mission"} {
		if !strings.Contains(refusal.Remedy, want) {
			t.Fatalf("remedy missing %q: %s", want, refusal.Remedy)
		}
	}
}

func TestMarkerPointingAtMissingMissionRefusesWithSlug(t *testing.T) {
	fsys := newMemFS()
	fsys.addFile("/work/.mission", "ghost\n")

	_, err := Resolve(Options{
		CWD: "/work/sub",
		Env: env("/repo"),
		FS:  fsys,
	})
	refusal := assertRefusal(t, err, RefusalMarkerMissing)
	if refusal.Slug != "ghost" || !strings.Contains(refusal.Reason, "marker points at missing mission ghost") {
		t.Fatalf("refusal = %#v", refusal)
	}
}

func TestMissionsRepoUnsetRefusesWhenNeeded(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		_, err := Resolve(Options{
			MissionFlag: "alpha",
			CWD:         "/work",
			Env:         env(""),
			FS:          newMemFS(),
		})
		refusal := assertRefusal(t, err, RefusalRepoUnset)
		if !strings.Contains(refusal.Remedy, "MISSIONS_REPO") {
			t.Fatalf("remedy missing MISSIONS_REPO: %s", refusal.Remedy)
		}
	})

	t.Run("marker", func(t *testing.T) {
		fsys := newMemFS()
		fsys.addFile("/work/.mission", "alpha\n")
		_, err := Resolve(Options{
			CWD: "/work/sub",
			Env: env(""),
			FS:  fsys,
		})
		assertRefusal(t, err, RefusalRepoUnset)
	})
}

func TestCWDInsideMissionDirDoesNotNeedMissionsRepo(t *testing.T) {
	for _, cwd := range []string{"/repo/missions/alpha", "/repo/missions/alpha/backlog/tasks"} {
		t.Run(cwd, func(t *testing.T) {
			fsys := newMemFS()
			fsys.addFile("/repo/missions/alpha/mission.md", "mission: alpha\n")

			got, err := Resolve(Options{
				CWD: cwd,
				Env: env(""),
				FS:  fsys,
			})
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			assertResult(t, got, "alpha", "/repo/missions/alpha", SourceCWD)
		})
	}
}

func TestTrailingSlashMissionsRepoResolvesCleanMissionPath(t *testing.T) {
	fsys := newMemFS()
	fsys.addDir("/repo/missions/alpha")
	fsys.addFile("/work/.mission", "alpha\n")

	got, err := Resolve(Options{
		CWD: "/work/sub",
		Env: env("/repo/"),
		FS:  fsys,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	assertResult(t, got, "alpha", "/repo/missions/alpha", SourceMarker)
}

func TestEmptyCWDRefusesWithoutAmbientFallback(t *testing.T) {
	_, err := Resolve(Options{
		Env: env("/repo"),
		FS:  newMemFS(),
	})
	assertRefusal(t, err, RefusalNoContext)
}

func TestMarkerReadErrorIsTypedRefusal(t *testing.T) {
	fsys := newMemFS()
	fsys.addReadError("/work/.mission")

	_, err := Resolve(Options{
		CWD: "/work/sub",
		Env: env("/repo"),
		FS:  fsys,
	})
	refusal := assertRefusal(t, err, RefusalMarkerUnreadable)
	if len(refusal.Paths) != 1 || refusal.Paths[0] != filepath.Clean("/work/.mission") {
		t.Fatalf("paths = %v, want marker path", refusal.Paths)
	}
}

func TestMissionMDOutsideMissionsParentChainDoesNotResolve(t *testing.T) {
	fsys := newMemFS()
	fsys.addFile("/repo/not-missions/alpha/mission.md", "mission: alpha\n")

	_, err := Resolve(Options{
		CWD: "/repo/not-missions/alpha/backlog/tasks",
		Env: env("/repo"),
		FS:  fsys,
	})
	assertRefusal(t, err, RefusalNoContext)
}

func assertResult(t *testing.T, got Result, wantSlug, wantDir string, wantSource Source) {
	t.Helper()
	if got.Slug != wantSlug || got.MissionDir != filepath.Clean(wantDir) || got.Source != wantSource {
		t.Fatalf("result = %#v, want slug=%q dir=%q source=%q", got, wantSlug, filepath.Clean(wantDir), wantSource)
	}
}

func assertRefusal(t *testing.T, err error, kind RefusalKind) *Refusal {
	t.Helper()
	if err == nil {
		t.Fatalf("Resolve returned nil error, want %s", kind)
	}
	var refusal *Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %T %v, want *Refusal", err, err)
	}
	if refusal.Kind != kind {
		t.Fatalf("kind = %s, want %s; refusal=%#v", refusal.Kind, kind, refusal)
	}
	return refusal
}

func env(repo string) EnvLookup {
	return func(key string) string {
		if key == missionsRepoEnv {
			return repo
		}
		return ""
	}
}

type memFS struct {
	files      map[string]string
	readErrors map[string]error
	dirs       map[string]bool
}

func newMemFS() *memFS {
	fsys := &memFS{
		files:      map[string]string{},
		readErrors: map[string]error{},
		dirs:       map[string]bool{},
	}
	fsys.addDir("/")
	return fsys
}

func (m *memFS) addFile(path, content string) {
	clean := filepath.Clean(path)
	m.files[clean] = content
	m.addParents(clean)
}

func (m *memFS) addReadError(path string) {
	clean := filepath.Clean(path)
	m.files[clean] = ""
	m.readErrors[clean] = fs.ErrPermission
	m.addParents(clean)
}

func (m *memFS) addDir(path string) {
	clean := filepath.Clean(path)
	m.dirs[clean] = true
	m.addParents(clean)
}

func (m *memFS) addParents(path string) {
	for dir := filepath.Dir(path); dir != path; dir = filepath.Dir(dir) {
		m.dirs[dir] = true
		if filepath.Dir(dir) == dir {
			return
		}
	}
}

func (m *memFS) Stat(path string) (fs.FileInfo, error) {
	clean := filepath.Clean(path)
	if _, ok := m.files[clean]; ok {
		return memInfo{name: filepath.Base(clean)}, nil
	}
	if m.dirs[clean] {
		return memInfo{name: filepath.Base(clean), dir: true}, nil
	}
	return nil, fs.ErrNotExist
}

func (m *memFS) ReadFile(path string) ([]byte, error) {
	clean := filepath.Clean(path)
	if err, ok := m.readErrors[clean]; ok {
		return nil, err
	}
	content, ok := m.files[clean]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(content), nil
}

type memInfo struct {
	name string
	dir  bool
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return 0 }
func (i memInfo) Mode() fs.FileMode  { return 0 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.dir }
func (i memInfo) Sys() any           { return nil }
