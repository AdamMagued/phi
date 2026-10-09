package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultThemeIsDark(t *testing.T) {
	assert.Equal(t, DarkTheme().ToolName.Fg, DefaultTheme().ToolName.Fg)
	assert.Equal(t, DarkTheme().Identity.Fg, DefaultTheme().Identity.Fg)
}

// The theme picker marks DefaultThemeName as the startup theme before any
// palette switch, so the name must resolve to DefaultTheme itself.
func TestDefaultThemeNameRoundTrips(t *testing.T) {
	th, ok := ThemeByName(DefaultThemeName())
	require.True(t, ok)
	assert.Equal(t, DefaultTheme().ToolName.Fg, th.ToolName.Fg)
	assert.Equal(t, DefaultTheme().Identity.Fg, th.Identity.Fg)
}

func TestThemesSeparateIdentityFromSuccess(t *testing.T) {
	for _, name := range ThemeNames() {
		th, ok := ThemeByName(name)
		require.True(t, ok, name)
		assert.NotEqual(t, th.Success.Fg, th.Identity.Fg, "%s: Identity must not equal Success", name)
	}
}

func TestTitleOrForeground(t *testing.T) {
	th := DarkTheme()
	st := th.TitleOrForeground()
	assert.Equal(t, th.Title.Fg, st.Fg)
	assert.True(t, st.Bold)
}
