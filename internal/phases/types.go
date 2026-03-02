package phases

// Phase1Result contains missing information items and PWA reproducibility status.
type Phase1Result struct {
	MissingItems      []MissingItem
	IsPwaReproducible bool
}

// MissingItem describes a missing piece of information in an issue report.
type MissingItem struct {
	Label  string
	Detail string
}

// Suggestion represents a documentation match from Phase 2.
type Suggestion struct {
	Title         string
	DocURL        string
	Reason        string
	ActionableStep string
}

// Duplicate represents a potential duplicate issue from Phase 3.
type Duplicate struct {
	Number     int
	Title      string
	State      string
	Reason     string
	Similarity int
	ClosedAt   *string
	Milestone  *string
}

// ContextMatch represents a feature/ADR/research match from Phase 4a.
type ContextMatch struct {
	Topic       string
	Status      string
	DocURL      string
	Source      string
	LastUpdated *string
	Reason      string
	IsInfeasible bool
}

// Misclassification represents a label suggestion from Phase 4b.
type Misclassification struct {
	SuggestedLabel string
	Confidence     int
	Reason         string
}
