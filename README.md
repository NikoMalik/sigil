# Sigil (fork of Ristretto)

> **This repository is a fork of **[**Ristretto**](https://github.com/hypermodeinc/ristretto) and is published under the name **Sigil**. It preserves the original project's behavior and interfaces but introduces a few focused enhancements listed below.


Sigil is a conservative fork of Ristretto (a fast, concurrent cache library) that maintains the original codebase and APIs while adding two pragmatic features requested by maintainers and users:

- **Iter** — an iterator over the cache that allows callers to visit every key/value pair currently stored in the cache in a safe manner.
- **Automatic reallocation** — background monitoring that will automatically trigger a reallocation (grow the cache capacity) when usage crosses a configurable threshold. The goal is to reduce manual intervention when a cache grows under load.

> Note: This fork intentionally keeps the original semantics and behaviour of Ristretto. Where behaviours change (for example, the semantics of `Iter` or reallocation), they are documented below.

---

## Features (Sigil additions)

- Everything in Ristretto (TinyLFU admission, SampledLFU eviction, batching for throughput).
- **Iter**: an exported method to iterate over all elements currently in the cache. Iter runs with minimal locking to avoid introducing contention; callbacks run outside of internal locks so user code won't deadlock the cache.
- **Automatic reallocation**: optional monitoring goroutine that observes cache occupancy and automatically triggers `Reallocate()` when the cache reaches a configured threshold (for example 90%). This behavior can be disabled in the `Config`.

---

## Status

Sigil is a feature fork; it is intended to be a drop-in replacement for most uses of Ristretto while providing the two extensions above. Use in production requires the same care as for Ristretto: pick sensible `Config` values, enable metrics to observe behaviour, and test workloads.

---

## Getting Started


## Usage (example)
The public API mirrors Ristretto; the two differences are: the `Iter` method and the `DisableAutoReallocate` config fields. Example usage:

```go
package main

import (
  "fmt"

  "github.com/<your-org>/sigil"
)

func main() {
  cache, err := sigil.NewCache(&sigil.Config[string, string]{
    NumCounters: 1e7,
    MaxCost:     1 << 30,
    BufferItems: 64,
    // Optional: enable automatic reallocation (default: enabled)
    // DisableAutoReallocate: false,
  })
  if err != nil {
    panic(err)
  }
  defer cache.Close()

  cache.Set("key", "value", 1)
  cache.Wait()

  value, found := cache.Get("key")
  if !found {
    panic("missing value")
  }
  fmt.Println(value)

  // Iterating over the cache
  cache.Iter(func(k string, v string) (stop bool) {
    fmt.Printf("key=%s value=%s\n", k, v)
    return false // continue
  })
}
```

---

## Automatic reallocation details
Sigil introduces an internal monitor that can be enabled by default. Behaviour summary:

- The monitor periodically checks the cache occupancy (`used / MaxCost`).
- If occupancy >=  (default 0.9) 90%, the cache calls `Reallocate()` which doubles the `MaxCost` and copies live items to a new store.
- Reallocation is guarded by an `atomic` flag; concurrent `Set`/`Get` operations return early when a reallocation is in progress.
- Automatic reallocation can be disabled by setting `DisableAutoReallocate: true` in the `Config`.

Notes and caveats:
- Reallocation is an expensive operation — it iterates all keys and reconstructs the policy/store. It is designed to be rare and off the hot path.
- The reallocation path drains the `setBuf` and temporarily pauses processing; callers should expect short pauses under heavy load while reallocation runs.

---

## Compatibility & Migration
- Sigil keeps Ristretto's API surface but adds some fields 
---

## Benchmarks
Benchmarks for Sigil should be run the same way as Ristretto; the fork inherits the upstream benchmark suite. If you want to compare behaviour with and without automatic reallocation, enable/disable the `DisableAutoReallocate` flag and run the workload.
---

## Projects Using Sigil
This is a new fork; list any consumers here once adopted.

---

## FAQ

### Why a fork?
The goal of Sigil is to provide a minimal, well-scoped set of features on top of Ristretto that are commonly demanded (simple iteration and an optional automatic growth mechanism). Forking lets us iterate on those features while keeping the original project stable.

### Is Sigil distributed?
No — same as Ristretto, Sigil is a single-process in-memory cache library.

---

## License

This fork inherits the original project's license (Apache-2.0). Keep the same licensing terms when reusing code from upstream.
---


