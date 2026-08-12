package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"strings"
)

//go:embed payload
var embeddedPayload embed.FS

const manifestPath = "payload/manifest.json"

type bundleManifest struct {
	SchemaVersion int               `json:"schema_version"`
	BundleID      string            `json:"bundle_id"`
	Version       string            `json:"version"`
	TargetOS      string            `json:"target_os"`
	TargetArch    string            `json:"target_arch"`
	Unsigned      bool              `json:"unsigned"`
	Components    []bundleComponent `json:"components"`
}

type bundleComponent struct {
	Path       string `json:"path"`
	Role       string `json:"role"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Mode       uint32 `json:"mode"`
	License    string `json:"license,omitempty"`
	Executable bool   `json:"executable,omitempty"`
}

func loadManifest() (bundleManifest, error) {
	source, err := embeddedPayload.ReadFile(manifestPath)
	if err != nil {
		return bundleManifest{}, fmt.Errorf("read embedded manifest: %w", err)
	}
	var manifest bundleManifest
	if err := json.Unmarshal(source, &manifest); err != nil {
		return bundleManifest{}, fmt.Errorf("decode embedded manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.BundleID == "" || manifest.Version == "" {
		return bundleManifest{}, fmt.Errorf("invalid embedded manifest identity")
	}
	if manifest.TargetOS != "" && (manifest.TargetOS != runtime.GOOS || manifest.TargetArch != runtime.GOARCH) {
		return bundleManifest{}, fmt.Errorf("bundle target %s/%s cannot run on %s/%s", manifest.TargetOS, manifest.TargetArch, runtime.GOOS, runtime.GOARCH)
	}
	seen := make(map[string]struct{}, len(manifest.Components))
	for _, component := range manifest.Components {
		clean := path.Clean(component.Path)
		if clean != component.Path || clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || component.Size < 0 || len(component.SHA256) != 64 {
			return bundleManifest{}, fmt.Errorf("invalid component path or metadata: %q", component.Path)
		}
		if _, duplicate := seen[component.Path]; duplicate {
			return bundleManifest{}, fmt.Errorf("duplicate component path: %s", component.Path)
		}
		seen[component.Path] = struct{}{}
		if _, err := fs.Stat(embeddedPayload, "payload/files/"+component.Path); err != nil {
			return bundleManifest{}, fmt.Errorf("embedded component missing: %s", component.Path)
		}
	}
	return manifest, nil
}

func (manifest bundleManifest) componentByRole(role string) (bundleComponent, bool) {
	for _, component := range manifest.Components {
		if component.Role == role {
			return component, true
		}
	}
	return bundleComponent{}, false
}
