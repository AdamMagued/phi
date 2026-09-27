// Package prompt builds the agent system prompt from templates and catalogs.
package prompt

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/pulseaiclub/phi/internal/llm/skills"
)

var (
	//go:embed system-prompt.tmpl
	systemPromptTmpl string
	//go:embed skills-prompt.tmpl
	skillsPromptTmpl string

	systemPrompt = template.Must(template.New("system").Parse(systemPromptTmpl))
	skillsPrompt = template.Must(template.New("skills").Parse(skillsPromptTmpl))
)

type systemData struct {
	Cwd           string
	Workspace     string
	AgentsEnabled bool
	MaxConcurrent int // sub-agent concurrency cap (0 when agents disabled)
}

type skillsData struct {
	Catalog string
}

// Sections returns the built-in system-prompt blocks, in order: the base prompt,
// the project context files, then the skills catalog. Blocks that do not apply
// are dropped, so the result is never empty. Callers join the blocks with a
// blank line and may insert or reorder plugin-contributed ones in between.
// agentsEnabled must match whether agent_* tools are registered.
func Sections(skillPath string, agentsEnabled bool, maxConcurrent int) []string {
	var buf strings.Builder
	data := systemData{
		Cwd:           currentDir(),
		Workspace:     workspaceDir(),
		AgentsEnabled: agentsEnabled,
		MaxConcurrent: maxConcurrent,
	}
	if err := systemPrompt.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("system prompt: %v", err))
	}
	parts := []string{buf.String()}
	if ctx := formatProjectContext(loadProjectContextFiles(currentDir(), phiAgentDir())); ctx != "" {
		parts = append(parts, ctx)
	}
	if skillBlock := skillsBlock(skillPath); skillBlock != "" {
		parts = append(parts, skillBlock)
	}
	return parts
}

func execTmpl(t *template.Template, data any) string {
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("%s prompt: %v", t.Name(), err))
	}
	return strings.TrimSpace(buf.String())
}

func skillsBlock(skillDir string) string {
	if skillDir == "" {
		return ""
	}
	list, err := skills.LoadSkills(skillDir)
	if err != nil || len(list) == 0 {
		return ""
	}
	catalog := strings.TrimSpace(skills.ToPromptMarkdown(list))
	if catalog == "" {
		return ""
	}
	return execTmpl(skillsPrompt, skillsData{Catalog: catalog})
}

func currentDir() string {
	path, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return path
}

// workspaceDir returns the nearest ancestor of cwd that contains .git, or "".
func workspaceDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
