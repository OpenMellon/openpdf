// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package sigattr ;import _d "encoding/asn1";type RevocationInfoArchival struct{Crl []_d .RawValue `asn1:"explicit,tag:0,optional"`;Ocsp []_d .RawValue `asn1:"explicit,tag:1,optional"`;OtherRevInfo []_d .RawValue `asn1:"explicit,tag:2,optional"`;};