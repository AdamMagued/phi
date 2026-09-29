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
	//go:embed mcp-prompt.tmpl
	mcpPromptTmpl string

	systemPrompt = template.Must(template.New("system").Parse(systemPromptTmpl))
	skillsPrompt = template.Must(template.New("skills").Parse(skillsPromptTmpl))
	mcpPrompt    = template.Must(template.New("mcp").Parse(mcpPromptTmpl))
)

type systemData struct {
	Cwd           string
	Workspace     string
	AgentsEnabled bool
	MaxConcurrent int    // sub-agent concurrency cap (0 when agents disabled)
	ToolRoster    string // one line per registered tool, or "- (none)"
}

// Tool is one entry in the prompt's tool roster. Tools registered without a
// Summary are left out of the roster; their schema still reaches the model.
type Tool struct {
	Name    string
	Summary string
}

type skillsData struct {
	Catalog string
}

type mcpData struct {
	Servers []string
}

// Build assembles the system prompt.
// agentsEnabled must match whether agent_* tools are registered.
// mcpServers are configured server names only (no tool schemas).
// toolList is the registered tool set; pass nil when unknown and the roster
// renders as "- (none)".
func Build(skillPath string, agentsEnabled bool, maxConcurrent int, mcpServers []string, toolList []Tool) string {
	var buf strings.Builder
	data := systemData{
		Cwd:           currentDir(),
		Workspace:     workspaceDir(),
		AgentsEnabled: agentsEnabled,
		MaxConcurrent: maxConcurrent,
		ToolRoster:    formatToolRoster(toolList),
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
	if mcpBlock := mcpBlock(mcpServers); mcpBlock != "" {
		parts = append(parts, mcpBlock)
	}
	return strings.Join(parts, "\n\n")
}

// formatToolRoster renders the "# Tools" menu: one line per tool that declares
// a Summary. The roster is generated from the live tool set so it cannot drift
// from what the request actually offers.
func formatToolRoster(toolList []Tool) string {
	lines := make([]string, 0, len(toolList))
	for _, tool := range toolList {
		name := strings.TrimSpace(tool.Name)
		summary := strings.TrimSpace(tool.Summary)
		if name == "" || summary == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- `%s` — %s", name, summary))
	}
	if len(lines) == 0 {
		return "- (none)"
	}
	return strings.Join(lines, "\n")
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

func mcpBlock(serverNames []string) string {
	servers := make([]string, 0, len(serverNames))
	for _, name := range serverNames {
		name = strings.TrimSpace(name)
		if name != "" {
			servers = append(servers, name)
		}
	}
	if len(servers) == 0 {
		return ""
	}
	return execTmpl(mcpPrompt, mcpData{Servers: servers})
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
