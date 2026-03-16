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
	Title  string
	DocURL string
	Reason string
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

