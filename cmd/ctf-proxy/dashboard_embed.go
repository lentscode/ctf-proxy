//go:build production

package main

import (
	"embed"
	"fmt"
	"io/fs"
)

// dashboardFiles contains the Vite production output. It is compiled only
// into release builds, after build:frontend has created dashboard/dist.
//
//go:embed dashboard/dist
var dashboardFiles embed.FS

func init() {
	loadEmbeddedDashboard = func() (fs.FS, error) {
		assets, err := fs.Sub(dashboardFiles, "dashboard/dist")
		if err != nil {
			return nil, fmt.Errorf("open embedded dashboard: %w", err)
		}
		if _, err := fs.Stat(assets, "index.html"); err != nil {
			return nil, fmt.Errorf("validate embedded dashboard: %w", err)
		}
		return assets, nil
	}
}
