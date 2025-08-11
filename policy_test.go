/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */
package sigil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPolicy(t *testing.T) {
	t.Parallel()

	defer func() {
		require.Nil(t, recover())
	}()
	newPolicy[int, int](100, 10)
}

func TestPolicyMetrics(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.CollectMetrics(newMetrics())
	require.NotNil(t, p.metrics)
	require.NotNil(t, p.evict.metrics)
}

func TestPolicyProcessItems(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.itemsCh <- []uint64{1, 2, 2}
	time.Sleep(wait)
	p.Lock()
	require.Equal(t, int64(2), p.admit.Estimate(2))
	require.Equal(t, int64(1), p.admit.Estimate(1))
	p.Unlock()

	p.stop <- struct{}{}
	<-p.done
	p.itemsCh <- []uint64{3, 3, 3}
	time.Sleep(wait)
	p.Lock()
	require.Equal(t, int64(0), p.admit.Estimate(3))
	p.Unlock()
}

func TestPolicyPush(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	require.True(t, p.Push([]uint64{}))

	keepCount := 0
	for i := 0; i < 10; i++ {
		if p.Push([]uint64{1, 2, 3, 4, 5}) {
			keepCount++
		}
	}
	require.NotEqual(t, 0, keepCount)
}

func TestPolicyAdd(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](1000, 100)
	// check reject element which cost more maxcost
	if victims, added := p.Add(1, 1, 101); victims != nil || added {
		t.Fatal("can't add an item bigger than entire cache")
	}

	// set keys with different hits and cost, to call eviction
	p.Lock()
	p.evict.add(1, 1, 1)  // cost=1, hits=5
	p.evict.add(2, 2, 50) // cost=50, hits=2
	p.evict.add(3, 3, 45) // cost=45, hits=5
	// set hits
	for i := 0; i < 5; i++ {
		p.admit.Increment(1)
		p.admit.Increment(3)
	}
	for i := 0; i < 2; i++ {
		p.admit.Increment(2) // hits=2 for 2, it can be evicted
	}
	p.Unlock()

	// check update key
	victims, added := p.Add(1, 1, 1)
	require.Nil(t, victims, "no victims for updating existing key")
	require.False(t, added, "key 1 already exists, should not add")

	// check added 2 key (already exist)
	victims, added = p.Add(2, 2, 50)
	require.Nil(t, victims, "no victims for updating existing key")
	require.False(t, added, "key 2 already exists, should not add")

	// check added key 4 wih cost=90,reject after key 2
	p.Push([]uint64{4, 4})            // incHits=2 for key 4
	time.Sleep(10 * time.Millisecond) // wait
	victims, added = p.Add(4, 4, 90)
	require.NotEmpty(t, victims, "should evict key 2 (hits=2 < 3) but reject due to minHitsThreshold")
	require.False(t, added, "key 4 should be rejected after evicting key 2")
	require.Equal(t, 1, len(victims), "should evict one victim")
	require.Equal(t, uint64(2), victims[0].Key, "key 2 (hits=2) should be evicted")
	require.Less(t, p.admit.Estimate(victims[0].Key), int64(minHitsThreshold), "evicted key should have hits < minHitsThreshold")

	p.Push([]uint64{6, 6})            // incHits=2 for key 6
	time.Sleep(10 * time.Millisecond) // wait
	victims, added = p.Add(6, 6, 40)
	require.Nil(t, victims, "no victims for adding key6")
	require.True(t, added, "key6 should be added")

	// check added key 5 with  cost=20 and hits=4, eviction 6 (hits=2)
	p.Push([]uint64{5, 5, 5, 5}) // incHits=4 for kye 5
	time.Sleep(10 * time.Millisecond)
	victims, added = p.Add(5, 5, 20)
	require.NotEmpty(t, victims, "should evict key with hits < minHitsThreshold")
	require.True(t, added, "key5 should be added")
	require.Equal(t, 1, len(victims), "should evict one victim")
	require.Equal(t, uint64(6), victims[0].Key, "key6 (hits=2) should be evicted")
	require.Less(t, p.admit.Estimate(victims[0].Key), int64(minHitsThreshold), "evicted key should have hits < minHitsThreshold")
}

func TestPolicyHas(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.Add(1, 1, 1)
	require.True(t, p.Has(1))
	require.False(t, p.Has(2))
}

func TestPolicyDel(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.Add(1, 1, 1)
	p.Del(1)
	p.Del(2)
	require.False(t, p.Has(1))
	require.False(t, p.Has(2))
}

func TestPolicyCap(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.Add(1, 1, 1)
	require.Equal(t, int64(9), p.Cap())
}

func TestPolicyUpdate(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.Add(1, 1, 1)
	p.Update(1, 1, 2)
	p.Lock()
	require.Equal(t, int64(2), p.evict.keyCosts[1].cost)
	p.Unlock()
}

func TestPolicyCost(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.Add(1, 1, 2)
	require.Equal(t, int64(2), p.Cost(1))
	require.Equal(t, int64(-1), p.Cost(2))
}

func TestPolicyClear(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.Add(1, 1, 1)
	p.Add(2, 2, 2)
	p.Add(3, 3, 3)
	p.Clear()
	require.Equal(t, int64(10), p.Cap())
	require.False(t, p.Has(1))
	require.False(t, p.Has(2))
	require.False(t, p.Has(3))
}

func TestPolicyClose(t *testing.T) {
	t.Parallel()

	defer func() {
		require.NotNil(t, recover())
	}()

	p := newDefaultPolicy[int, int](100, 10)
	p.Add(1, 1, 1)
	p.Close()
	p.itemsCh <- []uint64{1}
}

func TestPushAfterClose(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.Close()
	require.False(t, p.Push([]uint64{1, 2}))
}

func TestAddAfterClose(t *testing.T) {
	t.Parallel()

	p := newDefaultPolicy[int, int](100, 10)
	p.Close()
	victims, added := p.Add(1, 1, 1)
	require.Nil(t, victims)
	require.False(t, added)
}

func TestSampledLFUAdd(t *testing.T) {
	t.Parallel()

	e := newSampledLFU[int, int](4)
	e.add(1, 1, 1)
	e.add(2, 2, 2)
	e.add(3, 3, 1)
	require.Equal(t, int64(4), e.used)
	require.Equal(t, int64(2), e.keyCosts[2].cost)
	require.Equal(t, 2, e.keyCosts[2].originalKey)
}

func TestSampledLFUDel(t *testing.T) {
	t.Parallel()

	e := newSampledLFU[int, int](4)
	e.add(1, 1, 1)
	e.add(2, 2, 2)
	e.del(2)
	require.Equal(t, int64(1), e.used)
	_, ok := e.keyCosts[2]
	require.False(t, ok)
	e.del(4)
}

func TestSampledLFUUpdate(t *testing.T) {
	t.Parallel()

	e := newSampledLFU[int, int](4)
	e.add(1, 1, 1)
	require.True(t, e.updateIfHas(1, 1, 2))
	require.Equal(t, int64(2), e.used)
	require.Equal(t, int64(2), e.keyCosts[1].cost)
	require.Equal(t, 1, e.keyCosts[1].originalKey)
	require.False(t, e.updateIfHas(2, 2, 2))
}

func TestSampledLFUClear(t *testing.T) {
	t.Parallel()

	e := newSampledLFU[int, int](4)
	e.add(1, 1, 1)
	e.add(2, 2, 2)
	e.add(3, 3, 1)
	e.clear()
	require.Equal(t, 0, len(e.keyCosts))
	require.Equal(t, int64(0), e.used)
}

func TestSampledLFURoom(t *testing.T) {
	t.Parallel()

	e := newSampledLFU[int, int](16)
	e.add(1, 1, 1)
	e.add(2, 2, 2)
	e.add(3, 3, 3)
	require.Equal(t, int64(6), e.roomLeft(4))
}

func TestSampledLFUSample(t *testing.T) {
	t.Parallel()

	e := newSampledLFU[int, int](16)
	for i := 4; i <= 10; i++ {
		e.add(uint64(i), i, int64(i))
	}
	sample := []*policyPair[int]{
		{key: 1, originalKey: 1, cost: 1},
		{key: 2, originalKey: 2, cost: 2},
		{key: 3, originalKey: 3, cost: 3},
	}
	sample = e.fillSample(sample)
	require.LessOrEqual(t, len(sample), lfuSample, "sample size should not exceed lfuSample")
	require.GreaterOrEqual(t, len(sample), 3, "sample should include at least input elements")
	keys := make(map[uint64]bool)
	for _, pair := range sample {
		keys[pair.key] = true
	}
	for i := 1; i <= 3; i++ {
		require.True(t, keys[uint64(i)], "input key %d should be in sample", i)
	}
	addedFromKeyCosts := len(sample) - 3
	require.LessOrEqual(t, addedFromKeyCosts, 7, "too many elements added from keyCosts")
	e.del(5)
	sample = e.fillSample(sample[:3])
	require.LessOrEqual(t, len(sample), lfuSample, "sample size should not exceed lfuSample after deletion")
}

func TestTinyLFUIncrement(t *testing.T) {
	t.Parallel()

	a := newTinyLFU(4)
	a.Increment(1)
	a.Increment(1)
	a.Increment(1)
	require.True(t, a.door.Has(1))
	require.Equal(t, int64(2), a.freq.Estimate(1))

	a.Increment(1)
	require.False(t, a.door.Has(1))
	require.Equal(t, int64(1), a.freq.Estimate(1))
}

func TestTinyLFUEstimate(t *testing.T) {
	t.Parallel()

	a := newTinyLFU(8)
	a.Increment(1)
	a.Increment(1)
	a.Increment(1)
	require.Equal(t, int64(3), a.Estimate(1))
	require.Equal(t, int64(0), a.Estimate(2))
}

func TestTinyLFUPush(t *testing.T) {
	t.Parallel()

	a := newTinyLFU(16)
	a.Push([]uint64{1, 2, 2, 3, 3, 3})
	require.Equal(t, int64(1), a.Estimate(1))
	require.Equal(t, int64(2), a.Estimate(2))
	require.Equal(t, int64(3), a.Estimate(3))
	require.Equal(t, int64(6), a.incrs)
}

func TestTinyLFUClear(t *testing.T) {
	t.Parallel()

	a := newTinyLFU(16)
	a.Push([]uint64{1, 3, 3, 3})
	a.clear()
	require.Equal(t, int64(0), a.incrs)
	require.Equal(t, int64(0), a.Estimate(3))
}
