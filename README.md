# PySyun Pipeline & PySyun Chain for Golang

A minimalist Go implementation of the [PySyun](https://github.com/pysyun) data processing pipeline ecosystem.

This package ports two Python libraries to idiomatic Go:

- **[pysyun-timeline](https://github.com/pysyun/pysyun-timeline)** — graph-based time-series analysis pipelines (
  sources → converters → filters → segmenters → algebra → reducers), represented here as `PipelineNode` with fan-out,
  profiling, and recursive activation.
- **[pysyun_chain](https://github.com/pysyun/pysyun_chain)** — linear pipeline composition with synchronous and parallel
  execution, represented here as `Chainable` and `ChainableGroup`.

Python's `ThreadPoolExecutor` and `asyncio` are replaced by **goroutines** and `sync.WaitGroup` with a channel-based
semaphore for concurrency control.

## Installation

```bash
go get github.com/pysyun/go_pysyun_pipeline
```

## Quick Start

### Synchronous Pipeline

```go
import pysyun "github.com/pysyun/go_pysyun_pipeline"

pipeline := pysyun.NewChainable(ProcessorA{}).
Pipe(pysyun.NewChainable(ProcessorB{})).
Pipe(pysyun.NewChainable(ProcessorC{}))

result := pipeline.Process(input)
```

Or with the variadic helper:

```go
pipeline := pysyun.Pipe(
pysyun.NewChainable(ProcessorA{}),
pysyun.NewChainable(ProcessorB{}),
pysyun.NewChainable(ProcessorC{}),
)
```

### Parallel Fan-Out

```go
group := pysyun.NewChainableGroup(4). // 4 concurrent goroutines
Pipe(pysyun.NewChainable(HeavyProcessor{}))

results := group.Process(inputSlice) // input order preserved
```

Pass `0` or a negative value to spawn one goroutine per input item (unlimited concurrency):

```go
group := pysyun.NewChainableGroup(0)
```

### Graph-Based Pipeline with Profiling

```go
source := pysyun.NewPipelineNode(FetchProcessor{})
filter := pysyun.NewPipelineNode(FilterProcessor{})
output := pysyun.NewPipelineNode(RenderProcessor{})

source.Add(filter)
filter.Add(output)

source.Write(initialData)
source.Activate() // processes + propagates through the graph

fmt.Println(filter.LastDelta.Duration) // wall-clock time of filter stage
fmt.Println(filter.LastDelta.ItemDelta) // output count minus input count
```

### Graph Fan-Out

```go
source := pysyun.NewPipelineNode(SourceProcessor{})
branchA := pysyun.NewPipelineNode(ProcessorA{})
branchB := pysyun.NewPipelineNode(ProcessorB{})

source.Add(branchA)
source.Add(branchB)

source.Write(data)
source.Activate() // both branches receive source output
```

## The Processor Contract

Every stage implements a single interface:

```go
type Processor interface {
Process(data any) any
}
```

Plain functions work too via `ProcessorFunc`:

```go
negate := pysyun.ProcessorFunc(func (data any) any {
return -data.(int)
})

pipeline := pysyun.NewChainable(negate)
```

## API Reference

### `Processor`

```go
type Processor interface {
Process(data any) any
}
```

Universal processing contract implemented by every pipeline stage.

### `ProcessorFunc`

```go
type ProcessorFunc func (data any) any
```

Adapts a plain function to the `Processor` interface.

### `Chainable` — Synchronous Linear Stage

| Function / Method                                 | Description                                      |
|---------------------------------------------------|--------------------------------------------------|
| `NewChainable(p Processor) *Chainable`            | Wraps a processor in a pipeline stage            |
| `(c *Chainable) Process(data any) any`            | Runs the wrapped processor                       |
| `(c *Chainable) Pipe(next *Chainable) *Chainable` | Connects `next` after this stage; returns `next` |

### `ChainableGroup` — Parallel Fan-Out

| Function / Method                                           | Description                                                                 |
|-------------------------------------------------------------|-----------------------------------------------------------------------------|
| `NewChainableGroup(concurrency int) *ChainableGroup`        | Creates a parallel executor; `concurrency ≤ 0` means one goroutine per item |
| `(g *ChainableGroup) Pipe(next *Chainable) *ChainableGroup` | Appends a stage to the internal pipeline                                    |
| `(g *ChainableGroup) Process(data []any) []any`             | Distributes items across goroutines; preserves input order                  |

### `PipelineNode` — Graph Node with Profiling

| Function / Method                                             | Description                                                                   |
|---------------------------------------------------------------|-------------------------------------------------------------------------------|
| `NewPipelineNode(p Processor) *PipelineNode`                  | Creates a graph node wrapping a processor                                     |
| `(n *PipelineNode) Add(neighbor *PipelineNode) *PipelineNode` | Adds a downstream neighbor; returns `neighbor`                                |
| `(n *PipelineNode) Write(data any)`                           | Stores data in the node's input buffer                                        |
| `(n *PipelineNode) Read() any`                                | Returns the current buffer contents                                           |
| `(n *PipelineNode) Activate()`                                | Processes the buffer, updates `LastDelta`, propagates output to all neighbors |
| `(n *PipelineNode) LastDelta`                                 | `Delta` struct populated after each `Activate()` call                         |

### `Delta` — Profiling Data

```go
type Delta struct {
ItemDelta int           // output item count minus input item count
Duration  time.Duration // wall-clock time spent in Process
}
```

### `Pipe` — Variadic Chaining Helper

```go
func Pipe(stages ...*Chainable) *Chainable
```

Chains multiple stages left-to-right. Returns an identity pass-through when called with no arguments.

## Architecture

| Primitive        | Python Equivalent                | Purpose                                         |
|------------------|----------------------------------|-------------------------------------------------|
| `Processor`      | `.process(data)` duck type       | Universal processing contract                   |
| `ProcessorFunc`  | —                                | Adapt a plain function to `Processor`           |
| `Chainable`      | `pysyun_chain.Chainable`         | Synchronous linear pipeline stage               |
| `ChainableGroup` | `pysyun_chain.ChainableGroup`    | Parallel fan-out via goroutines + WaitGroup     |
| `PipelineNode`   | `pysyun-timeline` `PipelineNode` | Graph node with profiling and recursive fan-out |
| `Pipe()`         | `a \| b \| c`                    | Variadic chaining helper                        |

## Testing

```bash
go test -v ./...
```

The test suite covers synchronous pipelines, order preservation, concurrency limiting, parallel speedup (verified at ~8×
with 8 goroutines), graph fan-out, and profiling.

## License

LGPL-2.1 — same as [pysyun_chain](https://github.com/pysyun/pysyun_chain).

## Credits

**© [PySyun](https://github.com/pysyun)**
