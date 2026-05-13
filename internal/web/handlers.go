package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/DHaussermann/ghquery/internal/analysis"
	"github.com/DHaussermann/ghquery/internal/output"
	"github.com/DHaussermann/ghquery/internal/pipeline"
)

type handler struct {
	shutdown chan struct{}
}

func (h *handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index.html not found", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (h *handler) getConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildConfigResponse())
}

type runRequest struct {
	Mode          string   `json:"mode"`
	Repos         []string `json:"repos"`
	Authors       []string `json:"authors"`
	Days          int      `json:"days"`
	WebhookURL    string   `json:"webhook_url"`
	SkipAnalysis  bool     `json:"skip_analysis"`
	SkipWebhook   bool     `json:"skip_webhook"`
	PRUrl         string   `json:"pr_url"`
	UseCodeRabbit bool     `json:"use_coderabbit"`
}

func (h *handler) runPipeline(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", 400)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	sse := &sseWriter{w: w, flusher: flusher}

	params := pipeline.RunParams{
		Repos:         req.Repos,
		Authors:       req.Authors,
		Days:          req.Days,
		Mode:          req.Mode,
		SkipAnalysis:  req.SkipAnalysis,
		SkipWebhook:   req.SkipWebhook,
		WebhookURL:    req.WebhookURL,
		PRUrl:         req.PRUrl,
		UseCodeRabbit: req.UseCodeRabbit,
	}

	start := time.Now()
	report, err := pipeline.Execute(
		context.Background(),
		params,
		sse,
		viper.GetString("github_token"),
		viper.GetStringMap("catalog.teams"),
	)

	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		fmt.Fprintf(w, "event: done\ndata: \n\n")
		flusher.Flush()
		return
	}

	if report != nil {
		fmt.Fprintf(sse, "[done] Report ready in %s — %d PRs\n", time.Since(start).Round(time.Second), len(report.PRs))
		reportJSON, _ := json.Marshal(report)
		fmt.Fprintf(w, "event: report\ndata: %s\n\n", string(reportJSON))
		flusher.Flush()
	}

	fmt.Fprintf(w, "event: done\ndata: \n\n")
	flusher.Flush()
}

type testWebhookRequest struct {
	URL string `json:"url"`
}

type testWebhookResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (h *handler) testWebhook(w http.ResponseWriter, r *http.Request) {
	var req testWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if req.URL == "" {
		json.NewEncoder(w).Encode(testWebhookResponse{OK: false, Error: "No URL provided"})
		return
	}

	err := output.CheckWebhook(req.URL)
	if err != nil {
		json.NewEncoder(w).Encode(testWebhookResponse{OK: false, Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(testWebhookResponse{OK: true})
}

type sendWebhookRequest struct {
	URL    string                  `json:"url"`
	Report *analysis.RiskReport    `json:"report"`
}

type sendWebhookResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (h *handler) sendWebhook(w http.ResponseWriter, r *http.Request) {
	var req sendWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if req.URL == "" {
		json.NewEncoder(w).Encode(sendWebhookResponse{OK: false, Error: "No webhook URL provided"})
		return
	}
	if req.Report == nil {
		json.NewEncoder(w).Encode(sendWebhookResponse{OK: false, Error: "No report payload provided"})
		return
	}

	// Send directly — SendWebhook surfaces the actual server error if delivery fails.
	// (We don't pre-check with CheckWebhook because that posts an extra test message.)
	if err := output.SendWebhook(req.Report, req.URL, false); err != nil {
		// Failover: save the report to disk so it isn't lost.
		errMsg := err.Error()
		if savedPath, saveErr := pipeline.SaveReportFallback(req.Report); saveErr == nil {
			errMsg += fmt.Sprintf(" (report saved to %s)", savedPath)
		}
		json.NewEncoder(w).Encode(sendWebhookResponse{OK: false, Error: errMsg})
		return
	}

	json.NewEncoder(w).Encode(sendWebhookResponse{OK: true})
}

// ── Config save handlers ─────────────────────────────────────────────

type saveResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type webhookSaveRequest struct {
	WebhookURL string `json:"webhook_url"`
}

func (h *handler) saveWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req webhookSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(saveResponse{OK: false, Error: "invalid JSON: " + err.Error()})
		return
	}

	viper.Set("webhook_url", req.WebhookURL)
	if err := viper.WriteConfig(); err != nil {
		json.NewEncoder(w).Encode(saveResponse{OK: false, Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(saveResponse{OK: true})
}

type querySaveRequest struct {
	Repos        []string `json:"repos"`
	Authors      []string `json:"authors"`
	Days         int      `json:"days"`
	Mode         string   `json:"mode"`
	SkipAnalysis bool     `json:"skip_analysis"`
}

func (h *handler) saveQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req querySaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(saveResponse{OK: false, Error: "invalid JSON: " + err.Error()})
		return
	}

	// Save under query.* — separate namespace from the top-level catalog keys
	// so saving a selection doesn't wipe the available repos/teams menus.
	if len(req.Repos) > 0 {
		viper.Set("query.repos", req.Repos)
	}
	if len(req.Authors) > 0 {
		// Resolve any team names in the input to their member usernames so the
		// saved query is always a flat list of GitHub handles (unambiguous).
		resolved := pipeline.ResolveAuthors(req.Authors, viper.GetStringMap("catalog.teams"), io.Discard)
		viper.Set("query.authors", resolved)
	}
	if req.Days > 0 {
		viper.Set("query.days", req.Days)
	}
	if req.Mode != "" {
		viper.Set("query.mode", req.Mode)
	}
	// Always set skip_analysis (false is meaningful — user unchecking it should persist)
	viper.Set("query.skip_analysis", req.SkipAnalysis)

	if err := viper.WriteConfig(); err != nil {
		json.NewEncoder(w).Encode(saveResponse{OK: false, Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(saveResponse{OK: true})
}

func (h *handler) exit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"shutting down"}`))

	go func() {
		close(h.shutdown)
	}()
}

// sseWriter implements io.Writer — each Write() call sends an SSE log event.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func (s *sseWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	text := strings.TrimRight(string(p), "\n")
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fmt.Fprintf(s.w, "event: log\ndata: %s\n\n", line)
	}
	s.flusher.Flush()
	return len(p), nil
}
