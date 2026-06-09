// Package config centralizes runtime-mode detection and server-side paths
// that are shared across the CLI, web server, and scheduler.
//
// ghquery runs in one of two modes (see HOSTED-MODE-DESIGN.md):
//
//   - local mode (default): reads config.yaml, writes per-user preferences
//     back to it, and delegates scheduling to the OS scheduler.
//   - hosted mode (GHQUERY_HOSTED set): reads a read-only mounted catalog
//     file, keeps per-user preferences in the browser, and runs scheduling
//     in-process. Secrets come from environment variables.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// IsHosted reports whether ghquery is running in hosted (shared web app) mode.
// Hosted mode is enabled by setting GHQUERY_HOSTED to a truthy value
// (1, true, yes, or on — case-insensitive).
func IsHosted() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GHQUERY_HOSTED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// CatalogPath returns the path to the read-only catalog file used in hosted
// mode. Defaults to /etc/ghquery/catalog.yaml; override with GHQUERY_CATALOG.
func CatalogPath() string {
	if p := strings.TrimSpace(os.Getenv("GHQUERY_CATALOG")); p != "" {
		return p
	}
	return "/etc/ghquery/catalog.yaml"
}

// DataDir returns the server-side data directory used in hosted mode for the
// per-user schedule store and run logs. Defaults to ~/.ghquery; override with
// GHQUERY_DATA.
func DataDir() string {
	if d := strings.TrimSpace(os.Getenv("GHQUERY_DATA")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ghquery"
	}
	return filepath.Join(home, ".ghquery")
}

// ScheduleDir returns the directory holding per-user schedule JSON records
// in hosted mode.
func ScheduleDir() string {
	return filepath.Join(DataDir(), "schedules")
}
