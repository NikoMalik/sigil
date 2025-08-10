/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package simd

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearch(t *testing.T) {
	keys := make([]uint64, 512)
	for i := 0; i < len(keys); i += 2 {
		keys[i] = uint64(i)
		keys[i+1] = 1
	}

	for i := 0; i < len(keys); i++ {
		idx := int(Search(keys, uint64(i)))
		require.Equal(t, (i+1)/2, idx, "%v\n%v", i, keys)
	}
	require.Equal(t, 256, int(Search(keys, math.MaxInt64>>1)))
	require.Equal(t, 256, int(Search(keys, math.MaxInt64)))
}

func BenchmarkSearch(b *testing.B) {
	// Define different array sizes to test
	sizes := []int{64, 512, 4096, 32768}

	for _, size := range sizes {
		// Create input array
		keys := make([]uint64, size)
		for i := 0; i < len(keys); i += 2 {
			keys[i] = uint64(i)
			if i+1 < len(keys) {
				keys[i+1] = 1
			}
		}

		// Test cases: key at start, middle, end, and not found
		testCases := []struct {
			name string
			key  uint64
		}{
			{name: "KeyAtStart", key: 0},                 // Found at index 0
			{name: "KeyAtMiddle", key: uint64(size / 2)}, // Found at middle
			{name: "KeyAtEnd", key: uint64(size - 2)},    // Found at last even index
			{name: "KeyNotFound", key: math.MaxUint64},   // Not in array
		}

		for _, tc := range testCases {
			b.Run(

				"Search_Size="+strconv.Itoa(size)+"Simd"+"_"+tc.name,
				func(b *testing.B) {
					// Run the benchmark
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						SearchSimd(keys, tc.key)
					}
				})
			b.Run(
				"Search_Size="+strconv.Itoa(size)+"Unroll"+"_"+tc.name,
				func(b *testing.B) {
					// Run the benchmark
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						SearchUnroll(keys, tc.key)
					}
				})

		}

	}
}
