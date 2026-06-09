package web

import (
	"embed"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/viper"

	"github.com/DHaussermann/ghquery/internal/config"
	"github.com/DHaussermann/ghquery/internal/schedule"
)

//go:embed static/index.html
var staticFS embed.FS

// StartServer starts the web UI server and blocks.
func StartServer(port int, shutdown chan struct{}) error {
	h := &handler{
		shutdown: shutdown,
	}

	// Hosted mode: per-user schedules live in a flat-file store and are fired
	// by an in-process runner instead of the OS scheduler. Open the store and
	// start the runner; it stops when the server shuts down.
	if config.IsHosted() {
		store, err := schedule.NewStore(config.ScheduleDir())
		if err != nil {
			return fmt.Errorf("opening schedule store: %w", err)
		}
		h.store = store
		go schedule.NewRunner(store, os.Stderr).Start(shutdown)
		fmt.Fprintf(os.Stderr, "[ui] Hosted mode — catalog from %s, schedules in %s\n", config.CatalogPath(), config.ScheduleDir())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.serveIndex)
	mux.HandleFunc("GET /api/config", h.getConfig)
	mux.HandleFunc("POST /api/run", h.runPipeline)
	mux.HandleFunc("POST /api/test-webhook", h.testWebhook)
	mux.HandleFunc("POST /api/send-webhook", h.sendWebhook)
	mux.HandleFunc("POST /api/config/webhook", h.saveWebhook)
	mux.HandleFunc("POST /api/config/query", h.saveQuery)
	mux.HandleFunc("POST /api/exit", h.exit)
	mux.HandleFunc("GET /api/schedule", h.getSchedule)
	mux.HandleFunc("POST /api/schedule/enable", h.enableSchedule)
	mux.HandleFunc("POST /api/schedule/disable", h.disableSchedule)

	addr := fmt.Sprintf(":%d", port)
	fmt.Fprintf(os.Stderr, "[ui] Starting web server at http://localhost%s\n", addr)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Graceful shutdown in background
	go func() {
		<-shutdown
		server.Close()
	}()

	return server.ListenAndServe()
}

// ConfigResponse is the JSON returned by /api/config
type ConfigResponse struct {
	Hosted      bool                `json:"hosted"`       // true in hosted mode — client uses localStorage for per-user prefs
	Repos       []string            `json:"repos"`        // catalog
	Teams       map[string][]string `json:"teams"`        // catalog
	AuthorNames map[string]string   `json:"author_names"` // catalog
	WebhookURL  string              `json:"webhook_url"`
	Days        int                 `json:"days"` // legacy fallback for query.days
	Query       SavedQuery          `json:"query"`
}

// SavedQuery is the persisted "scheduled query" — what the schedule will run.
type SavedQuery struct {
	Repos        []string `json:"repos"`
	Authors      []string `json:"authors"`
	Days         int      `json:"days"`
	Mode         string   `json:"mode"`
	SkipAnalysis bool     `json:"skip_analysis"`
}

func buildConfigResponse() ConfigResponse {
	// Catalogs (from catalog.* in config.yaml — never overwritten by save)
	teams := viper.GetStringMap("catalog.teams")
	teamsClean := make(map[string][]string)
	for name, val := range teams {
		if members, ok := val.([]interface{}); ok {
			var strs []string
			for _, m := range members {
				if s, ok := m.(string); ok {
					strs = append(strs, s)
				}
			}
			teamsClean[name] = strs
		}
	}

	authorNames := make(map[string]string)
	rawNames := viper.GetStringMapString("catalog.author_names")
	for handle, name := range rawNames {
		authorNames[handle] = name
	}

	resp := ConfigResponse{
		Repos:       viper.GetStringSlice("catalog.repos"),
		Teams:       teamsClean,
		AuthorNames: authorNames,
	}

	// Hosted mode: per-user preferences (webhook URL, saved query) live in the
	// browser's localStorage, not server-side. Return the catalog only; the
	// client hydrates the form from localStorage.
	if config.IsHosted() {
		resp.Hosted = true
		return resp
	}

	resp.WebhookURL = viper.GetString("webhook_url")
	resp.Days = viper.GetInt("query.days")
	resp.Query = SavedQuery{
		Repos:        viper.GetStringSlice("query.repos"),
		Authors:      viper.GetStringSlice("query.authors"),
		Days:         viper.GetInt("query.days"),
		Mode:         viper.GetString("query.mode"),
		SkipAnalysis: viper.GetBool("query.skip_analysis"),
	}
	return resp
}
