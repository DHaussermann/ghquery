package analysis

type RiskLevel string

const (
	RiskHigh    RiskLevel = "HIGH"
	RiskMedium  RiskLevel = "MEDIUM"
	RiskLow     RiskLevel = "LOW"
	RiskUnknown RiskLevel = "UNKNOWN"
)

type RiskDimensions struct {
	BlastRadius       int `json:"blast_radius"`
	Complexity        int `json:"complexity"`
	RegressionSurface int `json:"regression_surface"`
	DataIntegrity     int `json:"data_integrity"`
	SecuritySurface   int `json:"security_surface"`
	InfraConfig       int `json:"infra_config"`
}

type RiskSummary struct {
	TotalPRs      int       `json:"total_prs"`
	HighRisk      int       `json:"high_risk"`
	MediumRisk    int       `json:"medium_risk"`
	LowRisk       int       `json:"low_risk"`
	Unknown       int       `json:"unknown,omitempty"`
	ReposAffected []string  `json:"repos_affected"`
	OverallRisk   RiskLevel `json:"overall_risk"`
}

type PRRisk struct {
	PRNumber          int            `json:"pr_number"`
	PRURL             string         `json:"pr_url"`
	Repo              string         `json:"repo"`
	Author            string         `json:"author"`
	Title             string         `json:"title"`
	PRState           string         `json:"pr_state"`
	IsUntested        bool           `json:"is_untested"` // merged with no QA team approval
	RiskLevel         RiskLevel      `json:"risk_level"`
	RiskScore         float64        `json:"risk_score"`
	Dimensions        RiskDimensions `json:"dimensions"`
	RiskReason        string         `json:"risk_reason"`
	AreasAffected     []string       `json:"areas_affected"`
	QARecommendations []string       `json:"qa_recommendations"`
	TestApproach      string         `json:"test_approach"`
	Source            string         `json:"source,omitempty"` // "" / "agent" (default) or "coderabbit"
	Error             string         `json:"error,omitempty"`
}

type RiskReport struct {
	Summary RiskSummary `json:"summary"`
	PRs     []PRRisk    `json:"prs"`
}

// BranchEnumeration is the output of Pass A — a structured list of new
// code branches and what the test changes cover. It is fed into Pass B as
// pre-computed context so the scoring agent doesn't have to derive the
// branch list from the diff itself.
type BranchEnumeration struct {
	NewBranches []BranchInfo `json:"new_branches"`
	TestChanges []TestInfo   `json:"test_changes"`
}

type BranchInfo struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

type TestInfo struct {
	Path   string `json:"path"`
	Covers string `json:"covers"`
}
