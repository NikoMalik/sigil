/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package z

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func verifyHashProduct(t *testing.T, wantKey, wantConflict, key, conflict uint64) {
	require.Equal(t, wantKey, key)
	require.Equal(t, wantConflict, conflict)
}

func TestKeyToHash(t *testing.T) {
	var key uint64
	var conflict uint64

	key, conflict = KeyToHash(uint64(1))
	verifyHashProduct(t, 1, 0, key, conflict)

	key, conflict = KeyToHash(1)
	verifyHashProduct(t, 1, 0, key, conflict)

	key, conflict = KeyToHash(int32(2))
	verifyHashProduct(t, 2, 0, key, conflict)

	key, conflict = KeyToHash(int32(-2))
	verifyHashProduct(t, math.MaxUint64-1, 0, key, conflict)

	key, conflict = KeyToHash(int64(-2))
	verifyHashProduct(t, math.MaxUint64-1, 0, key, conflict)

	key, conflict = KeyToHash(uint32(3))
	verifyHashProduct(t, 3, 0, key, conflict)

	key, conflict = KeyToHash(int64(3))
	verifyHashProduct(t, 3, 0, key, conflict)
}

func benchmarkKeyToHash[K Key](b *testing.B, name string, key K, fn func(K) (uint64, uint64)) {
	b.Run(name, func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			fn(key)
		}
	})
}

type keyToHashFn[K Key] func(K) (uint64, uint64)

// BenchmarkKeyToHash benchmarks both versions of the KeyToHash function for various input types and sizes.
func BenchmarkKeyToHash(b *testing.B) {
	var (
		u64Val     uint64 = 123456789
		strSmall   string = "short"
		strMed     string = string(make([]byte, 100))
		strLarge   string = string(make([]byte, 1000))
		sliceSmall []byte = []byte("short")
		sliceMed   []byte = make([]byte, 100)
		sliceLarge []byte = make([]byte, 1000)
		byteVal    byte   = 42
		intVal     int    = 123456
		int32Val   int32  = 123456
		int64Val   int64  = 123456789
		uint32Val  uint32 = 123456
	)

	// Reflect version
	// benchmarkKeyToHash(b, "Reflect_Uint64", u64Val, keyToHash[uint64])
	// benchmarkKeyToHash(b, "Reflect_StringSmall", strSmall, keyToHash[string])
	// benchmarkKeyToHash(b, "Reflect_StringMedium", strMed, keyToHash[string])
	// benchmarkKeyToHash(b, "Reflect_StringLarge", strLarge, keyToHash[string])
	// benchmarkKeyToHash(b, "Reflect_ByteSliceSmall", sliceSmall, keyToHash[[]byte])
	// benchmarkKeyToHash(b, "Reflect_ByteSliceMedium", sliceMed, keyToHash[[]byte])
	// benchmarkKeyToHash(b, "Reflect_ByteSliceLarge", sliceLarge, keyToHash[[]byte])
	// benchmarkKeyToHash(b, "Reflect_Byte", byteVal, keyToHash[byte])
	// benchmarkKeyToHash(b, "Reflect_Int", intVal, keyToHash[int])
	// benchmarkKeyToHash(b, "Reflect_Int32", int32Val, keyToHash[int32])
	// benchmarkKeyToHash(b, "Reflect_Int64", int64Val, keyToHash[int64])
	// benchmarkKeyToHash(b, "Reflect_Uint32", uint32Val, keyToHash[uint32])
	//
	// TypeAssert version
	benchmarkKeyToHash(b, "TypeAssert_Uint64", u64Val, KeyToHash[uint64])
	benchmarkKeyToHash(b, "TypeAssert_StringSmall", strSmall, KeyToHash[string])
	benchmarkKeyToHash(b, "TypeAssert_StringMedium", strMed, KeyToHash[string])
	benchmarkKeyToHash(b, "TypeAssert_StringLarge", strLarge, KeyToHash[string])
	benchmarkKeyToHash(b, "TypeAssert_ByteSliceSmall", sliceSmall, KeyToHash[[]byte])
	benchmarkKeyToHash(b, "TypeAssert_ByteSliceMedium", sliceMed, KeyToHash[[]byte])
	benchmarkKeyToHash(b, "TypeAssert_ByteSliceLarge", sliceLarge, KeyToHash[[]byte])
	benchmarkKeyToHash(b, "TypeAssert_Byte", byteVal, KeyToHash[byte])
	benchmarkKeyToHash(b, "TypeAssert_Int", intVal, KeyToHash[int])
	benchmarkKeyToHash(b, "TypeAssert_Int32", int32Val, KeyToHash[int32])
	benchmarkKeyToHash(b, "TypeAssert_Int64", int64Val, KeyToHash[int64])
	benchmarkKeyToHash(b, "TypeAssert_Uint32", uint32Val, KeyToHash[uint32])

	//wyhash version
	// benchmarkKeyToHash(b, "WyHashTypeAssert_Uint64", u64Val, keyToHashWyHash[uint64])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_StringSmall", strSmall, keyToHashWyHash[string])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_StringMedium", strMed, keyToHashWyHash[string])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_StringLarge", strLarge, keyToHashWyHash[string])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_ByteSliceSmall", sliceSmall, keyToHashWyHash[[]byte])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_ByteSliceMedium", sliceMed, keyToHashWyHash[[]byte])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_ByteSliceLarge", sliceLarge, keyToHashWyHash[[]byte])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_Byte", byteVal, keyToHashWyHash[byte])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_Int", intVal, keyToHashWyHash[int])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_Int32", int32Val, keyToHashWyHash[int32])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_Int64", int64Val, keyToHashWyHash[int64])
	// benchmarkKeyToHash(b, "WyHashTypeAssert_Uint32", uint32Val, keyToHashWyHash[uint32])

}

func TestMulipleSignals(t *testing.T) {
	closer := NewCloser(0)
	require.NotPanics(t, func() { closer.Signal() })
	// Should not panic.
	require.NotPanics(t, func() { closer.Signal() })
	require.NotPanics(t, func() { closer.SignalAndWait() })

	// Attempt 2.
	closer = NewCloser(1)
	require.NotPanics(t, func() { closer.Done() })

	require.NotPanics(t, func() { closer.SignalAndWait() })
	// Should not panic.
	require.NotPanics(t, func() { closer.SignalAndWait() })
	require.NotPanics(t, func() { closer.Signal() })
}

func TestCloser(t *testing.T) {
	closer := NewCloser(1)
	go func() {
		defer closer.Done()
		<-closer.Ctx().Done()
	}()
	closer.SignalAndWait()
}

func TestZeroOut(t *testing.T) {
	dst := make([]byte, 4*1024)
	fill := func() {
		for i := 0; i < len(dst); i++ {
			dst[i] = 0xFF
		}
	}
	check := func(buf []byte, b byte) {
		for i := 0; i < len(buf); i++ {
			require.Equalf(t, b, buf[i], "idx: %d", i)
		}
	}
	fill()

	ZeroOut(dst, 0, 1)
	check(dst[:1], 0x00)
	check(dst[1:], 0xFF)

	ZeroOut(dst, 0, 1024)
	check(dst[:1024], 0x00)
	check(dst[1024:], 0xFF)

	ZeroOut(dst, 0, len(dst))
	check(dst, 0x00)
}
