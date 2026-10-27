// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package lazystream

var Take func(owner, key any) (data []byte, ok bool)
var Drop func(owner any)
