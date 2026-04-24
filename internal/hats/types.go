package hats

// Posture is one of the reasoning postures declared in hats.md.
type Posture string

const (
	PostureCausalHypothesis  Posture = "causal-hypothesis"
	PostureWorkaroundMenu    Posture = "ambiguous-workaround-menu"
	PostureCausalNarrative   Posture = "internal-regression"
	PostureDemandGating      Posture = "demand-gating-needed"
	PostureConfigDependent   Posture = "config-dependent"
	PostureBlockedOnUpstream Posture = "blocked-on-upstream"
)

// Hat is one class entry in the taxonomy.
type Hat struct {
	Name                   string
	WhenToPick             string
	RetrievalLabels        []string
	RetrievalBoostKeywords []string
	Posture                Posture
	Phase1Asks             string
	// AnchorIssueNumbers collects every `#NNN` reference that appears in the
	// Anchors paragraph, including PR numbers cited in parentheticals. Issue
	// and PR numbers share a namespace on GitHub, so downstream consumers can
	// treat these uniformly — but they are not a curated subset of issues.
	AnchorIssueNumbers []int
}

// Taxonomy is the parsed content of a hats.md file.
type Taxonomy struct {
	Preamble string
	Hats     []Hat
}

// Find returns the hat with the given name, or nil.
func (t Taxonomy) Find(name string) *Hat {
	for i := range t.Hats {
		if t.Hats[i].Name == name {
			return &t.Hats[i]
		}
	}
	return nil
}
