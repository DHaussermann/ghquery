package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/viper"

	"github.com/DHaussermann/ghquery/internal/schedule"
)

// scheduleStateResponse is the JSON returned by GET /api/schedule.
type scheduleStateResponse struct {
	Enabled   bool                 `json:"enabled"`
	Time      string               `json:"time"`
	Frequency string               `json:"frequency"`
	Weekday   string               `json:"weekday"`
	TZ        string               `json:"tz"`
	Status    scheduleStatusOutput `json:"status"`
}

type scheduleStatusOutput struct {
	Installed bool   `json:"installed"`
	Detail    string `json:"detail"`
}

// scheduleEnableRequest is the JSON body for POST /api/schedule/enable.
type scheduleEnableRequest struct {
	Time      string `json:"time"`
	Frequency string `json:"frequency"`
	Weekday   string `json:"weekday"`
	TZ        string `json:"tz"`
}

type scheduleEnableResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (h *handler) getSchedule(w http.ResponseWriter, r *http.Request) {
	scheduler := schedule.NewScheduler()
	st, err := scheduler.Status()
	if err != nil {
		st = schedule.Status{Installed: false, Detail: err.Error()}
	}

	resp := scheduleStateResponse{
		Enabled:   viper.GetBool("schedule.enabled"),
		Time:      viper.GetString("schedule.time"),
		Frequency: viper.GetString("schedule.frequency"),
		Weekday:   viper.GetString("schedule.weekday"),
		TZ:        viper.GetString("schedule.tz"),
		Status: scheduleStatusOutput{
			Installed: st.Installed,
			Detail:    st.Detail,
		},
	}

	if resp.Frequency == "" {
		resp.Frequency = "daily"
	}
	if resp.Time == "" {
		resp.Time = "08:00"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *handler) enableSchedule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req scheduleEnableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeScheduleError(w, "invalid JSON body: "+err.Error())
		return
	}

	if req.Time == "" {
		writeScheduleError(w, "time is required")
		return
	}
	if req.Frequency == "" {
		req.Frequency = "daily"
	}

	// Build the Schedule struct
	sched, err := buildScheduleStruct(req)
	if err != nil {
		writeScheduleError(w, err.Error())
		return
	}

	// Install via the OS scheduler
	scheduler := schedule.NewScheduler()
	if err := scheduler.Install(sched); err != nil {
		writeScheduleError(w, "OS scheduler install failed: "+err.Error())
		return
	}

	// Persist to config.yaml
	viper.Set("schedule.enabled", true)
	viper.Set("schedule.time", req.Time)
	viper.Set("schedule.frequency", req.Frequency)
	viper.Set("schedule.weekday", req.Weekday)
	if req.TZ != "" {
		viper.Set("schedule.tz", req.TZ)
	}
	if err := viper.WriteConfig(); err != nil {
		// Schedule installed but config write failed — surface but don't roll back
		writeScheduleError(w, "schedule installed, but writing config.yaml failed: "+err.Error())
		return
	}

	json.NewEncoder(w).Encode(scheduleEnableResponse{OK: true})
}

func (h *handler) disableSchedule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	scheduler := schedule.NewScheduler()
	if err := scheduler.Remove(); err != nil {
		writeScheduleError(w, "OS scheduler remove failed: "+err.Error())
		return
	}

	viper.Set("schedule.enabled", false)
	if err := viper.WriteConfig(); err != nil {
		writeScheduleError(w, "schedule removed, but writing config.yaml failed: "+err.Error())
		return
	}

	json.NewEncoder(w).Encode(scheduleEnableResponse{OK: true})
}

func writeScheduleError(w http.ResponseWriter, msg string) {
	json.NewEncoder(w).Encode(scheduleEnableResponse{OK: false, Error: msg})
}

// buildScheduleStruct constructs a schedule.Schedule from request fields,
// resolving binary path, working directory, and log directory.
func buildScheduleStruct(req scheduleEnableRequest) (schedule.Schedule, error) {
	binary, err := os.Executable()
	if err != nil {
		return schedule.Schedule{}, fmt.Errorf("resolving binary path: %w", err)
	}
	binary, _ = filepath.Abs(binary)

	workDir, err := os.Getwd()
	if err != nil {
		return schedule.Schedule{}, fmt.Errorf("resolving working dir: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return schedule.Schedule{}, fmt.Errorf("resolving home dir: %w", err)
	}
	logDir := filepath.Join(home, ".ghquery", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return schedule.Schedule{}, fmt.Errorf("creating log dir: %w", err)
	}

	return schedule.Schedule{
		Time:      req.Time,
		Frequency: schedule.Frequency(req.Frequency),
		Weekday:   req.Weekday,
		Binary:    binary,
		WorkDir:   workDir,
		LogDir:    logDir,
	}, nil
}
