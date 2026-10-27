// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package license is a no-op stand-in for the former licence-key system. The library is open source and needs no key.
package license

// LicenseKey is retained so existing call sites keep compiling; every key is treated as licensed.
type LicenseKey struct{}

// IsLicensed always reports true.
func (k *LicenseKey) IsLicensed() bool { return true }

// GetLicenseKey returns a key that is always licensed.
func GetLicenseKey() *LicenseKey { return &LicenseKey{} }

// Track is a no-op.
func Track(docKey, useKey, docName string) error { return nil }

// TrackUse is a no-op.
func TrackUse(useKey string) {}

// TypeToString returns an empty tier name.
func (k *LicenseKey) TypeToString() string { return "" }
