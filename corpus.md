# PySyun Pipeline & Chain for Go — Integration Corpus

A compact, idiomatic Go port of the PySyun pipeline ecosystem.

- Graph-based pipelines (pysyun-timeline) → PipelineNode: fan-out, profiling, recursive activation
- Linear pipelines (pysyun_chain) → Chainable and ChainableGroup: synchronous and parallel execution

Goroutines and sync.WaitGroup replace Python’s asyncio/ThreadPoolExecutor.

Repository: github.com/pysyun/go_pysyun_pipeline  
Module path and import alias used below: pysyun "github.com/pysyun/go_pysyun_pipeline"

--------------------------------------------------------------------------------

## Installation

```bash
go get github.com/pysyun/go_pysyun_pipeline
```

Recommended Go version: 1.19+ (tests use sync/atomic typed counters).

--------------------------------------------------------------------------------

## Core Concepts

- Processor: universal processing contract; any stage can participate by implementing Process(data any) any.
- ProcessorFunc: functional adapter to quickly create processors from functions.
- Chainable: synchronous linear composition for end-to-end batch processing.
- ChainableGroup: parallel per-item fan-out with bounded concurrency and order preservation.
- PipelineNode: graph node abstraction with fan-out and simple profiling (item delta, duration).
- Pipe(...): convenience helper to chain multiple Chainable stages variadically.

--------------------------------------------------------------------------------

## Public API (signatures)

```go
package pipeline

type Processor interface {
    Process(data any) any
}

type ProcessorFunc func(data any) any

type Chainable struct {
    // NewChainable, Process, Pipe
}
func NewChainable(p Processor) *Chainable
func (c *Chainable) Process(data any) any
func (c *Chainable) Pipe(next *Chainable) *Chainable

type ChainableGroup struct {
    // NewChainableGroup, Pipe, Process
}
func NewChainableGroup(concurrency int) *ChainableGroup
func (g *ChainableGroup) Pipe(next *Chainable) *ChainableGroup
func (g *ChainableGroup) Process(data []any) []any

type Delta struct {
    ItemDelta int
    Duration  time.Duration
}

type PipelineNode struct {
    LastDelta Delta
    // NewPipelineNode, Add, Write, Read, Activate
}
func NewPipelineNode(p Processor) *PipelineNode
func (n *PipelineNode) Add(neighbor *PipelineNode) *PipelineNode
func (n *PipelineNode) Write(data any)
func (n *PipelineNode) Read() any
func (n *PipelineNode) Activate()

func Pipe(stages ...*Chainable) *Chainable
```

Notes
- Pipe() with no arguments returns an identity stage (pass-through).
- ChainableGroup requires at least one stage via Pipe(...) before calling Process.

--------------------------------------------------------------------------------

## Quick Start

### Synchronous linear pipeline

```go
import pysyun "github.com/pysyun/go_pysyun_pipeline"

// Example processors
type double struct{}
func (double) Process(d any) any {
    xs := d.([]any)
    out := make([]any, len(xs))
    for i, v := range xs {
        out[i] = v.(int) * 2
    }
    return out
}

type addN struct{ n int }
func (a addN) Process(d any) any {
    xs := d.([]any)
    out := make([]any, len(xs))
    for i, v := range xs {
        out[i] = v.(int) + a.n
    }
    return out
}

pipeline := pysyun.NewChainable(double{}).
    Pipe(pysyun.NewChainable(addN{n: 10}))

result := pipeline.Process([]any{1, 2, 3}).([]any) // [12 14 16]
```

Variadic helper:

```go
pipeline := pysyun.Pipe(
    pysyun.NewChainable(double{}),
    pysyun.NewChainable(addN{n: 10}),
    pysyun.NewChainable(double{}),
)
out := pipeline.Process([]any{1}).([]any) // [24]
```

### Parallel per-item fan-out

```go
// Per-item processor: work on a single element
type squareItem struct{}
func (squareItem) Process(d any) any { n := d.(int); return n * n }

// Build a per-item pipeline (1 stage here)
group := pysyun.NewChainableGroup(4). // up to 4 concurrent items
    Pipe(pysyun.NewChainable(squareItem{}))

items := []any{2, 3, 4, 5}
results := group.Process(items) // [4 9 16 25], input order preserved
```

Unlimited/spawn-per-item concurrency (bounded by input length):

```go
group := pysyun.NewChainableGroup(0). // or a negative value
    Pipe(pysyun.NewChainable(squareItem{}))
```

Multi-stage per-item pipeline:

```go
group := pysyun.NewChainableGroup(4).
    Pipe(pysyun.NewChainable(squareItem{})).
    Pipe(pysyun.NewChainable(squareItem{})) // x -> x^2 -> x^4
results := group.Process([]any{2, 3}) // [16 81]
```

### Graph-style pipeline with profiling

```go
type double struct{}
func (double) Process(d any) any {
    xs := d.([]any)
    out := make([]any, len(xs))
    for i, v := range xs { out[i] = v.(int) * 2 }
    return out
}

type addN struct{ n int }
func (a addN) Process(d any) any {
    xs := d.([]any)
    out := make([]any, len(xs))
    for i, v := range xs { out[i] = v.(int) + a.n }
    return out
}

src := pysyun.NewPipelineNode(double{})
dst := pysyun.NewPipelineNode(addN{n: 100})
src.Add(dst)

src.Write([]any{5})
src.Activate()

out := dst.Read().([]any) // [110]
dur := dst.LastDelta.Duration
delta := dst.LastDelta.ItemDelta // output count - input count (for []any)
```

Fan-out:

```go
source := pysyun.NewPipelineNode(double{})
a := pysyun.NewPipelineNode(addN{n: 1})
b := pysyun.NewPipelineNode(addN{n: 1000})
source.Add(a).Add(b)

source.Write([]any{5})
source.Activate()
_ = a.Read() // [11]
_ = b.Read() // [1010]
```

--------------------------------------------------------------------------------

## ProcessorFunc and ad-hoc stages

```go
negate := pysyun.ProcessorFunc(func(d any) any {
    xs := d.([]any)
    out := make([]any, len(xs))
    for i, v := range xs { out[i] = -v.(int) }
    return out
})

pipeline := pysyun.NewChainable(negate).
    Pipe(pysyun.NewChainable(pysyun.ProcessorFunc(func(d any) any {
        xs := d.([]any)
        out := make([]any, len(xs))
        for i, v := range xs { out[i] = v.(int) * 2 }
        return out
    })))

out := pipeline.Process([]any{3, 7}).([]any) // [-6 -14]
```

--------------------------------------------------------------------------------

## Integration Guidelines

Data typing and conversion
- The library uses any. Stages must agree on input/output types.
- For batch-style Chainable pipelines, tests and examples use []any containing per-item payloads (e.g., ints).
- For per-item ChainableGroup pipelines, each stage Process should accept and return a single element (e.g., int).
- Bridge typed slices with adapters:
    - Batch to per-item: split []T into []any, run ChainableGroup, then cast results back to []T.
    - Per-item to batch: implement a stage that maps []any -> []any.

Thread-safety and concurrency
- Chainable is synchronous; no special synchronization required.
- ChainableGroup runs your per-item pipeline concurrently. Ensure your stages are thread-safe and avoid shared mutable state; protect shared resources with sync.Mutex or atomic operations.
- Results preserve input order by writing into indexed positions.
- Concurrency is limited by a buffered semaphore channel; concurrency <= 0 maps to one goroutine per input item.

Error handling
- Processor.Process returns any; no built-in error channel. Recommended patterns:
    - Return a struct { Val T; Err error } per item.
    - Return a sentinel error value inside any and handle downstream.
    - Use panic/recover wrappers only with care.
- If needed, wrap processors with an adapter stage that records/logs errors.

Resource management and cancellation
- No built-in context propagation. If you need cancellation/timeouts:
    - Include context.Context in your data payload.
    - Or implement a top-level coordinator that cancels work before invoking Process.
- For long-running tasks in ChainableGroup, design processors to periodically check a Context present in the input payload.

Backpressure and queues
- ChainableGroup fans out over the provided slice; it does not stream. Backpressure is not modeled beyond the concurrency gate.
- For streaming/topology-level backpressure, wrap this library or introduce channels/actors around it.

Immutability and data sharing
- PipelineNode writes the same output reference to all neighbors; if neighbors mutate in-place, make defensive copies.
- ChainableGroup writes independently per index; ensure each item’s processor result does not share unsafe mutable state.

--------------------------------------------------------------------------------

## Behavioral Guarantees and Limitations

- ChainableGroup requires a non-nil per-item pipeline (at least one Pipe call) before Process; calling Process without stages will panic due to nil dereference.
- Pipe() with no arguments returns an identity stage (pass-through).
- PipelineNode profiling ItemDelta is computed only when input/output are of type []any; otherwise ItemDelta is 0.
- PipelineNode is designed for single-threaded Activate/Read/Write flows; do not concurrently call Activate on the same node.
- Order preservation: ChainableGroup preserves the input order in its output slice.
- Fan-out: PipelineNode performs a simple broadcast (write and recursively Activate) to all downstream neighbors.

--------------------------------------------------------------------------------

## Performance Notes

- ChainableGroup parallelizes per-item work. Expect near-linear speedup up to available CPU and I/O limits for CPU-bound or latency-heavy stages.
- Use a reasonable concurrency value to avoid oversubscription; use 0 (spawn-per-item) judiciously for small inputs or highly I/O-bound stages.
- Minimize allocations within hot per-item processors. Reuse buffers where possible; be careful about cross-goroutine sharing.

--------------------------------------------------------------------------------

## Example Processors from Tests (for reference)

```go
type doubleProcessor struct{}
func (doubleProcessor) Process(data any) any {
    items := data.([]any)
    out := make([]any, len(items))
    for i, v := range items {
        out[i] = v.(int) * 2
    }
    return out
}

type addProcessor struct{ n int }
func (a addProcessor) Process(data any) any {
    items := data.([]any)
    out := make([]any, len(items))
    for i, v := range items {
        out[i] = v.(int) + a.n
    }
    return out
}

type squareItemProcessor struct{}
func (squareItemProcessor) Process(data any) any {
    n := data.(int)
    return n * n
}

type slowSquareProcessor struct{}
func (slowSquareProcessor) Process(data any) any {
    time.Sleep(10 * time.Millisecond)
    n := data.(int)
    return n * n
}
```

--------------------------------------------------------------------------------

## Testing

```bash
go test -v ./...
```

The test suite covers:
- Synchronous composition and multi-stage chaining
- Variadic Pipe, identity pass-through for empty pipelines
- Parallel speedup and input-order preservation
- Concurrency limiting under load
- Graph fan-out and profiling (duration and item delta)
- An Example() function that appears in go doc

--------------------------------------------------------------------------------

## FAQ

- Can I chain ChainableGroup with Chainable directly?  
  ChainableGroup builds a per-item pipeline (each stage must be per-item). It operates on []any input/output. If you need batch stages before/after, wrap them externally or provide an adapter stage to split/combine batches.

- How do I propagate errors?  
  Encode errors in your item payloads (e.g., struct with value and error) or attach a logging/wrapping adapter stage.

- Does PipelineNode run concurrently?  
  No; Activate is synchronous and recursively activates neighbors. Introduce concurrency in your processors if needed.

- Can I use typed slices instead of []any?  
  Yes, internally you control your Processor implementations. This library does not enforce []any for per-item processors; tests use it as a convenient convention. Ensure upstream/downstream stages agree on types.

--------------------------------------------------------------------------------

## License and Credits

- License: LGPL-2.1 (aligned with pysyun_chain)
- © PySyun — https://github.com/pysyun
- Module: github.com/pysyun/go_pysyun_pipeline

For issues, improvements, or contributions, please open PRs or issues on the repository.

