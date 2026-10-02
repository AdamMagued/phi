package edittool

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools/tooldef"
)

func toolContext(t *testing.T) (context.Context, string) {
	t.Helper()
	dir := t.TempDir()
	return tooldef.WithCwd(t.Context(), dir), dir
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
}

func readFiles(t *testing.T, dir string, names ...string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		out[name] = string(raw)
	}
	return out
}

func TestEditToolAppliesAPayload(t *testing.T) {
	ctx, dir := toolContext(t)
	writeFiles(t, dir, map[string]string{"a.ts": "const timeout = 1000;\nstart();\n"})

	result, err := runEdit(ctx, editInput{
		Payload: "*** SM:EDIT a.ts\n*** SM:FIND\nconst timeout = 1000;\n*** SM:PUT\nconst timeout = 5000;\n",
	})
	require.NoError(t, err)

	assert.Equal(t, "const timeout = 5000;\nstart();\n", readFiles(t, dir, "a.ts")["a.ts"])
	assert.Equal(t, "a.ts", result.Detail)
	assert.Contains(t, result.Content, "a.ts")
	assert.Contains(t, result.Content, "-const timeout = 1000;")
	assert.Contains(t, result.Content, "+const timeout = 5000;")
}

func TestEditToolEditsMultipleFilesAtomically(t *testing.T) {
	ctx, dir := toolContext(t)
	writeFiles(t, dir, map[string]string{
		"a.ts": "const endpoint = \"/v1\";\n",
		"b.ts": "router.use(\"/v1\", api);\n",
	})

	payload := "*** SM:EDIT a.ts\n*** SM:FIND\nconst endpoint = \"/v1\";\n*** SM:PUT\nconst endpoint = \"/v2\";\n" +
		"*** SM:EDIT b.ts\n*** SM:FIND\nrouter.use(\"/v1\", api);\n*** SM:PUT\nrouter.use(\"/v2\", api);\n"
	result, err := runEdit(ctx, editInput{Payload: payload})
	require.NoError(t, err)

	files := readFiles(t, dir, "a.ts", "b.ts")
	assert.Equal(t, "const endpoint = \"/v2\";\n", files["a.ts"])
	assert.Equal(t, "router.use(\"/v2\", api);\n", files["b.ts"])
	assert.Equal(t, "a.ts, b.ts", result.Detail)
}

func TestEditToolFailureInOneFileWritesNothing(t *testing.T) {
	ctx, dir := toolContext(t)
	writeFiles(t, dir, map[string]string{
		"a.ts": "const endpoint = \"/v1\";\n",
		"b.ts": "unrelated();\n",
	})

	payload := "*** SM:EDIT a.ts\n*** SM:FIND\nconst endpoint = \"/v1\";\n*** SM:PUT\nconst endpoint = \"/v2\";\n" +
		"*** SM:EDIT b.ts\n*** SM:FIND\nrouter.use(\"/v1\", api);\n*** SM:PUT\nrouter.use(\"/v2\", api);\n"
	_, err := runEdit(ctx, editInput{Payload: payload})
	require.Error(t, err)

	files := readFiles(t, dir, "a.ts", "b.ts")
	assert.Equal(t, "const endpoint = \"/v1\";\n", files["a.ts"])
	assert.Equal(t, "unrelated();\n", files["b.ts"])
	assert.Contains(t, err.Error(), "[b.ts]")
	assert.Contains(t, err.Error(), "No files were modified — sections apply atomically.")
	assert.Contains(t, err.Error(), atomicityNotice)
}

func TestEditToolKeepsCRLFFilesCRLF(t *testing.T) {
	ctx, dir := toolContext(t)
	writeFiles(t, dir, map[string]string{"a.ts": "const timeout = 1000;\r\nstart();\r\n"})

	_, err := runEdit(ctx, editInput{
		Payload: "*** SM:EDIT a.ts\n*** SM:FIND\nconst timeout = 1000;\n*** SM:PUT\nconst timeout = 5000;\n",
	})
	require.NoError(t, err)
	assert.Equal(t, "const timeout = 5000;\r\nstart();\r\n", readFiles(t, dir, "a.ts")["a.ts"])
}

func TestEditToolNotesReachTheModel(t *testing.T) {
	ctx, dir := toolContext(t)
	writeFiles(t, dir, map[string]string{"a.ts": "a = 1;\nrun();\nb = 2;\n"})

	result, err := runEdit(ctx, editInput{
		Payload: "*** SM:EDIT a.ts\n*** SM:FIND\nrun();\n*** SM:PUT\n",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Content, "Note: operation 1 deleted")
}

func TestEditToolRejectsEmptyAndTargetlessPayloads(t *testing.T) {
	ctx, _ := toolContext(t)

	_, err := runEdit(ctx, editInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "edit requires a payload")

	_, err = runEdit(ctx, editInput{Payload: "*** SM:FIND\nrun();\n*** SM:PUT\ngo();\n"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing file target")
}

func TestEditDetailListsTargetPaths(t *testing.T) {
	assert.Equal(t, "a.ts, b.ts", editDetail(editInput{
		Payload: "*** SM:EDIT a.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n*** SM:EDIT b.ts\n*** SM:FIND\nx\n*** SM:PUT\ny\n",
	}))
	assert.Equal(t, "edit", editDetail(editInput{}))
}

func TestEditToolReportsNotesFromUnchangedFiles(t *testing.T) {
	ctx, dir := toolContext(t)
	writeFiles(t, dir, map[string]string{"a.ts": "const a = 1;\n"})

	result, err := runEdit(ctx, editInput{Payload: "*** SM:EDIT a.ts\n*** SM:PUT\nconst a = 1;\n"})
	require.NoError(t, err)
	assert.Equal(t, "const a = 1;\n", readFiles(t, dir, "a.ts")["a.ts"])
	assert.Contains(t, result.Content, "already matches the file")
}
