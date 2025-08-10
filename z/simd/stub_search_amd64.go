//go:build amd64
// +build amd64

package simd

import (
	"golang.org/x/sys/cpu"
)

func HasAVX2() bool {
	return cpu.X86.HasAVX2
}

//go:noescape
func SearchSimdBinary(xs []uint64, k uint64) int16

// Search finds the first idx for which xs[idx] >= k in xs.

//go:noescape
func SearchUnroll(xs []uint64, k uint64) int16

//go:noescape
func SearchSimd(xs []uint64, k uint64) int16

func SearchBinary(xs []uint64, k uint64) int16 {
	if !HasAVX2() {
		return SearchUnroll(xs, k)
	}
	return SearchSimdBinary(xs, k)
}

func Search(xs []uint64, k uint64) int16 {
	// if !HasAVX2() {
	// return SearchUnroll(xs, k)
	// }
	return SearchSimd(xs, k)

}
