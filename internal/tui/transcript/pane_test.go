package transcript

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/components"
	"github.com/pulseaiclub/phi/internal/components/block"
	"github.com/pulseaiclub/phi/internal/components/status"
	"github.com/pulseaiclub/phi/internal/session"
	"github.com/pulseaiclub/phi/internal/tui/controller"
)

func TestTranscriptPane_ApplySessionAndSync(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "Phi test")

	pane.ApplySession(session.UserAppend{Text: "hello"})
	pane.Sync()

	require.False(t, pane.IsEmpty(), "expected transcript entries after user append")
	require.Len(t, pane.Snapshot().Messages, 1)
}

func TestTranscriptPane_IsStreaming(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "Phi test")

	require.False(t, pane.IsStreaming(), "empty pane should not stream")

	pane.ApplySession(session.AssistantMessageUpdate{Message: session.Message{
		ID:    "a1",
		State: session.StateStreaming,
	}})
	require.True(t, pane.IsStreaming(), "expected streaming after assistant StateStreaming")

	pane.ApplySession(session.AssistantMessageUpdate{Message: session.Message{
		ID:    "a1",
		State: session.StateComplete,
	}})
	require.False(t, pane.IsStreaming(), "expected idle after StreamEnd")
}

func TestTranscriptPane_LoadReplayClearsWidgets(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "Phi test")

	pane.ApplySession(session.UserAppend{Text: "x"})
	pane.Sync()
	require.False(t, pane.IsEmpty(), "setup: expected entries")

	pane.LoadReplay(session.Snapshot{})
	pane.Sync()
	require.True(t, pane.IsEmpty(), "LoadReplay should clear visible entries until snap has items")
}

const paneTestRows = 12

// bashPane draws one user turn plus a collapsed bash run and returns the drawn
// pane. Rows are bottom-anchored, so the bash title is the last viewport row.
func bashPane(t *testing.T) (*TranscriptPane, *controller.Bus, *block.BashBlock) {
	t.Helper()
	th := components.DefaultTheme()
	pane := NewTranscriptPane(th, status.NewSpinner(th.ToolName), "Phi test")
	bus := controller.NewBus(nil)
	pane.SetCopyHandlers(bus, func(string) bool { return true })
	pane.LoadReplay(session.Snapshot{
		Messages: []session.Message{
			{ID: "u1", Role: session.RoleUser, State: session.StateComplete, Text: "hello"},
			{
				ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
				Content: []session.ContentBlock{{Type: session.BlockToolUse, ID: "t1", Name: "bash"}},
			},
		},
		Tools: map[string]session.ToolRun{
			"t1": {ToolUseID: "t1", Name: "bash", Status: session.ToolDone, Detail: "ls", Output: "a.go"},
		},
	})
	pane.Sync()
	pane.Draw(
		components.DrawContext{Max: components.Size{Width: 60, Height: paneTestRows}},
		60, paneTestRows,
	)
	require.NotEmpty(t, pane.list.Entries, "setup: expected transcript entries")
	bash, ok := pane.list.Entries[len(pane.list.Entries)-1].(*block.BashBlock)
	require.True(t, ok, "setup: expected the bash run last")
	return pane, bus, bash
}

// Dragging from a collapsed title row must copy text, not expand: the block
// toggles only once the pane has ruled out a drag-selection.
func TestTranscriptPane_DragFromTitleRowCopies(t *testing.T) {
	pane, bus, bash := bashPane(t)
	var copied string
	pane.SetCopyHandlers(bus, func(text string) bool {
		copied = text
		return true
	})
	const row = paneTestRows - 1

	ctx := &components.EventContext{}
	pane.HandleMouse(ctx, xui.MouseEvent{X: 2, Y: row, Action: xui.MousePress, Button: xui.MouseLeft}, nil)
	pane.HandleMouse(ctx, xui.MouseEvent{X: 24, Y: row, Action: xui.MouseDrag, Button: xui.MouseLeft}, nil)
	pane.HandleMouse(ctx, xui.MouseEvent{X: 24, Y: row, Action: xui.MouseRelease, Button: xui.MouseLeft}, nil)

	require.Equal(t, "ls", copied, "drag across the title row copies the text without chrome")
	require.False(t, bash.Expanded, "dragging from the title row must not expand")

	batch := bus.Drain()
	require.Len(t, batch, 1)
	msg, ok := batch[0].(controller.ToastMsg)
	require.True(t, ok, "copy feedback goes through the bus")
	require.Equal(t, "Selection copied to clipboard", msg.Message)
}

// A press that never moves is a click: the same row toggles instead of copying.
func TestTranscriptPane_ClickOnTitleRowToggles(t *testing.T) {
	pane, _, bash := bashPane(t)
	const row = paneTestRows - 1

	ctx := &components.EventContext{}
	pane.HandleMouse(ctx, xui.MouseEvent{X: 2, Y: row, Action: xui.MousePress, Button: xui.MouseLeft}, nil)
	require.False(t, bash.Expanded, "press alone must not expand")
	pane.HandleMouse(ctx, xui.MouseEvent{X: 2, Y: row, Action: xui.MouseRelease, Button: xui.MouseLeft}, nil)

	require.True(t, bash.Expanded, "click on the title row expands")
	require.Equal(t, len(pane.list.Entries)-1, pane.list.Selected, "the clicked row is selected for Cmd+C")
}
