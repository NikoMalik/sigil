/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package ristretto

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/NikoMalik/sigil/z"
)

const (
	// lfuSample is the number of items to sample when looking at eviction
	// candidates. 5 seems to be the most optimal number [citation needed].
	lfuSample = 5
)

func newPolicy[K Key, V any](numCounters, maxCost int64) *defaultPolicy[K, V] {
	return newDefaultPolicy[K, V](numCounters, maxCost)
}

type defaultPolicy[K Key, V any] struct {
	sync.Mutex
	admit    *tinyLFU
	evict    *sampledLFU[K, V]
	itemsCh  chan []uint64
	stop     chan struct{}
	done     chan struct{}
	isClosed bool
	metrics  *Metrics
}

func newDefaultPolicy[K Key, V any](numCounters, maxCost int64) *defaultPolicy[K, V] {
	p := &defaultPolicy[K, V]{
		admit:   newTinyLFU(numCounters),
		evict:   newSampledLFU[K, V](maxCost),
		itemsCh: make(chan []uint64, 3),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go p.processItems()
	return p
}

func (p *defaultPolicy[K, V]) CollectMetrics(metrics *Metrics) {
	p.metrics = metrics
	p.evict.metrics = metrics
}

type policyPair[K Key] struct {
	key         uint64
	originalKey K
	cost        int64
}

func (p *defaultPolicy[K, V]) processItems() {
	for {
		select {
		case items := <-p.itemsCh:
			p.Lock()
			p.admit.Push(items)
			p.Unlock()
		case <-p.stop:
			p.done <- struct{}{}
			return
		}
	}
}

func (p *defaultPolicy[K, V]) Push(keys []uint64) bool {
	if p.isClosed {
		return false
	}

	if len(keys) == 0 {
		return true
	}

	select {
	case p.itemsCh <- keys:
		p.metrics.add(keepGets, keys[0], uint64(len(keys)))
		return true
	default:
		p.metrics.add(dropGets, keys[0], uint64(len(keys)))
		return false
	}
}

// Add decides whether the item with the given key and cost should be accepted by
// the policy. It returns the list of victims that have been evicted and a boolean
// indicating whether the incoming item should be accepted.
func (p *defaultPolicy[K, V]) Add(key uint64, originalKey K, cost int64) ([]*Item[K, V], bool) {
	p.Lock()
	defer p.Unlock()

	if p.isClosed {
		return nil, false
	}

	if cost > p.evict.getMaxCost() {
		// fmt.Printf("Rejected key=%d due to cost=%d > maxCost=%d\n", key, cost, p.evict.getMaxCost())
		return nil, false
	}

	if has := p.evict.updateIfHas(key, originalKey, cost); has {
		// fmt.Printf("Updated existing key=%d, cost=%d\n", key, cost)
		return nil, false
	}

	room := p.evict.roomLeft(cost)
	if room >= 0 {
		p.evict.add(key, originalKey, cost)
		p.metrics.add(costAdd, key, uint64(cost))
		// fmt.Printf("Added key=%d, cost=%d (room=%d)\n", key, cost, room)
		return nil, true
	}

	incHits := p.admit.Estimate(key)
	sample := make([]*policyPair[K], 0, lfuSample)
	victims := make([]*Item[K, V], 0)

	for ; room < 0; room = p.evict.roomLeft(cost) {
		sample = p.evict.fillSample(sample)
		minKey, minHits, minId, minCost := uint64(0), int64(math.MaxInt64), 0, int64(0)
		var minOriginalKey K
		for i, pair := range sample {
			if hits := p.admit.Estimate(pair.key); hits < minHits {
				minKey, minHits, minId, minCost = pair.key, hits, i, pair.cost
				minOriginalKey = pair.originalKey
			}
		}

		if incHits < minHits {
			p.metrics.add(rejectSets, key, 1)
			// fmt.Printf("Rejected key=%d (incHits=%d < minHits=%d)\n", key, incHits, minHits)
			return victims, false
		}

		p.evict.del(minKey)
		sample[minId] = sample[len(sample)-1]
		sample = sample[:len(sample)-1]
		victims = append(victims, &Item[K, V]{
			Key:         minKey,
			OriginalKey: minOriginalKey,
			Conflict:    0,
			Cost:        minCost,
		})
		// fmt.Printf("Evicted key=%d (minHits=%d, cost=%d)\n", minKey, minHits, minCost)
	}

	p.evict.add(key, originalKey, cost)
	p.metrics.add(costAdd, key, uint64(cost))
	// fmt.Printf("Added key=%d, cost=%d after eviction\n", key, cost)
	return victims, true
}

func (p *defaultPolicy[K, V]) Has(key uint64) bool {
	p.Lock()
	_, exists := p.evict.keyCosts[key]
	p.Unlock()
	return exists
}

func (p *defaultPolicy[K, V]) Del(key uint64) {
	p.Lock()
	p.evict.del(key)
	p.Unlock()
}

func (p *defaultPolicy[K, V]) Cap() int64 {
	p.Lock()
	capacity := p.evict.getMaxCost() - p.evict.used
	p.Unlock()
	return capacity
}

func (p *defaultPolicy[K, V]) Update(key uint64, originalKey K, cost int64) {
	p.Lock()
	p.evict.updateIfHas(key, originalKey, cost)
	p.Unlock()
}

func (p *defaultPolicy[K, V]) Cost(key uint64) int64 {
	p.Lock()
	if pair, found := p.evict.keyCosts[key]; found {
		p.Unlock()
		return pair.cost
	}
	p.Unlock()
	return -1
}

func (p *defaultPolicy[K, V]) Clear() {
	p.Lock()
	p.admit.clear()
	p.evict.clear()
	p.Unlock()
}

func (p *defaultPolicy[K, V]) Close() {
	if p.isClosed {
		return
	}

	// Block until the p.processItems goroutine returns.
	p.stop <- struct{}{}
	<-p.done
	close(p.stop)
	close(p.done)
	close(p.itemsCh)
	p.isClosed = true
}

func (p *defaultPolicy[K, V]) MaxCost() int64 {
	if p == nil || p.evict == nil {
		return 0
	}
	return p.evict.getMaxCost()
}

func (p *defaultPolicy[K, V]) UpdateMaxCost(maxCost int64) {
	if p == nil || p.evict == nil {
		return
	}
	p.evict.updateMaxCost(maxCost)
}

// sampledLFU is an eviction helper storing key-cost pairs.
type sampledLFU[K Key, V any] struct {
	// NOTE: align maxCost to 64-bit boundary for use with atomic.
	// As per https://golang.org/pkg/sync/atomic/: "On ARM, x86-32,
	// and 32-bit MIPS, it is the caller’s responsibility to arrange
	// for 64-bit alignment of 64-bit words accessed atomically.
	// The first word in a variable or in an allocated struct, array,
	// or slice can be relied upon to be 64-bit aligned."
	maxCost  int64
	used     int64
	metrics  *Metrics
	keyCosts map[uint64]policyPair[K]
}

func newSampledLFU[K Key, V any](maxCost int64) *sampledLFU[K, V] {
	return &sampledLFU[K, V]{
		keyCosts: make(map[uint64]policyPair[K]),
		maxCost:  maxCost,
	}
}
func (p *sampledLFU[K, V]) getMaxCost() int64 {
	return atomic.LoadInt64(&p.maxCost)
}

func (p *sampledLFU[K, V]) updateMaxCost(maxCost int64) {
	atomic.StoreInt64(&p.maxCost, maxCost)
}

func (p *sampledLFU[K, V]) roomLeft(cost int64) int64 {
	return p.getMaxCost() - (p.used + cost)
}

func (p *sampledLFU[K, V]) fillSample(in []*policyPair[K]) []*policyPair[K] {
	if len(in) >= lfuSample {
		return in
	}
	for key, pair := range p.keyCosts {
		in = append(in, &policyPair[K]{key: key, originalKey: pair.originalKey, cost: pair.cost})
		if len(in) >= lfuSample {
			return in
		}
	}
	return in
}

func (p *sampledLFU[K, V]) del(key uint64) {
	pair, ok := p.keyCosts[key]
	if !ok {
		return
	}
	p.used -= pair.cost
	delete(p.keyCosts, key)
	p.metrics.add(costEvict, key, uint64(pair.cost))
	p.metrics.add(keyEvict, key, 1)
}

func (p *sampledLFU[K, V]) add(key uint64, originalKey K, cost int64) {
	p.keyCosts[key] = policyPair[K]{key: key, originalKey: originalKey, cost: cost}
	p.used += cost
}

func (p *sampledLFU[K, V]) updateIfHas(key uint64, originalKey K, cost int64) bool {
	if prev, found := p.keyCosts[key]; found {
		p.metrics.add(keyUpdate, key, 1)
		if prev.cost > cost {
			diff := prev.cost - cost
			p.metrics.add(costAdd, key, ^(uint64(diff) - 1))
		} else if cost > prev.cost {
			diff := cost - prev.cost
			p.metrics.add(costAdd, key, uint64(diff))
		}
		p.used += cost - prev.cost
		p.keyCosts[key] = policyPair[K]{key: key, originalKey: originalKey, cost: cost}
		return true
	}
	return false
}

func (p *sampledLFU[K, V]) clear() {
	p.used = 0
	p.keyCosts = make(map[uint64]policyPair[K])
}

// tinyLFU is an admission helper that keeps track of access frequency using
// tiny (4-bit) counters in the form of a count-min sketch.
// tinyLFU is NOT thread safe.
type tinyLFU struct {
	freq    *cmSketch
	door    *z.Bloom
	incrs   int64
	resetAt int64
}

func newTinyLFU(numCounters int64) *tinyLFU {
	return &tinyLFU{
		freq:    newCmSketch(numCounters),
		door:    z.NewBloomFilter(float64(numCounters), 0.01),
		resetAt: numCounters,
	}
}

func (p *tinyLFU) Push(keys []uint64) {
	for _, key := range keys {
		p.Increment(key)
	}
}

func (p *tinyLFU) Estimate(key uint64) int64 {
	hits := p.freq.Estimate(key)
	if p.door.Has(key) {
		hits++
	}
	return hits
}

func (p *tinyLFU) Increment(key uint64) {
	// Flip doorkeeper bit if not already done.
	if added := p.door.AddIfNotHas(key); !added {
		// Increment count-min counter if doorkeeper bit is already set.
		p.freq.Increment(key)
	}
	p.incrs++
	if p.incrs >= p.resetAt {
		p.reset()
	}
}

func (p *tinyLFU) reset() {
	// Zero out incrs.
	p.incrs = 0
	// clears doorkeeper bits
	p.door.Clear()
	// halves count-min counters
	p.freq.Reset()
}

func (p *tinyLFU) clear() {
	p.incrs = 0
	p.door.Clear()
	p.freq.Clear()
}
