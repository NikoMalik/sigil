/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package ristretto

import (
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NikoMalik/sigil/z"
	"github.com/stretchr/testify/require"
)

var wait = time.Millisecond * 10

func TestCacheKeyToHash(t *testing.T) {

	keyToHashCount := 0
	c, err := NewCache(&Config[int, int]{
		NumCounters:        10,
		MaxCost:            1000,
		BufferItems:        64,
		IgnoreInternalCost: true,
		KeyToHash: func(key int) (uint64, uint64) {
			keyToHashCount++
			return z.KeyToHash(key)
		},
	})
	require.NoError(t, err)
	if c.Set(1, 1, 1) {
		time.Sleep(wait)
		val, ok := c.Get(1)
		require.True(t, ok)
		require.NotNil(t, val)
		c.Del(1)
	}
	require.Equal(t, 3, keyToHashCount)
}

func TestCache_AutoReallocate(t *testing.T) {
	cache, err := NewCache(&Config[int, int]{
		NumCounters:        1000,
		MaxCost:            5,
		BufferItems:        64,
		IgnoreInternalCost: true,
		Metrics:            true,
	})
	require.NoError(t, err)
	defer cache.Close()

	for i := 0; i < 5; i++ {
		ok := cache.Set(i, i, 1)
		require.True(t, ok, "Set(%d) should succeed", i)
	}
	cache.Wait()

	t.Logf("Before reallocate: MaxCost=%d, UsedCost=%d, KeysAdded=%d",
		cache.MaxCost(), cache.MaxCost()-cache.RemainingCost(), cache.Metrics.KeysAdded())
	require.Equal(t, int64(5), cache.MaxCost(), "before auto-reallocate")

	require.Eventually(t, func() bool {
		maxCost := cache.MaxCost()
		t.Logf("Checking MaxCost=%d, UsedCost=%d, KeysAdded=%d",
			maxCost, cache.MaxCost()-cache.RemainingCost(), cache.Metrics.KeysAdded())
		return maxCost == 10
	}, 10*time.Second, 100*time.Millisecond, "expected MaxCost to auto-double to 10")

	for i := 0; i < 5; i++ {
		v, ok := cache.Get(i)
		require.True(t, ok, "key %d must survive reallocation", i)
		require.Equal(t, i, v)
	}

	ok := cache.Set(42, 42, 1)
	require.True(t, ok, "Set(42) after auto-reallocate should succeed")
	cache.Wait()

	v, ok := cache.Get(42)
	require.True(t, ok, "new key must be in cache")
	require.Equal(t, 42, v)

	t.Logf("Final state: MaxCost=%d, UsedCost=%d, KeysAdded=%d",
		cache.MaxCost(), cache.MaxCost()-cache.RemainingCost(), cache.Metrics.KeysAdded())
}

func TestReallocateConcurrency(t *testing.T) {
	cache, err := NewCache(&Config[int, int]{
		NumCounters: 1000,
		MaxCost:     100,
		BufferItems: 64,
	})
	require.NoError(t, err)
	defer cache.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache.Set(i, i, 1)
			if i == 50 {
				require.NoError(t, cache.Reallocate())
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < 100; i++ {
		val, ok := cache.Get(i)
		if ok {
			require.Equal(t, i, val)
		}
	}
	require.Equal(t, int64(200), cache.MaxCost())
}

func TestReallocateMultipleTimes(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:           1000,
		MaxCost:               10,
		BufferItems:           64,
		IgnoreInternalCost:    true,
		Metrics:               true,
		DisableAutoReallocate: true, // Disable auto-reallocation to test manual calls
	})
	require.NoError(t, err)
	defer c.Close()

	// Set initial items
	for i := 0; i < 5; i++ {
		require.True(t, c.Set(i, i, 1), "Set(%d) should succeed", i)
	}
	c.Wait()

	// Initial state
	require.Equal(t, int64(10), c.MaxCost(), "initial MaxCost")
	require.Equal(t, uint64(5), c.Metrics.KeysAdded(), "initial KeysAdded")
	t.Logf("Initial: MaxCost=%d, UsedCost=%d, KeysAdded=%d",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded())

	// Call Reallocate three times
	for i := 1; i <= 3; i++ {
		require.NoError(t, c.Reallocate(), "Reallocate #%d should succeed", i)
		expectedMaxCost := int64(10 * (1 << uint(i))) // 20, 40, 80
		require.Equal(t, expectedMaxCost, c.MaxCost(), "MaxCost after Reallocate #%d", i)
		require.Equal(t, uint64(5), c.Metrics.KeysAdded(), "KeysAdded after Reallocate #%d", i)
		t.Logf("After Reallocate #%d: MaxCost=%d, UsedCost=%d, KeysAdded=%d",
			i, c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded())

		// Verify all items are preserved
		for j := 0; j < 5; j++ {
			val, ok := c.Get(j)
			require.True(t, ok, "key %d must survive Reallocate #%d", j, i)
			require.Equal(t, j, val, "value for key %d after Reallocate #%d", j, i)
		}
	}

	// Add a new item after final reallocation
	require.True(t, c.Set(42, 42, 1), "Set(42) should succeed after reallocations")
	c.Wait()
	val, ok := c.Get(42)
	require.True(t, ok, "new key must be in cache")
	require.Equal(t, 42, val, "value for key 42")
	require.Equal(t, uint64(6), c.Metrics.KeysAdded(), "final KeysAdded")
}

// TestReallocateWithEvictions tests Reallocate when items are evicted due to policy constraints.
func TestReallocateWithEvictions(t *testing.T) {

	evictedKeys := make(map[int]struct{})
	rejectedKeys := make(map[int]struct{})
	var mu sync.Mutex
	c, err := NewCache(&Config[int, int]{
		NumCounters:           1000,
		MaxCost:               5,
		BufferItems:           64,
		IgnoreInternalCost:    true,
		Metrics:               true,
		DisableAutoReallocate: true,
		OnEvict: func(item *Item[int, int]) {
			mu.Lock()
			t.Logf("Evicted key: %d", item.OriginalKey)
			evictedKeys[item.OriginalKey] = struct{}{}
			mu.Unlock()
		},
		OnReject: func(item *Item[int, int]) {
			mu.Lock()
			t.Logf("Rejected key: %d", item.OriginalKey)
			rejectedKeys[item.OriginalKey] = struct{}{}
			mu.Unlock()
		},
	})
	require.NoError(t, err)
	defer c.Close()

	// Add items to exceed MaxCost
	for i := 0; i < 10; i++ {
		require.True(t, c.Set(i, i, 2), "Set(%d) should succeed", i)
	}
	c.Wait()

	// Log initial state
	mu.Lock()
	t.Logf("Initial: MaxCost=%d, UsedCost=%d, KeysAdded=%d, Evicted=%v, Rejected=%v",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded(), evictedKeys, rejectedKeys)
	mu.Unlock()

	// Call Reallocate
	require.NoError(t, c.Reallocate(), "Reallocate should succeed")
	mu.Lock()
	t.Logf("After Reallocate: MaxCost=%d, UsedCost=%d, KeysAdded=%d, Evicted=%v, Rejected=%v",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded(), evictedKeys, rejectedKeys)
	mu.Unlock()

	// Verify surviving items
	survivingKeys := 0
	for i := 0; i < 10; i++ {
		mu.Lock()
		_, wasEvicted := evictedKeys[i]
		_, wasRejected := rejectedKeys[i]
		mu.Unlock()
		if !wasEvicted && !wasRejected {
			val, ok := c.Get(i)
			t.Logf("Checking key %d: ok=%v, value=%v", i, ok, val)
			require.True(t, ok, "key %d should survive Reallocate if not evicted or rejected", i)
			require.Equal(t, i, val, "value for key %d", i)
			survivingKeys++
		}
	}
	require.Equal(t, uint64(survivingKeys), c.Metrics.KeysAdded(), "KeysAdded should match surviving keys")

	// Add new items to verify increased capacity
	require.True(t, c.Set(42, 42, 2), "Set(42) should succeed after Reallocate")
	c.Wait()
	val, ok := c.Get(42)
	require.True(t, ok, "new key must be in cache")
	require.Equal(t, 42, val, "value for key 42")
}

// TestReallocateConcurrentSetGet tests concurrent Reallocate, Set, and Get operations.
func TestReallocateConcurrentSetGet(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:           1000,
		MaxCost:               100,
		BufferItems:           64,
		IgnoreInternalCost:    true,
		Metrics:               true,
		DisableAutoReallocate: true,
	})
	require.NoError(t, err)
	defer c.Close()

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOps = 100

	// Concurrent Set operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(startKey int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := startKey + j
				c.Set(key, key, 1)
				time.Sleep(time.Microsecond)
			}
		}(i * numOps)
	}

	// Concurrent Get operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(startKey int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := startKey + j
				c.Get(key)
				time.Sleep(time.Microsecond)
			}
		}(i * numOps)
	}

	// Concurrent Reallocate calls
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			time.Sleep(time.Duration(i*10) * time.Millisecond) // Stagger calls
			require.NoError(t, c.Reallocate(), "Reallocate #%d should succeed", i)
			t.Logf("Reallocate #%d completed: MaxCost=%d", i, c.MaxCost())
		}(i)
	}

	wg.Wait()

	// Verify final MaxCost (100 * 2^3 = 800)
	require.Equal(t, int64(800), c.MaxCost(), "MaxCost after multiple Reallocates")

	// Verify some items are still accessible
	keysFound := 0
	for i := 0; i < numGoroutines*numOps; i++ {
		if val, ok := c.Get(i); ok {
			require.Equal(t, i, val, "value for key %d", i)
			keysFound++
		}
	}
	t.Logf("Keys found after concurrent operations: %d", keysFound)
	require.True(t, keysFound > 0, "some keys should remain in cache")
}

// TestReallocateWithTTL tests that TTLs are preserved during Reallocate.
func TestReallocateWithTTL(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:           1000,
		MaxCost:               10,
		BufferItems:           64,
		IgnoreInternalCost:    true,
		Metrics:               true,
		DisableAutoReallocate: true,
	})
	require.NoError(t, err)
	defer c.Close()

	// Set items with TTL
	ttl := 5 * time.Second
	for i := 0; i < 5; i++ {
		require.True(t, c.SetWithTTL(i, i, 1, ttl), "SetWithTTL(%d) should succeed", i)
	}
	c.Wait()

	// Verify TTLs before Reallocate
	for i := 0; i < 5; i++ {
		ttlVal, ok := c.GetTTL(i)
		require.True(t, ok, "GetTTL(%d) should succeed", i)
		require.WithinDuration(t, time.Now().Add(ttl), time.Now().Add(ttlVal), 1*time.Second, "TTL for key %d", i)
	}

	// Call Reallocate
	require.NoError(t, c.Reallocate(), "Reallocate should succeed")
	require.Equal(t, int64(20), c.MaxCost(), "MaxCost after Reallocate")

	// Verify TTLs and items after Reallocate
	for i := 0; i < 5; i++ {
		val, ok := c.Get(i)
		require.True(t, ok, "key %d must survive Reallocate", i)
		require.Equal(t, i, val, "value for key %d", i)
		ttlVal, ok := c.GetTTL(i)
		require.True(t, ok, "GetTTL(%d) should succeed after Reallocate", i)
		require.WithinDuration(t, time.Now().Add(ttl), time.Now().Add(ttlVal), 1*time.Second, "TTL for key %d after Reallocate", i)
	}

	// Wait for TTL expiration
	time.Sleep(6 * time.Second)

	// Verify items are expired
	for i := 0; i < 5; i++ {
		_, ok := c.Get(i)
		require.False(t, ok, "key %d should be expired", i)
		ttlVal, ok := c.GetTTL(i)
		require.False(t, ok, "GetTTL(%d) should fail after expiration", i)
		require.Equal(t, time.Duration(0), ttlVal, "TTL for key %d should be zero", i)
	}
}

// TestReallocateMetrics tests that metrics are correctly updated after Reallocate.
func TestReallocateMetrics(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:           1000,
		MaxCost:               10,
		BufferItems:           64,
		IgnoreInternalCost:    true,
		Metrics:               true,
		DisableAutoReallocate: true,
	})
	require.NoError(t, err)
	defer c.Close()

	// Add items
	for i := 0; i < 5; i++ {
		require.True(t, c.Set(i, i, 2), "Set(%d) should succeed", i)
	}
	c.Wait()

	// Verify initial metrics
	require.Equal(t, uint64(5), c.Metrics.KeysAdded(), "initial KeysAdded")
	require.Equal(t, uint64(10), c.Metrics.CostAdded(), "initial CostAdded")
	require.Equal(t, int64(10), c.MaxCost()-c.RemainingCost(), "initial UsedCost")
	t.Logf("Initial: MaxCost=%d, UsedCost=%d, KeysAdded=%d, CostAdded=%d",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded(), c.Metrics.CostAdded())

	// Call Reallocate
	require.NoError(t, c.Reallocate(), "Reallocate should succeed")
	require.Equal(t, int64(20), c.MaxCost(), "MaxCost after Reallocate")

	// Verify metrics after Reallocate
	require.Equal(t, uint64(5), c.Metrics.KeysAdded(), "KeysAdded after Reallocate")
	require.Equal(t, uint64(10), c.Metrics.CostAdded(), "CostAdded after Reallocate")
	require.Equal(t, int64(10), c.MaxCost()-c.RemainingCost(), "UsedCost after Reallocate")
	t.Logf("After Reallocate: MaxCost=%d, UsedCost=%d, KeysAdded=%d, CostAdded=%d",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded(), c.Metrics.CostAdded())

	// Add more items
	for i := 5; i < 7; i++ {
		require.True(t, c.Set(i, i, 2), "Set(%d) should succeed", i)
	}
	c.Wait()

	// Verify final metrics
	require.Equal(t, uint64(7), c.Metrics.KeysAdded(), "final KeysAdded")
	require.Equal(t, uint64(14), c.Metrics.CostAdded(), "final CostAdded")
	require.Equal(t, int64(14), c.MaxCost()-c.RemainingCost(), "final UsedCost")
}

// TestReallocateAfterUpdateMaxCost tests interaction between Reallocate and UpdateMaxCost.
func TestReallocateAfterUpdateMaxCost(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:           1000,
		MaxCost:               10,
		BufferItems:           64,
		IgnoreInternalCost:    true,
		Metrics:               true,
		DisableAutoReallocate: true,
	})
	require.NoError(t, err)
	defer c.Close()

	// Set initial items
	for i := 0; i < 5; i++ {
		require.True(t, c.Set(i, i, 1), "Set(%d) should succeed", i)
	}
	c.Wait()

	// Update MaxCost
	c.UpdateMaxCost(50)
	require.Equal(t, int64(50), c.MaxCost(), "MaxCost after UpdateMaxCost")
	require.Equal(t, uint64(5), c.Metrics.KeysAdded(), "KeysAdded after UpdateMaxCost")
	t.Logf("After UpdateMaxCost: MaxCost=%d, UsedCost=%d, KeysAdded=%d",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded())

	// Call Reallocate
	require.NoError(t, c.Reallocate(), "Reallocate should succeed")
	require.Equal(t, int64(100), c.MaxCost(), "MaxCost after Reallocate") // 50 * 2
	require.Equal(t, uint64(5), c.Metrics.KeysAdded(), "KeysAdded after Reallocate")
	t.Logf("After Reallocate: MaxCost=%d, UsedCost=%d, KeysAdded=%d",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded())

	// Verify all items are preserved
	for i := 0; i < 5; i++ {
		val, ok := c.Get(i)
		require.True(t, ok, "key %d must survive Reallocate", i)
		require.Equal(t, i, val, "value for key %d", i)
	}

	// Add new item
	require.True(t, c.Set(42, 42, 1), "Set(42) should succeed")
	c.Wait()
	val, ok := c.Get(42)
	require.True(t, ok, "new key must be in cache")
	require.Equal(t, 42, val, "value for key 42")
}

// TestReallocateEmptyCache tests Reallocate on an empty cache.
func TestReallocateEmptyCache(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:           1000,
		MaxCost:               10,
		BufferItems:           64,
		IgnoreInternalCost:    true,
		Metrics:               true,
		DisableAutoReallocate: true,
	})
	require.NoError(t, err)
	defer c.Close()

	// Initial state: empty cache
	require.Equal(t, int64(10), c.MaxCost(), "initial MaxCost")
	require.Equal(t, uint64(0), c.Metrics.KeysAdded(), "initial KeysAdded")
	require.Equal(t, int64(10), c.RemainingCost(), "initial RemainingCost")
	t.Logf("Initial: MaxCost=%d, UsedCost=%d, KeysAdded=%d",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded())

	// Call Reallocate
	require.NoError(t, c.Reallocate(), "Reallocate should succeed on empty cache")
	require.Equal(t, int64(20), c.MaxCost(), "MaxCost after Reallocate")
	require.Equal(t, uint64(0), c.Metrics.KeysAdded(), "KeysAdded after Reallocate")
	require.Equal(t, int64(20), c.RemainingCost(), "RemainingCost after Reallocate")
	t.Logf("After Reallocate: MaxCost=%d, UsedCost=%d, KeysAdded=%d",
		c.MaxCost(), c.MaxCost()-c.RemainingCost(), c.Metrics.KeysAdded())

	// Add an item to verify functionality
	require.True(t, c.Set(1, 1, 1), "Set(1) should succeed")
	c.Wait()
	val, ok := c.Get(1)
	require.True(t, ok, "key 1 must be in cache")
	require.Equal(t, 1, val, "value for key 1")
	require.Equal(t, uint64(1), c.Metrics.KeysAdded(), "final KeysAdded")
}

func TestCacheMaxCost(t *testing.T) {

	charset := "abcdefghijklmnopqrstuvwxyz0123456789"
	key := func() []byte {
		k := make([]byte, 2)
		for i := range k {
			k[i] = charset[rand.Intn(len(charset))]
		}
		return k
	}
	c, err := NewCache(&Config[[]byte, string]{
		NumCounters:           12960, // 36^2 * 10
		MaxCost:               1e6,   // 1mb
		BufferItems:           64,
		Metrics:               true,
		DisableAutoReallocate: true,
	})
	require.NoError(t, err)
	stop := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
					time.Sleep(time.Millisecond)

					k := key()
					if _, ok := c.Get(k); !ok {
						val := ""
						if rand.Intn(100) < 10 {
							val = "test"
						} else {
							val = strings.Repeat("a", 1000)
						}
						c.Set(key(), val, int64(2+len(val)))
					}
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		time.Sleep(time.Second)
		cacheCost := c.Metrics.CostAdded() - c.Metrics.CostEvicted()
		t.Logf("total cache cost: %d\n", cacheCost)
		require.True(t, float64(cacheCost) <= float64(1e6*1.05))
	}
	for i := 0; i < 8; i++ {
		stop <- struct{}{}
	}
}

func TestUpdateMaxCost(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters: 10,
		MaxCost:     10,
		BufferItems: 64,
	})
	require.NoError(t, err)
	require.Equal(t, int64(10), c.MaxCost())
	require.True(t, c.Set(1, 1, 1))
	time.Sleep(wait)
	_, ok := c.Get(1)
	// Set is rejected because the cost of the entry is too high
	// when accounting for the internal cost of storing the entry.
	require.False(t, ok)

	// Update the max cost of the cache and retry.
	c.UpdateMaxCost(1000)
	require.Equal(t, int64(1000), c.MaxCost())
	require.True(t, c.Set(1, 1, 1))
	time.Sleep(wait)
	val, ok := c.Get(1)
	require.True(t, ok)
	require.NotNil(t, val)
	c.Del(1)
}

func TestRemainingCost(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters: 10,
		MaxCost:     100,
		BufferItems: 64,
	})
	require.NoError(t, err)
	require.Equal(t, int64(100), c.RemainingCost())

	// Set allowed into cache
	require.True(t, c.Set(1, 1, 1))
	c.Wait()
	_, ok := c.Get(1)
	require.True(t, ok)

	expectedUsed := 1 + c.itemSize
	require.Equal(t, c.MaxCost()-expectedUsed, c.RemainingCost())

	// Set rejected due to exceeding capacity
	require.True(t, c.Set(2, 2, 99))
	c.Wait()
	_, ok = c.Get(2)
	require.False(t, ok)
	// No change in RemainingCost
	require.Equal(t, c.MaxCost()-expectedUsed, c.RemainingCost())

	// RemainingCost is still MaxCost() - Used after UpdateMaxCost()
	c.UpdateMaxCost(1000)
	require.Equal(t, int64(1000), c.MaxCost())
	require.Equal(t, c.MaxCost()-expectedUsed, c.RemainingCost())
	c.Del(1)
}

func TestNewCache(t *testing.T) {

	_, err := NewCache(&Config[int, int]{
		NumCounters: 0,
	})
	require.Error(t, err)

	_, err = NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     0,
	})
	require.Error(t, err)

	_, err = NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     10,
		BufferItems: 0,
	})
	require.Error(t, err)

	c, err := NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     10,
		BufferItems: 64,
		Metrics:     true,
	})
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNilCache(t *testing.T) {

	var c *Cache[int, int]
	val, ok := c.Get(1)
	require.False(t, ok)
	require.Zero(t, val)

	require.False(t, c.Set(1, 1, 1))
	c.Del(1)
	c.Clear()
	c.Close()
}

func TestMultipleClose(t *testing.T) {

	var c *Cache[int, int]
	c.Close()

	var err error
	c, err = NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     10,
		BufferItems: 64,
		Metrics:     true,
	})
	require.NoError(t, err)
	c.Close()
	c.Close()
}

func TestSetAfterClose(t *testing.T) {

	c, err := newTestCache()
	require.NoError(t, err)
	require.NotNil(t, c)

	c.Close()
	require.False(t, c.Set(1, 1, 1))
}

func TestClearAfterClose(t *testing.T) {

	c, err := newTestCache()
	require.NoError(t, err)
	require.NotNil(t, c)

	c.Close()
	c.Clear()
}

func TestGetAfterClose(t *testing.T) {

	c, err := newTestCache()
	require.NoError(t, err)
	require.NotNil(t, c)

	require.True(t, c.Set(1, 1, 1))
	c.Close()

	_, ok := c.Get(1)
	require.False(t, ok)
}

func TestDelAfterClose(t *testing.T) {

	c, err := newTestCache()
	require.NoError(t, err)
	require.NotNil(t, c)

	require.True(t, c.Set(1, 1, 1))
	c.Close()

	c.Del(1)
}

func TestCacheProcessItems(t *testing.T) {

	m := &sync.Mutex{}
	evicted := make(map[uint64]struct{})
	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		BufferItems:        64,
		IgnoreInternalCost: true,
		Cost: func(value int) int64 {
			return int64(value)
		},
		OnEvict: func(item *Item[int, int]) {
			m.Lock()
			defer m.Unlock()
			evicted[item.Key] = struct{}{}
		},
	})
	require.NoError(t, err)

	var key uint64
	var conflict uint64

	key, conflict = z.KeyToHash(1)
	c.setBuf <- &Item[int, int]{
		flag:        itemNew,
		Key:         key,
		OriginalKey: 1,
		Conflict:    conflict,
		Value:       1,
		Cost:        0,
	}
	time.Sleep(wait)
	require.True(t, c.cachePolicy.Has(1))
	require.Equal(t, int64(1), c.cachePolicy.Cost(1))

	key, conflict = z.KeyToHash(1)
	c.setBuf <- &Item[int, int]{
		flag:        itemUpdate,
		Key:         key,
		OriginalKey: 1,
		Conflict:    conflict,
		Value:       2,
		Cost:        0,
	}
	time.Sleep(wait)
	require.Equal(t, int64(2), c.cachePolicy.Cost(1))

	key, conflict = z.KeyToHash(1)
	c.setBuf <- &Item[int, int]{
		flag:     itemDelete,
		Key:      key,
		Conflict: conflict,
	}
	time.Sleep(wait)
	key, conflict = z.KeyToHash(1)
	val, ok := c.storedItems.Get(key, conflict)
	require.False(t, ok)
	require.Zero(t, val)
	require.False(t, c.cachePolicy.Has(1))

	key, conflict = z.KeyToHash(2)
	c.setBuf <- &Item[int, int]{
		flag:        itemNew,
		Key:         key,
		OriginalKey: 2,
		Conflict:    conflict,
		Value:       2,
		Cost:        3,
	}
	key, conflict = z.KeyToHash(3)
	c.setBuf <- &Item[int, int]{
		flag:        itemNew,
		Key:         key,
		OriginalKey: 3,
		Conflict:    conflict,
		Value:       3,
		Cost:        3,
	}
	key, conflict = z.KeyToHash(4)
	c.setBuf <- &Item[int, int]{
		flag:        itemNew,
		Key:         key,
		OriginalKey: 4,
		Conflict:    conflict,
		Value:       3,
		Cost:        3,
	}
	key, conflict = z.KeyToHash(5)
	c.setBuf <- &Item[int, int]{
		flag:        itemNew,
		Key:         key,
		OriginalKey: 5,
		Conflict:    conflict,
		Value:       3,
		Cost:        5,
	}
	time.Sleep(wait)
	m.Lock()
	require.NotEqual(t, 0, len(evicted))
	m.Unlock()

	defer func() {
		require.NotNil(t, recover())
	}()
	c.Close()
	c.setBuf <- &Item[int, int]{flag: itemNew}
}

func TestCacheGet(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		BufferItems:        64,
		IgnoreInternalCost: true,
		Metrics:            true,
	})
	require.NoError(t, err)

	key, conflict := z.KeyToHash(1)
	i := Item[int, int]{
		Key:         key,
		OriginalKey: 1,
		Conflict:    conflict,
		Value:       1,
	}
	c.storedItems.Set(&i)
	val, ok := c.Get(1)
	require.True(t, ok)
	require.NotNil(t, val)

	val, ok = c.Get(2)
	require.False(t, ok)
	require.Zero(t, val)

	// 0.5 and not 1.0 because we tried Getting each item twice
	require.Equal(t, 0.5, c.Metrics.Ratio())

	c = nil
	val, ok = c.Get(0)
	require.False(t, ok)
	require.Zero(t, val)
}

func TestCacheSetIter(t *testing.T) {

	c, err := NewCache(&Config[string, int]{
		NumCounters:        100,
		MaxCost:            10,
		BufferItems:        64,
		IgnoreInternalCost: true,
		Metrics:            true,
	})
	require.NoError(t, err)

	expectedValues := map[string]int{
		"a": 1,
		"b": 2,
		"c": 3,
		"d": 4,
	}
	for k, v := range expectedValues {
		key, conflict := z.KeyToHash(k)
		i := Item[string, int]{
			Key:         key,
			OriginalKey: k,
			Conflict:    conflict,
			Value:       v,
		}
		c.storedItems.Set(&i)
	}

	resultMap := make(map[string]int)
	c.Iter(func(k string, v int) (stop bool) {
		resultMap[k] = v
		return false
	})

	require.Equal(t, expectedValues, resultMap)
}

// retrySet calls SetWithTTL until the item is accepted by the cache.
func retrySet(t *testing.T, c *Cache[int, int], key, value int, cost int64, ttl time.Duration) {
	for {
		if set := c.SetWithTTL(key, value, cost, ttl); !set {
			time.Sleep(wait)
			continue
		}

		time.Sleep(wait)
		val, ok := c.Get(key)
		require.True(t, ok)
		require.NotNil(t, val)
		require.Equal(t, value, val)
		return
	}
}

func TestCacheSet(t *testing.T) {
	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		IgnoreInternalCost: true,
		BufferItems:        64,
		Metrics:            true,
	})
	require.NoError(t, err)

	retrySet(t, c, 1, 1, 1, 0)

	c.Set(1, 2, 2)
	val, ok := c.storedItems.Get(z.KeyToHash(1))
	require.True(t, ok)
	require.Equal(t, 2, val)

	c.stop <- struct{}{}
	<-c.done
	for i := 0; i < setBufSize; i++ {
		key, conflict := z.KeyToHash(1)
		c.setBuf <- &Item[int, int]{
			flag:        itemUpdate,
			Key:         key,
			OriginalKey: 1,
			Conflict:    conflict,
			Value:       1,
			Cost:        1,
		}
	}
	require.False(t, c.Set(2, 2, 1))
	require.Equal(t, uint64(1), c.Metrics.SetsDropped())
	close(c.setBuf)
	close(c.stop)
	close(c.done)

	c = nil
	require.False(t, c.Set(1, 1, 1))
}

func TestCacheInternalCost(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     10,
		BufferItems: 64,
		Metrics:     true,
	})
	require.NoError(t, err)

	// Get should return false because the cache's cost is too small to storedItems the item
	// when accounting for the internal cost.
	c.SetWithTTL(1, 1, 1, 0)
	time.Sleep(wait)
	_, ok := c.Get(1)
	require.False(t, ok)
}

func TestRecacheWithTTL(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		IgnoreInternalCost: true,
		BufferItems:        64,
		Metrics:            true,
	})

	require.NoError(t, err)

	// Set initial value for key = 1
	insert := c.SetWithTTL(1, 1, 1, 5*time.Second)
	require.True(t, insert)
	time.Sleep(2 * time.Second)

	// Get value from cache for key = 1
	val, ok := c.Get(1)
	require.True(t, ok)
	require.NotNil(t, val)
	require.Equal(t, 1, val)

	// Wait for expiration
	time.Sleep(5 * time.Second)

	// The cached value for key = 1 should be gone
	val, ok = c.Get(1)
	require.False(t, ok)
	require.Zero(t, val)

	// Set new value for key = 1
	insert = c.SetWithTTL(1, 2, 1, 5*time.Second)
	require.True(t, insert)
	time.Sleep(2 * time.Second)

	// Get value from cache for key = 1
	val, ok = c.Get(1)
	require.True(t, ok)
	require.NotNil(t, val)
	require.Equal(t, 2, val)
}

func TestCacheSetWithTTL(t *testing.T) {

	m := &sync.Mutex{}
	evicted := make(map[uint64]struct{})
	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		IgnoreInternalCost: true,
		BufferItems:        64,
		Metrics:            true,
		OnEvict: func(item *Item[int, int]) {
			m.Lock()
			defer m.Unlock()
			evicted[item.Key] = struct{}{}
		},
	})
	require.NoError(t, err)

	retrySet(t, c, 1, 1, 1, time.Second)

	// Sleep to make sure the item has expired after execution resumes.
	time.Sleep(2 * time.Second)
	val, ok := c.Get(1)
	require.False(t, ok)
	require.Zero(t, val)

	// Sleep to ensure that the bucket where the item was stored has been cleared
	// from the expiraton map.
	time.Sleep(5 * time.Second)
	m.Lock()
	require.Equal(t, 1, len(evicted))
	_, ok = evicted[1]
	require.True(t, ok)
	m.Unlock()

	// Verify that expiration times are overwritten.
	retrySet(t, c, 2, 1, 1, time.Second)
	retrySet(t, c, 2, 2, 1, 100*time.Second)
	time.Sleep(3 * time.Second)
	val, ok = c.Get(2)
	require.True(t, ok)
	require.Equal(t, 2, val)

	// Verify that entries with no expiration are overwritten.
	retrySet(t, c, 3, 1, 1, 0)
	retrySet(t, c, 3, 2, 1, time.Second)
	time.Sleep(3 * time.Second)
	val, ok = c.Get(3)
	require.False(t, ok)
	require.Zero(t, val)
}

func TestCacheDel(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     10,
		BufferItems: 64,
	})
	require.NoError(t, err)

	c.Set(1, 1, 1)
	c.Del(1)
	// The deletes and sets are pushed through the setbuf. It might be possible
	// that the delete is not processed before the following get is called. So
	// wait for a millisecond for things to be processed.
	time.Sleep(time.Millisecond)
	val, ok := c.Get(1)
	require.False(t, ok)
	require.Zero(t, val)

	c = nil
	defer func() {
		require.Nil(t, recover())
	}()
	c.Del(1)
}

func TestCacheDelWithTTL(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		IgnoreInternalCost: true,
		BufferItems:        64,
	})
	require.NoError(t, err)
	retrySet(t, c, 3, 1, 1, 10*time.Second)
	time.Sleep(1 * time.Second)
	// Delete the item
	c.Del(3)
	// Ensure the key is deleted.
	val, ok := c.Get(3)
	require.False(t, ok)
	require.Zero(t, val)
}

func TestCacheGetTTL(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		IgnoreInternalCost: true,
		BufferItems:        64,
		Metrics:            true,
	})
	require.NoError(t, err)

	// try expiration with valid ttl item
	{
		expiration := time.Second * 5
		retrySet(t, c, 1, 1, 1, expiration)

		val, ok := c.Get(1)
		require.True(t, ok)
		require.Equal(t, 1, val)

		ttl, ok := c.GetTTL(1)
		require.True(t, ok)
		require.WithinDuration(t,
			time.Now().Add(expiration), time.Now().Add(ttl), 1*time.Second)

		c.Del(1)

		ttl, ok = c.GetTTL(1)
		require.False(t, ok)
		require.Equal(t, ttl, time.Duration(0))
	}
	// try expiration with no ttl
	{
		retrySet(t, c, 2, 2, 1, time.Duration(0))

		val, ok := c.Get(2)
		require.True(t, ok)
		require.Equal(t, 2, val)

		ttl, ok := c.GetTTL(2)
		require.True(t, ok)
		require.Equal(t, ttl, time.Duration(0))
	}
	// try expiration with missing item
	{
		ttl, ok := c.GetTTL(3)
		require.False(t, ok)
		require.Equal(t, ttl, time.Duration(0))
	}
	// try expiration with expired item
	{
		expiration := time.Second
		retrySet(t, c, 3, 3, 1, expiration)

		val, ok := c.Get(3)
		require.True(t, ok)
		require.Equal(t, 3, val)

		time.Sleep(time.Second)

		ttl, ok := c.GetTTL(3)
		require.False(t, ok)
		require.Equal(t, ttl, time.Duration(0))
	}
}

func TestCacheClear(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		IgnoreInternalCost: true,
		BufferItems:        64,
		Metrics:            true,
	})
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		c.Set(i, i, 1)
	}
	time.Sleep(wait)
	require.Equal(t, uint64(10), c.Metrics.KeysAdded())

	c.Clear()
	require.Equal(t, uint64(0), c.Metrics.KeysAdded())

	for i := 0; i < 10; i++ {
		val, ok := c.Get(i)
		require.False(t, ok)
		require.Zero(t, val)
	}
}

func TestCacheMetrics(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters:        100,
		MaxCost:            10,
		IgnoreInternalCost: true,
		BufferItems:        64,
		Metrics:            true,
	})
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		c.Set(i, i, 1)
	}
	time.Sleep(wait)
	m := c.Metrics
	require.Equal(t, uint64(10), m.KeysAdded())
}

func TestMetrics(t *testing.T) {

	newMetrics()
}

func TestNilMetrics(t *testing.T) {

	var m *Metrics
	for _, f := range []func() uint64{
		m.Hits,
		m.Misses,
		m.KeysAdded,
		m.KeysEvicted,
		m.CostEvicted,
		m.SetsDropped,
		m.SetsRejected,
		m.GetsDropped,
		m.GetsKept,
	} {
		require.Equal(t, uint64(0), f())
	}
}

func TestMetricsAddGet(t *testing.T) {

	m := newMetrics()
	m.add(hit, 1, 1)
	m.add(hit, 2, 2)
	m.add(hit, 3, 3)
	require.Equal(t, uint64(6), m.Hits())

	m = nil
	m.add(hit, 1, 1)
	require.Equal(t, uint64(0), m.Hits())
}

func TestMetricsRatio(t *testing.T) {

	m := newMetrics()
	require.Equal(t, float64(0), m.Ratio())

	m.add(hit, 1, 1)
	m.add(hit, 2, 2)
	m.add(miss, 1, 1)
	m.add(miss, 2, 2)
	require.Equal(t, 0.5, m.Ratio())

	m = nil
	require.Equal(t, float64(0), m.Ratio())
}

func TestMetricsString(t *testing.T) {

	m := newMetrics()
	m.add(hit, 1, 1)
	m.add(miss, 1, 1)
	m.add(keyAdd, 1, 1)
	m.add(keyUpdate, 1, 1)
	m.add(keyEvict, 1, 1)
	m.add(costAdd, 1, 1)
	m.add(costEvict, 1, 1)
	m.add(dropSets, 1, 1)
	m.add(rejectSets, 1, 1)
	m.add(dropGets, 1, 1)
	m.add(keepGets, 1, 1)
	require.Equal(t, uint64(1), m.Hits())
	require.Equal(t, uint64(1), m.Misses())
	require.Equal(t, 0.5, m.Ratio())
	require.Equal(t, uint64(1), m.KeysAdded())
	require.Equal(t, uint64(1), m.KeysUpdated())
	require.Equal(t, uint64(1), m.KeysEvicted())
	require.Equal(t, uint64(1), m.CostAdded())
	require.Equal(t, uint64(1), m.CostEvicted())
	require.Equal(t, uint64(1), m.SetsDropped())
	require.Equal(t, uint64(1), m.SetsRejected())
	require.Equal(t, uint64(1), m.GetsDropped())
	require.Equal(t, uint64(1), m.GetsKept())

	require.NotEqual(t, 0, len(m.String()))

	m = nil
	require.Equal(t, 0, len(m.String()))

	require.Equal(t, "unidentified", stringFor(doNotUse))
}

func TestCacheMetricsClear(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     10,
		BufferItems: 64,
		Metrics:     true,
	})
	require.NoError(t, err)

	c.Set(1, 1, 1)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				c.Get(1)
			}
		}
	}()
	time.Sleep(wait)
	c.Clear()
	stop <- struct{}{}
	c.Metrics = nil
	c.Metrics.Clear()
}

func init() {
	// Set bucketSizeSecs to 1 to avoid waiting too much during the tests.
	bucketDurationSecs = 1
}

func TestBlockOnClear(t *testing.T) {

	c, err := NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     10,
		BufferItems: 64,
		Metrics:     false,
	})
	require.NoError(t, err)
	defer c.Close()

	done := make(chan struct{})

	go func() {
		for i := 0; i < 10; i++ {
			c.Wait()
		}
		close(done)
	}()

	for i := 0; i < 10; i++ {
		c.Clear()
	}

	select {
	case <-done:
		// We're OK
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out while waiting on cache")
	}
}

// Regression test for bug https://github.com/hypermodeinc/ristretto/issues/167
func TestDropUpdates(t *testing.T) {

	originalSetBufSize := setBufSize
	defer func() { setBufSize = originalSetBufSize }()

	test := func() {
		// droppedMap stores the items dropped from the cache.
		droppedMap := make(map[int]struct{})
		lastEvictedSet := int64(-1)

		var err error
		handler := func(_ interface{}, value interface{}) {
			v := value.(string)
			lastEvictedSet, err = strconv.ParseInt(v, 10, 32)
			require.NoError(t, err)

			_, ok := droppedMap[int(lastEvictedSet)]
			if ok {
				panic(fmt.Sprintf("val = %+v was dropped but it got evicted. Dropped items: %+v\n",
					lastEvictedSet, droppedMap))
			}
		}

		// This is important. The race condition shows up only when the setBuf
		// is full and that's why we reduce the buf size here. The test will
		// try to fill up the setbuf to it's capacity and then perform an
		// update on a key.
		setBufSize = 10

		c, err := NewCache(&Config[int, string]{
			NumCounters:        100,
			MaxCost:            10,
			BufferItems:        64,
			IgnoreInternalCost: true,
			Metrics:            true,
			// DisableAutoReallocate: true,
			OnEvict: func(item *Item[int, string]) {
				handler(nil, item.Value)
			},
		})
		require.NoError(t, err)

		for i := 0; i < 5*setBufSize; i++ {
			v := fmt.Sprintf("%0100d", i)
			// We're updating the same key.
			if !c.Set(0, v, 1) {
				// The race condition doesn't show up without this sleep.
				time.Sleep(time.Microsecond)
				droppedMap[i] = struct{}{}
			}
		}
		// Wait for all items to be processed: prevents next c.Set from getting dropped
		c.Wait()
		// This will cause eviction from the cache.
		require.True(t, c.Set(1, "1", 10))

		// Close() calls Clear(), which can cause the (key, value) pair of (1, nil) to be passed to
		// the OnEvict() callback if it was still in the setBuf. This fixes a panic in OnEvict:
		// "interface {} is nil, not string"
		c.Wait()
		c.Close()

		require.NotEqual(t, int64(-1), lastEvictedSet)
	}

	// Run the test 100 times since it's not reliable.
	for i := 0; i < 100; i++ {
		test()
	}
}

func TestRistrettoCalloc(t *testing.T) {

	maxCacheSize := 1 << 20
	config := &Config[int, []byte]{
		// Use 5% of cache memory for storing counters.
		NumCounters: int64(float64(maxCacheSize) * 0.05 * 2),
		MaxCost:     int64(float64(maxCacheSize) * 0.95),
		BufferItems: 64,
		Metrics:     true,
		OnExit: func(val []byte) {
			z.Free(val)
		},
	}
	r, err := NewCache(config)
	require.NoError(t, err)
	defer r.Close()

	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rd := rand.New(rand.NewSource(time.Now().UnixNano()))
			for i := 0; i < 10000; i++ {
				k := rd.Intn(10000)
				v := z.Calloc(256, "test")
				rd.Read(v)
				if !r.Set(k, v, 256) {
					z.Free(v)
				}
				if rd.Intn(10) == 0 {
					r.Del(k)
				}
			}
		}()
	}
	wg.Wait()
	r.Clear()
	require.Zero(t, z.NumAllocBytes())
}

func TestRistrettoCallocTTL(t *testing.T) {

	maxCacheSize := 1 << 20
	config := &Config[int, []byte]{
		// Use 5% of cache memory for storing counters.
		NumCounters: int64(float64(maxCacheSize) * 0.05 * 2),
		MaxCost:     int64(float64(maxCacheSize) * 0.95),
		BufferItems: 64,
		Metrics:     true,
		OnExit: func(val []byte) {
			z.Free(val)
		},
	}
	r, err := NewCache(config)
	require.NoError(t, err)
	defer r.Close()

	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rd := rand.New(rand.NewSource(time.Now().UnixNano()))
			for i := 0; i < 10000; i++ {
				k := rd.Intn(10000)
				v := z.Calloc(256, "test")
				rd.Read(v)
				if !r.SetWithTTL(k, v, 256, time.Second) {
					z.Free(v)
				}
				if rd.Intn(10) == 0 {
					r.Del(k)
				}
			}
		}()
	}
	wg.Wait()
	time.Sleep(5 * time.Second)
	require.Zero(t, z.NumAllocBytes())
}

func newTestCache() (*Cache[int, int], error) {
	return NewCache(&Config[int, int]{
		NumCounters: 100,
		MaxCost:     10,
		BufferItems: 64,
		Metrics:     true,
	})
}

func TestCacheWithTTL(t *testing.T) {

	// There may be a race condition, so run the test multiple times.
	const try = 10

	for i := 0; i < try; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			c, err := NewCache(&Config[int, int]{
				NumCounters: 100,
				MaxCost:     1000,
				BufferItems: 64,
				Metrics:     true,
			})

			require.NoError(t, err)

			// Set initial value for key = 1
			insert := c.SetWithTTL(1, 1, 1, 800*time.Millisecond)
			require.True(t, insert)

			time.Sleep(100 * time.Millisecond)

			// Get value from cache for key = 1
			val, ok := c.Get(1)
			require.True(t, ok)
			require.NotNil(t, val)
			require.Equal(t, 1, val)

			time.Sleep(1200 * time.Millisecond)

			val, ok = c.Get(1)
			require.False(t, ok)
			require.Zero(t, val)
		})
	}
}
