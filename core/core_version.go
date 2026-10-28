package core

import (
	"fmt"
)

// Version represents a version of a PDF standard.
type Version struct {
	Major int
	Minor int
}

// String returns the PDF version as a string. Implements interface fmt.Stringer.
func (version Version) String() string { return fmt.Sprintf("%0d.%0d", version.Major, version.Minor) }
