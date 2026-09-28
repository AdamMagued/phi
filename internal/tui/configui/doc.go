// Package configui is the full-screen editor behind `phi config`: one keyboard
// form over ~/.phi/config.yaml.
//
// The document (project.ConfigDoc) is the single source of truth. Rows are
// rebuilt from it after every edit, the cursor is anchored to a row key instead
// of an index, and nothing is written to disk until the user saves.
package configui

import "context"

// ModelLister fetches the model IDs a provider advertises for one connection.
// It runs off the UI goroutine, so implementations must be safe for concurrent
// use and must honor ctx.
type ModelLister func(ctx context.Context, baseURL, apiKey, api, model string) ([]string, error)
