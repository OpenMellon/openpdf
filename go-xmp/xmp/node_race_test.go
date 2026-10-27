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
	"sync"
	"testing"
)

// TestNodePoolConcurrent exercises the global node pool from many goroutines.
// Every NewNode/Close bumps the diagnostic counters (npAllocs/npFrees/npHits/
// npReturns); run with -race to detect unsynchronized access to them.
func TestNodePoolConcurrent(t *testing.T) {
	const (
		workers    = 16
		iterations = 5000
	)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				n := NewNode(NewName("test"))
				n.Nodes = append(n.Nodes, NewNode(NewName("child")))
				n.Close()
			}
		}()
	}
	wg.Wait()
}
