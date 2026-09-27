package extension

import (
	"context"
	"os"
	"path/filepath"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/debuglog"
)

// Load discovers PXB extensions and spawns each subprocess.
func Load(userDir, projectDir string) (*Runner, []Warning, error) {
	found, warns, err := Discover(userDir, projectDir)
	if err != nil {
		return nil, warns, err
	}
	r := NewRunner()
	if len(found) == 0 {
		r.warns = warns
		return r, warns, nil
	}

	logDir := extensionLogDir(userDir)
	cwd, _ := os.Getwd()

	for _, d := range found {
		proc, err := StartProc(context.Background(), d.Manifest, d.Path, logDir, cwd, "")
		if err != nil {
			warns = append(warns, Warning{Path: d.Path, Message: err.Error()})
			debuglog.Logf("extension: load %s: %v", d.Path, err)
			continue
		}
		api := ext.NewAPI()
		proc.BuildAPI(api)
		r.bus.addAPI(api)
		r.procs = append(r.procs, proc)
		r.loaded = append(r.loaded, d)
	}
	r.warns = warns
	return r, warns, nil
}

func extensionLogDir(userDir string) string {
	if userDir == "" {
		return filepath.Join(os.TempDir(), "phi-ext-logs")
	}
	// ~/.phi/extensions → ~/.phi/logs
	return filepath.Join(filepath.Dir(userDir), "logs")
}
