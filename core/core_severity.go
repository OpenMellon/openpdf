package core

// Severity classifies a linearization violation as a hard error or a warning.
// The ISO 32000-2 Annex F checks differ in how strictly they bear on whether a
// PDF works in Fast Web View: some violations break it outright (Error), others
// only degrade or signal staleness (Warning).
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)
