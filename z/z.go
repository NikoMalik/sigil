/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package z

import (
	"context"
	"sync"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	"github.com/goccy/go-reflect"
	"github.com/zhangyunhao116/wyhash"
)

type Key interface {
	uint64 | string | []byte | byte | int | int32 | uint32 | int64
}

func keyToHash[K Key](key K) (uint64, uint64) {
	t := reflect.ValueNoEscapeOf(key)
	switch t.Kind() {
	case reflect.Uint64:
		return *(*uint64)(unsafe.Pointer(&key)), 0
	case reflect.String:
		s := *(*string)(unsafe.Pointer(&key))
		return MemHashString(s), xxhash.Sum64String(s)
	case reflect.Slice:
		val := reflect.ValueOf(key)
		if val.Type().Elem().Kind() == reflect.Uint8 {
			b := val.Bytes()
			return MemHash(b), xxhash.Sum64(b)
		}
	case reflect.Uint8:
		return uint64(*(*byte)(unsafe.Pointer(&key))), 0
	case reflect.Int, reflect.Int32, reflect.Int64:
		return uint64(reflect.ValueNoEscapeOf(key).Int()), 0
	case reflect.Uint32:
		return uint64(reflect.ValueNoEscapeOf(key).Uint()), 0
	default:
		panic("unsupported type")
	}
	panic("unsupported")
}

const DefaultSeed = 0xa0761d6478bd642f

func KeyToHash[K Key](key K) (uint64, uint64) {
	switch k := any(key).(type) {
	case uint64:
		return k, 0
	case string:
		ptr := unsafe.Pointer(unsafe.StringData(k))
		size := uintptr(len(k))
		h1 := MemHashString(k)                // first hash
		h2 := memhash(ptr, DefaultSeed, size) // secondhash
		return uint64(h1), uint64(h2)
	case []byte:
		var ptr unsafe.Pointer
		if len(k) > 0 {
			ptr = unsafe.Pointer(&k[0])
		}
		size := uintptr(len(k))
		h1 := MemHash(k)                      // first hash
		h2 := memhash(ptr, DefaultSeed, size) // second hash
		return uint64(h1), uint64(h2)
	case byte:
		return uint64(k), 0
	case int:
		return uint64(k), 0
	case int32:
		return uint64(k), 0
	case uint32:
		return uint64(k), 0
	case int64:
		return uint64(k), 0
	default:
		panic("Key type not supported")
	}
}

// wyhash faster version
func keyToHashWyHash[K Key](key K) (uint64, uint64) {
	keyAsAny := any(key)
	switch k := keyAsAny.(type) {
	case uint64:
		return k, 0
	case string:
		return MemHashString(k), wyhash.Sum64String(k)
	case []byte:
		return MemHash(k), wyhash.Sum64(k)
	case byte:
		return uint64(k), 0
	case int:
		return uint64(k), 0
	case int32:
		return uint64(k), 0
	case uint32:
		return uint64(k), 0
	case int64:
		return uint64(k), 0
	default:
		panic("Key type not supported")
	}
}

// func KeyToHash[K Key](key K) (uint64, uint64) {
// 	keyAsAny := any(key)
// 	switch k := keyAsAny.(type) {
// 	case uint64:
// 		return k, 0
// 	case string:
// 		return MemHashString(k), xxhash.Sum64String(k)
// 	case []byte:
// 		return MemHash(k), xxhash.Sum64(k)
// 	case byte:
// 		return uint64(k), 0
// 	case int:
// 		return uint64(k), 0
// 	case int32:
// 		return uint64(k), 0
// 	case uint32:
// 		return uint64(k), 0
// 	case int64:
// 		return uint64(k), 0
// 	default:
// 		panic("Key type not supported")
// 	}
// }

var (
	dummyCloserChan <-chan struct{}
	tmpDir          string
)

// Closer holds the two things we need to close a goroutine and wait for it to
// finish: a chan to tell the goroutine to shut down, and a WaitGroup with
// which to wait for it to finish shutting down.
type Closer struct {
	waiting sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc
}

// SetTmpDir sets the temporary directory for the temporary buffers.
func SetTmpDir(dir string) {
	tmpDir = dir
}

// NewCloser constructs a new Closer, with an initial count on the WaitGroup.
func NewCloser(initial int) *Closer {
	ret := &Closer{}
	ret.ctx, ret.cancel = context.WithCancel(context.Background())
	ret.waiting.Add(initial)
	return ret
}

// AddRunning Add()'s delta to the WaitGroup.
func (lc *Closer) AddRunning(delta int) {
	lc.waiting.Add(delta)
}

// Ctx can be used to get a context, which would automatically get cancelled when Signal is called.
func (lc *Closer) Ctx() context.Context {
	if lc == nil {
		return context.Background()
	}
	return lc.ctx
}

// Signal signals the HasBeenClosed signal.
func (lc *Closer) Signal() {
	// Todo(ibrahim): Change Signal to return error on next badger breaking change.
	lc.cancel()
}

// HasBeenClosed gets signaled when Signal() is called.
func (lc *Closer) HasBeenClosed() <-chan struct{} {
	if lc == nil {
		return dummyCloserChan
	}
	return lc.ctx.Done()
}

// Done calls Done() on the WaitGroup.
func (lc *Closer) Done() {
	if lc == nil {
		return
	}
	lc.waiting.Done()
}

// Wait waits on the WaitGroup. (It waits for NewCloser's initial value, AddRunning, and Done
// calls to balance out.)
func (lc *Closer) Wait() {
	lc.waiting.Wait()
}

// SignalAndWait calls Signal(), then Wait().
func (lc *Closer) SignalAndWait() {
	lc.Signal()
	lc.Wait()
}

// ZeroOut zeroes out all the bytes in the range [start, end).
func ZeroOut(dst []byte, start, end int) {
	if start < 0 || start >= len(dst) {
		return // BAD
	}
	if end >= len(dst) {
		end = len(dst)
	}
	if end-start <= 0 {
		return
	}
	Memclr(dst[start:end])
	// b := dst[start:end]
	// for i := range b {
	// 	b[i] = 0x0
	// }
}
