package observations

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	"github.com/charmbracelet/x/ansi"
)

var updateGoldens = flag.Bool("update-goldens", false, "update ANSI golden fixtures")

func TestResponsiveGoldens(t *testing.T) {
	// Goldens pin wall-clock cells; keep Local=UTC so fixtures stay portable.
	prev := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = prev })

	page := newTestPage(t)

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

			if !strings.Contains(plain, "TIME") || !strings.Contains(plain, "KIND") {
				t.Fatalf("%s lost columns:\n%s", size.name, plain)
			}
			if !strings.Contains(plain, "┌") || !strings.Contains(plain, "└") {
				t.Fatalf("%s missing K9s box border:\n%s", size.name, plain)
			}
			if strings.Contains(plain, "╭") {
				t.Fatalf("%s contains rounded border:\n%s", size.name, plain)
			}
			for _, line := range strings.Split(plain, "\n") {
				if ansi.StringWidth(line) > size.width {
					t.Fatalf("%s line wider than viewport: %q", size.name, line)
				}
			}
			if !strings.Contains(view, resourceview.TokenBorder) && !strings.Contains(view, "Observations") {
				t.Fatalf("%s missing K9s chrome", size.name)
			}

			path := filepath.Join("testdata", "golden", "observations-"+size.name+".ansi")
			if *updateGoldens {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
					t.Fatal(err)
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
