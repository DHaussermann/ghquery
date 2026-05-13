package github

import "time"

type FileDiff struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
	Truncated bool   `json:"truncated,omitempty"`
}

type PRData struct {
	PRNumber    int          `json:"pr_number"`
	PRState     string       `json:"pr_state"`
	PRURL       string       `json:"pr_url"`
	Title       string       `json:"title"`
	Author      string       `json:"author"`
	Repo        string       `json:"repo"`
	HeadSHA     string       `json:"head_sha"`
	Date        time.Time    `json:"date"`
	Stats       PRStats      `json:"stats"`
	BlastRadius BlastRadius  `json:"blast_radius"`
	Approvers   []string     `json:"approvers"`
	Files       []FileDiff   `json:"files"`

	// CodeRabbit fields — populated only when FetchOptions.FetchCodeRabbit
	// is set AND the PR has a parseable CodeRabbit "Change Impact" comment.
	// When CodeRabbitImpact is non-empty, the analysis layer skips the
	// agent passes and uses these fields directly.
	CodeRabbitImpact string `json:"coderabbit_impact,omitempty"` // "HIGH" / "MEDIUM" / "LOW"
	CodeRabbitReason string `json:"coderabbit_reason,omitempty"`
	CodeRabbitQA     string `json:"coderabbit_qa,omitempty"`
}

type BlastRadius struct {
	FilesChanged int      `json:"files_changed"`
	DirsChanged  int      `json:"dirs_changed"`
	AreasAffected []string `json:"areas_affected"`
	TotalLines   int      `json:"total_lines"`
	CrossArea    bool     `json:"cross_area"`
}

type PRStats struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Total     int `json:"total"`
}

type FetchOptions struct {
	Repos           []string
	Authors         []string
	Since           time.Time
	Until           time.Time
	FetchCodeRabbit bool // when true, also pull PR issue comments and parse out CodeRabbit's Change Impact summary
}

type FetchResult struct {
	PRs           []PRData `json:"prs"`
	APICallsMade  int      `json:"api_calls_made"`
	RateRemaining int      `json:"rate_remaining"`
}
