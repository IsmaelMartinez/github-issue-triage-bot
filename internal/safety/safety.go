package safety

// ValidationResult holds the outcome of a content validation check.
type ValidationResult struct {
	Passed     bool
	Reason     string
	Confidence float32
}

// Validator checks content against safety rules.
type Validator interface {
	Validate(content string) ValidationResult
}
