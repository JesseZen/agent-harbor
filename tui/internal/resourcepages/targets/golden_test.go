package targets

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	"github.com/charmbracelet/x/ansi"
)

var updateGoldens = flag.Bool("update-goldens", false, "update ANSI golden fixtures")

func TestResponsiveGoldens(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})

	kinds := []struct {
		kind Kind
		file string
	}{
		{KindCredentials, "credentials"},
		{KindEndpoints, "endpoints"},
		{KindTargets, "targets"},
	}
	sizes := []struct {
		name          string
		width, height int
	}{
		{"160x45", 160, 45},
		{"120x30", 120, 30},
		{"90x30", 90, 30},
		{"70x30", 70, 30},
	}

	for _, kind := range kinds {
		page.SetKind(kind.kind)
		for _, size := range sizes {
			t.Run(kind.file+"/"+size.name, func(t *testing.T) {
				page.SetSize(size.width, size.height)
				view := page.View()
				plain := ansi.Strip(view)

				if !strings.Contains(plain, "ID") || !strings.Contains(plain, "NAME") {
					t.Fatalf("%s lost permanent ID/NAME columns:\n%s", size.name, plain)
				}
				if !strings.Contains(plain, "┌") || !strings.Contains(plain, "└") {
					t.Fatalf("%s missing K9s box border:\n%s", size.name, plain)
				}
				if strings.Contains(plain, "╭") {
					t.Fatalf("%s contains rounded border:\n%s", size.name, plain)
				}
				for _, line := range strings.Split(plain, "\n") {
					if ansi.StringWidth(line) > size.width {
						t.Fatalf("%s line wider than viewport (%d>%d): %q", size.name, ansi.StringWidth(line), size.width, line)
					}
				}
				if bytesContainPlanted(view) {
					t.Fatalf("%s golden contains planted token", size.name)
				}
				if !strings.Contains(view, resourceview.TokenBorder) && !strings.Contains(view, kind.kind.Label()) {
					t.Fatalf("%s missing K9s chrome:\n%s", size.name, view)
				}

				path := filepath.Join("testdata", "golden", kind.file+"-"+size.name+".ansi")
				if *updateGoldens {
					if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
						t.Fatalf("write golden: %v", err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden %s (run with -update-goldens): %v", path, err)
				}
				if string(want) != view {
					t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", size.name, view, want)
				}
			})
		}
	}
}

func TestSimpleUpstreamResponsiveGoldens(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	makeFixtureUpstreamSimple(draft)
	page.SetKind(KindUpstreams)

	sizes := []struct {
		name          string
		width, height int
	}{
		{"160x45", 160, 45},
		{"120x30", 120, 30},
		{"90x30", 90, 30},
		{"70x30", 70, 30},
	}
	for _, size := range sizes {
		t.Run(size.name, func(t *testing.T) {
			page.SetSize(size.width, size.height)
			view := page.View()
			plain := ansi.Strip(view)
			for _, column := range []string{"NAME", "API FORMATS", "URL"} {
				if !strings.Contains(plain, column) {
					t.Fatalf("%s lost permanent %s column:\n%s", size.name, column, plain)
				}
			}
			if !strings.Contains(plain, "┌") || !strings.Contains(plain, "└") {
				t.Fatalf("%s missing K9s box border:\n%s", size.name, plain)
			}
			for _, line := range strings.Split(plain, "\n") {
				if ansi.StringWidth(line) > size.width {
					t.Fatalf("%s line wider than viewport (%d>%d): %q", size.name, ansi.StringWidth(line), size.width, line)
				}
			}
			if bytesContainPlanted(view) {
				t.Fatalf("%s golden contains planted token", size.name)
			}
			path := filepath.Join("testdata", "golden", "upstreams-"+size.name+".ansi")
			if *updateGoldens {
				if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s (run with -update-goldens): %v", path, err)
			}
			if string(want) != view {
				t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", size.name, view, want)
			}
		})
	}
}

func TestUpstreamFormResponsiveGoldens(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindUpstreams)
	page.BeginCreate()
	for _, size := range []struct {
		name          string
		width, height int
	}{{"160x45", 160, 45}, {"120x30", 120, 30}, {"90x30", 90, 30}, {"70x30", 70, 30}} {
		t.Run(size.name, func(t *testing.T) {
			page.SetSize(size.width, size.height)
			view := page.View()
			plain := ansi.Strip(view)
			if !strings.Contains(plain, "╭") || strings.Contains(plain, "$.") || bytesContainPlanted(view) {
				t.Fatalf("invalid or unsafe form:\n%s", plain)
			}
			for _, line := range strings.Split(plain, "\n") {
				if ansi.StringWidth(line) > size.width {
					t.Fatalf("line overflow: %q", line)
				}
			}
			path := filepath.Join("testdata", "golden", "upstream-form-"+size.name+".ansi")
			if *updateGoldens {
				if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != view {
				t.Fatalf("golden mismatch %s", path)
			}
		})
	}
}

func bytesContainPlanted(view string) bool {
	token := plantedToken()
	return strings.Contains(view, string(token))
}
