// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package security

// StdEncryptDict is a set of additional fields used in standard encryption dictionary.
type StdEncryptDict struct {
	R               int
	P               Permissions
	EncryptMetadata bool
	O, U            []byte
	OE, UE          []byte
	Perms           []byte
}

// StdHandler is an interface for standard security handlers.
type StdHandler interface {
	// GenerateParams uses owner and user passwords to set encryption parameters and generate an encryption key.
	// It assumes that R, P and EncryptMetadata are already set.
	GenerateParams(d *StdEncryptDict, opass, upass []byte) ([]byte, error)

	// Authenticate uses encryption dictionary parameters and the password to calculate
	// the document encryption key. It also returns permissions that should be granted to a user.
	// In case of failed authentication, it returns an empty key with no error.
	// Permissions may be zero even when authentication succeeds.
	Authenticate(d *StdEncryptDict, pass []byte) ([]byte, Permissions, error)
}
