// Copyright (c) 2017-2018 Alexander Eichhorn
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package xmp

import (
	"strings"
	"testing"
	"time"
)

func TestSanitizeLogValue(t *testing.T) {
	if got := sanitizeLogValue("bad\r\nvalue\ninjected"); strings.ContainsAny(got, "\r\n") {
		t.Errorf("sanitizeLogValue(%q) still contains CR/LF: %q", "bad\r\nvalue\ninjected", got)
	}
}

func TestParseDateLenient(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		{"loose-tz-fractional", "2019-12-24T15:49:34.000+03", time.Date(2019, 12, 24, 15, 49, 34, 0, time.FixedZone("", 3*3600))},
		{"loose-tz", "2019-12-24T15:49:34+03", time.Date(2019, 12, 24, 15, 49, 34, 0, time.FixedZone("", 3*3600))},
		{"pdf-single-digit-hour", "D:20200309102906+3'00'", time.Date(2020, 3, 9, 10, 29, 6, 0, time.FixedZone("", 3*3600))},
		{"pdf-full-offset", "D:20200309102906+03'00'", time.Date(2020, 3, 9, 10, 29, 6, 0, time.FixedZone("", 3*3600))},
		{"pdf-negative-offset", "D:20200309102906-05'00'", time.Date(2020, 3, 9, 10, 29, 6, 0, time.FixedZone("", -5*3600))},
		{"pdf-utc", "D:20200309102906Z", time.Date(2020, 3, 9, 10, 29, 6, 0, time.UTC)},
		{"pdf-no-offset", "D:20200309102906", time.Date(2020, 3, 9, 10, 29, 6, 0, time.UTC)},
		{"pdf-year-only", "D:2020", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"pdf-overlong-digits", "D:2020030910290612345+03'00'", time.Date(2020, 3, 9, 10, 29, 6, 0, time.FixedZone("", 3*3600))},
		{"pdf-truncated-component", "D:20200309102", time.Date(2020, 3, 9, 10, 0, 0, 0, time.UTC)},
		{"rfc3339", "2020-03-09T10:29:06Z", time.Date(2020, 3, 9, 10, 29, 6, 0, time.UTC)},
		{"date-only", "2019-12-24", time.Date(2019, 12, 24, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := ParseDate(c.in)
			if err != nil {
				t.Fatalf("ParseDate(%q) unexpected error: %v", c.in, err)
			}
			if !d.Value().Equal(c.want) {
				t.Errorf("ParseDate(%q) = %v, want %v", c.in, d.Value(), c.want)
			}
		})
	}
}

func TestParseDateInvalid(t *testing.T) {
	for _, in := range []string{"garbage", "not-a-date", "D:garbage", "D:"} {
		if _, err := ParseDate(in); err == nil {
			t.Errorf("ParseDate(%q) = nil error, want error", in)
		}
	}
}
