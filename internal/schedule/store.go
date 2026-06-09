package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Record is a single user's saved recurring query plus its schedule timing,
// persisted as one flat JSON file per UUID in hosted mode. It is the complete
// recipe the in-process runner needs to fire a scheduled run while the user's
// browser is closed — including the webhook URL it delivers to.
//
// There is no database: the Store is a directory of <uuid>.json files.
type Record struct {
	UUID string `json:"uuid"`

	// Recipe — what to query (set by the "save recurring query" action).
	Repos         []string `json:"repos"`
	Authors       []string `json:"authors"`
	Days          int      `json:"days"`
	Mode          string   `json:"mode"`
	SkipAnalysis  bool     `json:"skip_analysis"`
	UseCodeRabbit bool     `json:"use_coderabbit"`
	WebhookURL    string   `json:"webhook_url"`

	// Timing — when to run (set by the schedule modal).
	Time      string `json:"time"`      // "HH:MM" 24h, interpreted in TZ
	Frequency string `json:"frequency"` // daily | weekdays | weekly
	Weekday   string `json:"weekday"`   // only when Frequency == weekly
	TZ        string `json:"tz"`        // IANA name, e.g. America/New_York
	Enabled   bool   `json:"enabled"`
}

// Store is a directory of per-UUID schedule records. Safe for concurrent use
// by the web handlers and the runner because each operation reads or writes a
// whole file atomically; callers serialize their own load-modify-save cycles.
type Store struct {
	dir string
}

// NewStore returns a Store rooted at dir, creating the directory if needed.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating schedule dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// uuidPattern guards against path traversal — record keys must look like a
// browser-generated UUID/token (hex, digits, dashes), nothing else.
var uuidPattern = regexp.MustCompile(`^[A-Za-z0-9\-]{8,128}$`)

func (s *Store) path(uuid string) (string, error) {
	uuid = strings.TrimSpace(uuid)
	if !uuidPattern.MatchString(uuid) {
		return "", fmt.Errorf("invalid uuid %q", uuid)
	}
	return filepath.Join(s.dir, uuid+".json"), nil
}

// Load returns the record for uuid, or (nil, nil) if none exists yet.
func (s *Store) Load(uuid string) (*Record, error) {
	p, err := s.path(uuid)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading record: %w", err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parsing record %s: %w", uuid, err)
	}
	return &rec, nil
}

// Save writes rec atomically (temp file + rename) to avoid a half-written
// record being read by the runner mid-write.
func (s *Store) Save(rec *Record) error {
	p, err := s.path(rec.UUID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling record: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing record: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("committing record: %w", err)
	}
	return nil
}

// Delete removes the record for uuid. Missing records are not an error.
func (s *Store) Delete(uuid string) error {
	p, err := s.path(uuid)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting record: %w", err)
	}
	return nil
}

// List returns every stored record. Unparseable files are skipped rather than
// failing the whole listing — one corrupt record shouldn't stop the runner.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing schedule dir: %w", err)
	}
	var records []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var rec Record
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, nil
}
