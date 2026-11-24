package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type extensionItem struct {
	ID          string
	Name        string
	Description string
	Source      string
	Path        string
	Enabled     bool
	Toggleable  bool
	Scope       string
	Clients     []string
}

type extensionSnapshot struct {
	ProfileName string
	CodexHome   string
	SkillsCLI   string
	Skills      []extensionItem
	Warnings    []string
}

type skillCatalogItem struct {
	ID        string
	Name      string
	Source    string
	Skill     string
	Installs  int
	Installed bool
}

type profileExtensionService interface {
	InspectExtensionsLocal(context.Context, profileBinding) (extensionSnapshot, error)
	InspectCodexSkills(context.Context, profileBinding) ([]extensionItem, error)
	SearchSkills(context.Context, profileBinding, string) ([]skillCatalogItem, error)
	ToggleExtension(context.Context, profileBinding, extensionItem, bool) error
	ExtensionExecCommand(context.Context, profileBinding, string, string) (*exec.Cmd, error)
}

func resolveSkillsCLI(env []string) (mode, path string) {
	if explicit := strings.TrimSpace(os.Getenv("AGENT_HARBOR_SKILLS_BIN")); explicit != "" {
		if filepath.IsAbs(explicit) && isExecutableFile(explicit) {
			return "companion", explicit
		}
	}
	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			self = resolved
		}
		candidate := filepath.Join(filepath.Dir(self), "skills")
		if isExecutableFile(candidate) {
			return "companion", candidate
		}
	}
	pathEnv := environmentValue(env, "PATH")
	if pathEnv == "" {
		pathEnv = os.Getenv("PATH")
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if candidate := filepath.Join(dir, "skills"); isExecutableFile(candidate) {
			return "path", candidate
		}
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if candidate := filepath.Join(dir, "npx"); isExecutableFile(candidate) {
			return "npx", candidate
		}
	}
	return "unavailable", ""
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

func skillInputIsSource(target string) bool {
	target = strings.TrimSpace(target)
	return strings.Contains(target, "/") || strings.HasPrefix(target, "git@")
}

type codexSkillConfigEntry struct {
	Path    string `toml:"path"`
	Enabled *bool  `toml:"enabled"`
}

func readCodexSkillDisabledMap(codexHome string) (map[string]bool, error) {
	disabled := map[string]bool{}
	path := filepath.Join(codexHome, "config.toml")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return disabled, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return disabled, errors.New("Codex config.toml is unreadable")
	}
	var cfg struct {
		Skills struct {
			Config []codexSkillConfigEntry `toml:"config"`
		} `toml:"skills"`
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return disabled, errors.New("Codex config.toml could not be parsed")
	}
	for _, entry := range cfg.Skills.Config {
		if entry.Path == "" || entry.Enabled == nil {
			continue
		}
		canonical := canonicalSkillPath(entry.Path)
		disabled[entry.Path] = !*entry.Enabled
		if canonical != "" {
			disabled[canonical] = !*entry.Enabled
		}
	}
	return disabled, nil
}

func canonicalSkillPath(p string) string {
	if p == "" {
		return ""
	}
	cleaned := filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}
	return cleaned
}

// writeCodexSkillConfigFallback updates or appends a [[skills.config]] entry in
// $CODEX_HOME/config.toml atomically while preserving unrelated TOML sections.
func writeCodexSkillConfigFallback(codexHome, skillPath string, enabled bool) error {
	if codexHome == "" || skillPath == "" {
		return errors.New("skill path and Codex home are required")
	}
	if err := os.MkdirAll(codexHome, 0700); err != nil {
		return err
	}
	configPath := filepath.Join(codexHome, "config.toml")
	var existing string
	if data, err := os.ReadFile(configPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	canonicalTarget := canonicalSkillPath(skillPath)
	lines := []string{}
	if existing != "" {
		lines = strings.Split(strings.TrimRight(existing, "\n"), "\n")
	}

	var out []string
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "[[skills.config]]" {
			block := []string{lines[i]}
			i++
			for i < len(lines) {
				next := strings.TrimSpace(lines[i])
				if strings.HasPrefix(next, "[") {
					break
				}
				block = append(block, lines[i])
				i++
			}
			var parsed struct {
				Skills struct {
					Config []codexSkillConfigEntry `toml:"config"`
				} `toml:"skills"`
			}
			if _, err := toml.Decode(strings.Join(block, "\n"), &parsed); err == nil && len(parsed.Skills.Config) == 1 {
				entryPath := parsed.Skills.Config[0].Path
				if entryPath == skillPath || (canonicalTarget != "" && canonicalSkillPath(entryPath) == canonicalTarget) {
					continue // replace with updated block below
				}
			}
			out = append(out, block...)
			continue
		}
		out = append(out, lines[i])
		i++
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) > 0 {
		out = append(out, "")
	}
	out = append(out,
		"[[skills.config]]",
		"path = "+strconv.Quote(skillPath),
		"enabled = "+strconv.FormatBool(enabled),
	)
	content := strings.Join(out, "\n") + "\n"
	return writeStateFile(configPath, []byte(content))
}

// scanLocalSkills discovers skills from shared ~/.agents/skills and Profile
// $CODEX_HOME/skills in pure Go without requiring Node or an App Server.
func scanLocalSkills(userHome, codexHome string) ([]extensionItem, []string) {
	var warnings []string
	disabled, err := readCodexSkillDisabledMap(codexHome)
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	sharedRoot := ""
	if userHome != "" {
		sharedRoot = filepath.Join(userHome, ".agents", "skills")
	}
	profileRoot := ""
	if codexHome != "" {
		profileRoot = filepath.Join(codexHome, "skills")
	}

	byCanonical := map[string]extensionItem{}
	order := []string{}
	addFromRoot := func(root, defaultScope, defaultSource string) {
		if root == "" {
			return
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			dirPath := filepath.Join(root, name)
			skillFile := filepath.Join(dirPath, "SKILL.md")
			info, err := os.Stat(skillFile)
			if err != nil || info.IsDir() {
				continue
			}
			canonical := canonicalSkillPath(skillFile)
			if existing, dup := byCanonical[canonical]; dup {
				// If profile root symlinks into ~/.agents/skills, preserve shared origin.
				if sharedRoot != "" && strings.HasPrefix(canonical, canonicalSkillPath(sharedRoot)+string(os.PathSeparator)) {
					existing.Scope = "global-shared"
					existing.Source = "shared (~/.agents/skills)"
				}
				if disabled[skillFile] || disabled[canonical] {
					existing.Enabled = false
				}
				byCanonical[canonical] = existing
				continue
			}
			parsedName, desc := parseSkillFrontmatter(skillFile)
			if parsedName == "" {
				parsedName = name
			}
			scope, source := defaultScope, defaultSource
			if sharedRoot != "" && strings.HasPrefix(canonical, canonicalSkillPath(sharedRoot)+string(os.PathSeparator)) {
				scope = "global-shared"
				source = "shared (~/.agents/skills)"
			}
			enabled := !disabled[skillFile] && !disabled[canonical]
			byCanonical[canonical] = extensionItem{
				ID:          canonical,
				Name:        parsedName,
				Description: desc,
				Source:      source,
				Path:        skillFile,
				Enabled:     enabled,
				Toggleable:  true,
				Scope:       scope,
				Clients:     []string{"codex"},
			}
			order = append(order, canonical)
		}
	}

	addFromRoot(sharedRoot, "global-shared", "shared (~/.agents/skills)")
	addFromRoot(profileRoot, "profile", "profile ($CODEX_HOME/skills)")

	items := make([]extensionItem, 0, len(order))
	for _, key := range order {
		items = append(items, byCanonical[key])
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, warnings
}

func parseSkillFrontmatter(path string) (name, description string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, 65536))
	first, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", ""
	}
	if strings.TrimSpace(first) != "---" {
		return "", ""
	}
	for {
		line, err := r.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || trimmed == "..." {
			break
		}
		if key, val, ok := strings.Cut(trimmed, ":"); ok {
			k := strings.ToLower(strings.TrimSpace(key))
			v := strings.TrimSpace(val)
			v = strings.Trim(v, `"'`)
			switch k {
			case "name":
				if name == "" {
					name = v
				}
			case "description":
				if description == "" {
					description = v
				}
			}
		}
		if err != nil {
			break
		}
	}
	return cleanUI(name), cleanUI(description)
}
