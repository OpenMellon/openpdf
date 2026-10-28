package security

import (
	"math"
)

const (
	PermOwner             = Permissions(math.MaxUint32)
	PermPrinting          = Permissions(1 << 2)
	PermModify            = Permissions(1 << 3)
	PermExtractGraphics   = Permissions(1 << 4)
	PermAnnotate          = Permissions(1 << 5)
	PermFillForms         = Permissions(1 << 8)
	PermDisabilityExtract = Permissions(1 << 9)
	PermRotateInsert      = Permissions(1 << 10)
	PermFullPrintQuality  = Permissions(1 << 11)
)

// Normalized returns p with the reserved bits of the P entry set to the
// values required by the PDF specification.
func (p Permissions) Normalized() Permissions { return (p | permReservedOnes) &^ permReservedZeros }

const (
	EventDocOpen = AuthEvent("DocOpen")
	EventEFOpen  = AuthEvent("EFOpen")
)

const (
	permReservedOnes  = Permissions(0xFFFFF0C0)
	permReservedZeros = Permissions(0x3)
)

// Permissions is a bitmask of access permissions for a PDF file.
type Permissions uint32

// AuthEvent is an event type that triggers authentication.
type AuthEvent string

// Allowed checks if a set of permissions can be granted.
func (p Permissions) Allowed(p2 Permissions) bool { return p&p2 == p2 }
