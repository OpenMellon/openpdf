// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package sigattr

import "encoding/asn1"

type RevocationInfoArchival struct {
	Crl          []asn1.RawValue `asn1:"explicit,tag:0,optional"`
	Ocsp         []asn1.RawValue `asn1:"explicit,tag:1,optional"`
	OtherRevInfo []asn1.RawValue `asn1:"explicit,tag:2,optional"`
}
