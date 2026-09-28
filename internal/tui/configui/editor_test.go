package configui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/pulseaiclub/phi/internal/components"
	"github.com/pulseaiclub/phi/internal/project"
)

// newTestEditor builds an editor over a fake home, so paths in the chrome stay
// short and "~" shortening is exercised.
func newTestEditor(t *testing.T, doc *project.ConfigDoc) *ConfigEditor {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	e := New(doc, filepath.Join(home, ".phi", "config.yaml"), filepath.Join(home, ".phi", "skills"),
		components.DarkTheme(), nil, nil)
	require.NotEmpty(t, e.rows)
	return e
}

// focus moves the cursor onto key, failing the test when the row is missing.
func focus(t *testing.T, e *ConfigEditor, key string) {
	t.Helper()
	for i := range e.rows {
		if e.rows[i].key == key {
			e.setCursor(i)
			require.Equal(t, key, e.rows[e.cursor].key)
			return
		}
	}
	t.Fatalf("no row with key %q", key)
}

func press(e *ConfigEditor, code xui.KeyCode, r rune) *components.EventContext {
	ctx := &components.EventContext{}
	e.Handle(ctx, xui.KeyEvent{Code: code, Rune: r, Press: true})
	return ctx
}

func typeRune(e *ConfigEditor, r rune) {
	press(e, xui.KeyRune, r)
}

func sampleDoc() *project.ConfigDoc {
	return &project.ConfigDoc{
		Models: []project.ModelDoc{
			{Name: "model-a", APIKey: "key-a", BaseURL: "https://a.example/v1", Default: true},
			{Name: "model-b", APIKey: "key-b", BaseURL: "https://b.example/v1"},
		},
	}
}

func TestBuildRowsCoverEverySection(t *testing.T) {
	e := newTestEditor(t, sampleDoc())

	var sections []string
	for _, r := range e.rows {
		if r.kind == rowSection {
			sections = append(sections, r.title)
		}
	}
	assert.Equal(t, []string{"Models", "Skills", "Permissions", "Bash", "Sub-agents"}, sections)

	// Every row key must be addressable, and the first model's default badge
	// must follow the document.
	require.Equal(t, "default", e.rows[1].badge)
	assert.Equal(t, "model-a", e.rows[1].name)
	assert.Empty(t, e.rows[2].badge)
}

func TestCursorSkipsSectionHeaders(t *testing.T) {
	e := newTestEditor(t, sampleDoc())
	for range len(e.rows) * 2 {
		e.move(1)
		require.NotEqual(t, rowSection, e.rows[e.cursor].kind, "cursor landed on a section header")
	}
	for range len(e.rows) * 2 {
		e.move(-1)
		require.NotEqual(t, rowSection, e.rows[e.cursor].kind, "cursor landed on a section header")
	}

	e.jumpEdge(true)
	require.NotEqual(t, rowSection, e.rows[e.cursor].kind)
	e.jumpEdge(false)
	require.NotEqual(t, rowSection, e.rows[e.cursor].kind)
}

func TestEditModelNameRejectsEmpty(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, modelKey(0, fName))
	press(e, xui.KeyEnter, 0)
	require.NotNil(t, e.edit, "Enter must open the inline editor")

	press(e, xui.KeyBackspace, 0)
	for range len("model-a") - 1 {
		press(e, xui.KeyBackspace, 0)
	}
	press(e, xui.KeyEnter, 0)
	require.NotNil(t, e.edit, "an empty name must keep the editor open")
	assert.Equal(t, "model-a", doc.Models[0].Name)
	assert.Equal(t, statusError, e.statusKind)

	typeRune(e, 'x')
	press(e, xui.KeyEnter, 0)
	assert.Nil(t, e.edit)
	assert.Equal(t, "x", doc.Models[0].Name)
	assert.True(t, e.Dirty())
}

func TestEditKeyPromptUsesNoCLIValues(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, modelKey(0, fKey))
	press(e, xui.KeyEnter, 0)
	require.NotNil(t, e.edit)
	assert.Equal(t, "key-a", string(e.edit.buf), "editing starts from the stored value")

	press(e, xui.KeyEscape, 0)
	assert.Nil(t, e.edit)
	assert.False(t, e.Dirty(), "canceling an edit must not dirty the document")
}

func TestCycleChoiceAndTriState(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, keyPermMode)
	assert.Nil(t, doc.Permissions)
	press(e, xui.KeyRight, 0)
	require.NotNil(t, doc.Permissions)
	assert.Equal(t, "interactive", doc.Permissions.Mode)
	press(e, xui.KeyLeft, 0)
	assert.Empty(t, doc.Permissions.Mode)

	focus(t, e, modelKey(0, fImage))
	press(e, xui.KeyRight, 0)
	require.NotNil(t, doc.Models[0].ImageEnabled)
	assert.True(t, *doc.Models[0].ImageEnabled)
	press(e, xui.KeyRight, 0)
	require.NotNil(t, doc.Models[0].ImageEnabled)
	assert.False(t, *doc.Models[0].ImageEnabled)
	press(e, xui.KeyRight, 0)
	assert.Nil(t, doc.Models[0].ImageEnabled, "the third step must return to auto (absent)")
}

func TestDefaultModelIsExclusive(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, modelKey(1, fDeflt))
	press(e, xui.KeyEnter, 0)
	assert.False(t, doc.Models[0].Default)
	assert.True(t, doc.Models[1].Default)
}

func TestAddAndDeleteModel(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	typeRune(e, 'a')
	require.Len(t, doc.Models, 3)
	assert.Equal(t, "new model — type a model id", e.status)
	require.NotNil(t, e.edit, "adding a model opens the name editor")
	typeRune(e, 'c')
	press(e, xui.KeyEnter, 0)
	assert.Equal(t, "c", doc.Models[2].Name)

	focus(t, e, modelKey(2, fName))
	typeRune(e, 'd')
	require.NotNil(t, e.confirm, "deleting a model must confirm first")
	assert.False(t, e.confirm.yes, "a destructive confirm must default to No")
	assert.Len(t, doc.Models, 3)

	press(e, xui.KeyEnter, 0) // Enter takes the default: keep the model.
	assert.Len(t, doc.Models, 3)

	typeRune(e, 'd')
	typeRune(e, 'y')
	assert.Nil(t, e.confirm)
	assert.Len(t, doc.Models, 2)

	// Deleting the default promotes the next model, so the file never relies on
	// the loader's implicit first-entry rule.
	focus(t, e, modelKey(0, fName))
	typeRune(e, 'd')
	typeRune(e, 'y')
	require.Len(t, doc.Models, 1)
	assert.True(t, doc.Models[0].Default)
}

func TestDeleteModelCanBeCancelled(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, modelKey(0, fName))
	typeRune(e, 'd')
	press(e, xui.KeyEscape, 0)
	assert.Nil(t, e.confirm)
	assert.Len(t, doc.Models, 2)
}

func TestBashRulesInheritUntilEdited(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	// Collapsed, the field says the rules are the built-in ones.
	focus(t, e, keyPermBashAllow)
	r, _ := e.focused()
	assert.True(t, r.inherited)
	assert.Contains(t, r.display, "built-in")

	// Expanding lists the effective rules and offers a new-pattern row.
	press(e, xui.KeyEnter, 0)
	assert.Equal(t, keyPermBashAllow, e.expanded)
	require.Greater(t, len(e.rows), len(e.listItems(keyPermBashAllow)))
	assert.Nil(t, doc.Permissions, "looking at the rules must not write them")
	r, _ = e.focused()
	assert.Equal(t, rowItem, r.kind)

	// Editing one rule materializes the whole list, built-ins included.
	press(e, xui.KeyEnter, 0)
	require.NotNil(t, e.edit)
	assert.Equal(t, e.listItems(keyPermBashAllow)[0], string(e.edit.buf))
	press(e, xui.KeyEnd, 0)
	typeRune(e, 'x')
	press(e, xui.KeyEnter, 0)
	require.NotNil(t, doc.Permissions)
	require.NotNil(t, doc.Permissions.Bash)
	require.NotNil(t, doc.Permissions.Bash.Allow)
	assert.Len(t, *doc.Permissions.Bash.Allow, len(e.listItems(keyPermBashAllow)))
}

func TestAddBashRuleRow(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, keyPermBashAllow)
	press(e, xui.KeyEnter, 0)
	focus(t, e, listAddKey(keyPermBashAllow))
	press(e, xui.KeyEnter, 0)
	require.NotNil(t, e.edit)

	for _, r := range "^make " {
		typeRune(e, r)
	}
	press(e, xui.KeyEnter, 0)
	require.NotNil(t, doc.Permissions.Bash.Allow)
	assert.Equal(t, "^make ", (*doc.Permissions.Bash.Allow)[len(*doc.Permissions.Bash.Allow)-1])
	// The cursor lands on the row it just created.
	assert.Equal(t, listItemKey(keyPermBashAllow, len(*doc.Permissions.Bash.Allow)-1), e.rows[e.cursor].key)
}

func TestDeleteBashRule(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, keyPermBashDeny)
	press(e, xui.KeyEnter, 0)
	before := len(e.listItems(keyPermBashDeny))
	press(e, xui.KeyEnter, 0) // edit
	press(e, xui.KeyEscape, 0)
	typeRune(e, 'd')
	require.NotNil(t, doc.Permissions.Bash.Deny)
	assert.Len(t, *doc.Permissions.Bash.Deny, before-1)
}

func TestSaveWritesFileAndBackup(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".phi")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("models:\n  - name: old\n    api_key: k\n"), 0o600))

	doc, err := project.ReadConfigDoc(path)
	require.NoError(t, err)
	e := New(doc, path, "/skills", components.DarkTheme(), nil, nil)

	focus(t, e, modelKey(0, fName))
	press(e, xui.KeyEnter, 0)
	press(e, xui.KeyEnd, 0)
	for _, r := range "-2" {
		typeRune(e, r)
	}
	press(e, xui.KeyEnter, 0)

	press(e, xui.KeyRune, 's')
	assert.Equal(t, statusSuccess, e.statusKind, e.status)
	assert.False(t, e.Dirty())

	saved, err := project.ReadConfigDoc(path)
	require.NoError(t, err)
	require.Len(t, saved.Models, 1)
	assert.Equal(t, "old-2", saved.Models[0].Name)

	backup, err := os.ReadFile(path + ".bak")
	require.NoError(t, err)
	assert.Contains(t, string(backup), "name: old")

	// The written file must load through the runtime parser.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	p, err := project.Discover("")
	require.NoError(t, err)
	require.NoError(t, p.LoadConfig())
	assert.Equal(t, "old-2", p.Config().Model().Name)
}

func TestSaveRefusesInvalidDocument(t *testing.T) {
	doc := &project.ConfigDoc{Models: []project.ModelDoc{{Name: "", APIKey: "k"}}}
	e := New(doc, filepath.Join(t.TempDir(), "config.yaml"), "/skills", components.DarkTheme(), nil, nil)

	press(e, xui.KeyRune, 's')
	assert.Equal(t, statusError, e.statusKind)
	assert.Contains(t, e.status, "no name")
}

func TestQuitGuardsUnsavedChanges(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	ctx := press(e, xui.KeyEscape, 0)
	assert.True(t, ctx.Quit, "a clean editor quits on Esc")

	focus(t, e, keyPermWorkspace)
	typeRune(e, ' ') // toggle a bool through the space shortcut
	require.True(t, e.Dirty())

	ctx = press(e, xui.KeyEscape, 0)
	require.NotNil(t, e.confirm)
	assert.False(t, ctx.Quit)

	ctx = press(e, xui.KeyEscape, 0) // cancel the confirm
	assert.Nil(t, e.confirm)
	assert.False(t, ctx.Quit)

	press(e, xui.KeyEscape, 0)
	ctx = press(e, xui.KeyRune, 'y') // discard
	assert.True(t, ctx.Quit)
}

func TestEscapeClosesExpandedListFirst(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, keyPermBashAllow)
	press(e, xui.KeyEnter, 0)
	require.NotEmpty(t, e.expanded)

	ctx := press(e, xui.KeyEscape, 0)
	assert.Empty(t, e.expanded)
	assert.False(t, ctx.Quit)
	assert.Equal(t, keyPermBashAllow, e.rows[e.cursor].key)
}

func TestChoicePickerCommitsSelection(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	focus(t, e, keyAgentsExplore)
	press(e, xui.KeyEnter, 0)
	require.True(t, e.picker.Open)

	press(e, xui.KeyEnter, 0) // first row is "(inherit)".
	assert.False(t, e.picker.Open)
	require.NotNil(t, doc.Agents)
	require.NotNil(t, doc.Agents.Models)
	assert.Empty(t, doc.Agents.Models.Explore)

	focus(t, e, keyAgentsExplore)
	press(e, xui.KeyEnter, 0)
	require.True(t, e.picker.Open)
	typeRune(e, 'b') // filter to model-b
	press(e, xui.KeyEnter, 0)
	assert.Equal(t, "model-b", doc.Agents.Models.Explore)
}

func TestDrawPaintsFormAndStatus(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)

	ctx := components.DrawContext{Max: components.Size{Width: 100, Height: 48}, Method: xui.WidthUnicode}
	surf := e.Draw(ctx)
	text := components.SurfaceText(surf)

	assert.Contains(t, text, "phi config")
	assert.Contains(t, text, "model-a")
	assert.Contains(t, text, "~/.phi/config.yaml")
	assert.Contains(t, text, "auto")
	assert.Contains(t, text, "MODELS")
	assert.Contains(t, text, "PERMISSIONS")
	assert.Contains(t, text, "allow patterns")
	assert.Contains(t, text, "built-in · ")
	assert.Contains(t, text, "↑↓ move")

	// The caret follows the inline editor.
	focus(t, e, modelKey(0, fName))
	press(e, xui.KeyEnter, 0)
	surf = e.Draw(ctx)
	require.NotNil(t, surf.Cursor)
	assert.Positive(t, surf.Cursor.Y)

	// An unsaved document is called out in the title bar.
	typeRune(e, 'x')
	press(e, xui.KeyEnter, 0)
	surf = e.Draw(ctx)
	assert.Contains(t, components.SurfaceText(surf), "●")
}

func TestDrawIsStableOnTinyScreens(t *testing.T) {
	e := newTestEditor(t, sampleDoc())
	for _, size := range []components.Size{{Width: 1, Height: 1}, {Width: 20, Height: 4}, {Width: 40, Height: 6}} {
		surf := e.Draw(components.DrawContext{Max: size, Method: xui.WidthUnicode})
		assert.Equal(t, size, surf.Size)
	}
}

func TestMouseWheelMovesAndClickSelects(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)
	ctx := components.DrawContext{Max: components.Size{Width: 100, Height: 32}, Method: xui.WidthUnicode}
	e.Draw(ctx)

	start := e.cursor
	ev := &components.EventContext{}
	e.Handle(ev, xui.MouseEvent{Action: xui.MousePress, Button: xui.MouseWheelDown, Wheel: 2})
	assert.Equal(t, min(start+2, len(e.rows)-1), e.cursor)

	// A click on a selectable row selects it.
	y := e.rowsTop + (1 - e.scroll)
	require.GreaterOrEqual(t, y, e.rowsTop)
	ev = &components.EventContext{}
	e.Handle(ev, xui.MouseEvent{X: 6, Y: y, Action: xui.MousePress, Button: xui.MouseLeft})
	assert.Equal(t, 1, e.cursor)
}

func TestMouseDropsInlineEditOnClickAway(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)
	ctx := components.DrawContext{Max: components.Size{Width: 100, Height: 32}, Method: xui.WidthUnicode}
	e.Draw(ctx)

	// Open the inline editor on the first model's name.
	focus(t, e, modelKey(0, fName))
	e.activate()
	require.NotNil(t, e.edit)
	typeRune(e, 'X')

	// Click a different row: the edit is dropped (no silent commit), cursor
	// moves to the clicked row, and the buffer is gone.
	ev := &components.EventContext{}
	e.Handle(ev, xui.MouseEvent{X: 6, Y: e.rowsTop + (3 - e.scroll), Action: xui.MousePress, Button: xui.MouseLeft})
	assert.Nil(t, e.edit)
	assert.NotEqual(t, modelKey(0, fName), e.rows[e.cursor].key)
}

func TestMouseDismissesPickerAndIgnoresConfirm(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)
	ctx := components.DrawContext{Max: components.Size{Width: 100, Height: 32}, Method: xui.WidthUnicode}
	e.Draw(ctx)

	// A press while the picker is open closes it without touching the form.
	focus(t, e, modelKey(0, fAPI))
	e.activate() // opens the api picker
	require.True(t, e.picker.Open)
	before := e.cursor
	ev := &components.EventContext{}
	e.Handle(ev, xui.MouseEvent{X: 5, Y: 5, Action: xui.MousePress, Button: xui.MouseLeft})
	assert.False(t, e.picker.Open)
	assert.Equal(t, before, e.cursor)

	// A press while the confirm modal is up is ignored entirely.
	e.dirty = true // requestQuit only shows the modal when dirty
	e.requestQuit(&components.EventContext{})
	require.NotNil(t, e.confirm)
	cur := e.cursor
	ev = &components.EventContext{}
	e.Handle(ev, xui.MouseEvent{X: 5, Y: 5, Action: xui.MousePress, Button: xui.MouseLeft})
	require.NotNil(t, e.confirm)
	assert.Equal(t, cur, e.cursor)
}

func TestSkillsFallbackIsShownWhenUnset(t *testing.T) {
	doc := sampleDoc()
	e := newTestEditor(t, doc)
	focus(t, e, keySkillPath)
	r, _ := e.focused()
	assert.Equal(t, "~/.phi/skills", r.display)
	assert.Equal(t, styleMuted, r.style)
	// Empty skill_path stays absent from the document.
	press(e, xui.KeyEnter, 0)
	press(e, xui.KeyEnter, 0)
	assert.Nil(t, doc.SkillPath)
}

func TestFetchDiscardedWhenModelMoves(t *testing.T) {
	doc := sampleDoc()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	list := ModelLister(func(_ context.Context, _, _, _, _ string) ([]string, error) {
		return []string{"alpha", "beta"}, nil
	})
	e := New(doc, filepath.Join(home, ".phi", "config.yaml"), filepath.Join(home, ".phi", "skills"),
		components.DarkTheme(), list, func() {})

	// Start a fetch on model 0, then delete model 0 before the result lands so
	// model 1 shifts into its slot. The fetch's captured index/name pair must no
	// longer match, and the picker must not open for the wrong model.
	focus(t, e, modelKey(0, fName))
	e.fetchModels()
	require.True(t, e.fetch.active)

	// Drain the background fetch synchronously: it only touches fetchMu state.
	require.Eventually(t, func() bool {
		e.fetchMu.Lock()
		done := e.fetch.done
		e.fetchMu.Unlock()
		return done
	}, time.Second, time.Millisecond)

	// Delete model 0 (default promotion keeps model 1 as the new default).
	e.deleteModel(0)
	require.Len(t, doc.Models, 1)
	assert.Equal(t, "model-b", doc.Models[0].Name)

	e.pollFetch()
	assert.False(t, e.picker.Open, "picker must not open for a model that moved")
	assert.Contains(t, e.status, "changed while fetching")
}

func TestHumanTokens(t *testing.T) {
	cases := map[int]string{
		0:       "",
		999:     "999",
		128000:  "128k",
		1000000: "1M",
		1280000: "1.3M",
		2000000: "2M",
	}
	for n, want := range cases {
		assert.Equal(t, want, humanTokens(n), "n=%d", n)
	}
}
