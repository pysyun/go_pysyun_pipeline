# PySyun Chain — Go

A minimalist Go implementation of the [PySyun](https://github.com/pysyun) data processing pipeline ecosystem.

This package ports two Python libraries to idiomatic Go:

- **[pysyun-timeline](https://github.com/pysyun/pysyun-timeline)** — graph-based time-series analysis pipelines (sources → converters → filters → segmenters → algebra → reducers), represented here as `PipelineNode` with fan-out, profiling, and recursive activation.

- **[pysyun_chain](https://github.com/pysyun/pysyun_chain)** — linear pipeline composition with synchronous and parallel execution, represented here as `Chainable` and `ChainableGroup`.

Python's `ThreadPoolExecutor` and `asyncio` are replaced by **goroutines** and **sync.WaitGroup** with a channel-based semaphore for concurrency control.

## Installation

```bash
go get github.com/pysyun/pysyun_chain_go/pysyun
```

## Quick Start

### Synchronous Pipeline

```go
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

### Parallel Fan-Out (Goroutines + WaitGroup)

```go
group := pysyun.NewChainableGroup(4).           // 4 concurrent goroutines
    Pipe(pysyun.NewChainable(HeavyProcessor{}))

results := group.Process(inputSlice)             // order preserved
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

fmt.Println(filter.LastDelta.Duration) // profiling data
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
negate := pysyun.ProcessorFunc(func(data any) any {
    return -data.(int)
})
```

## Architecture

| Primitive | Python Equivalent | Purpose |
|---|---|---|
| `Chainable` | `pysyun_chain.Chainable` | Synchronous linear pipeline stage |
| `ChainableGroup` | `pysyun_chain.ChainableGroup` | Parallel fan-out (goroutines + WaitGroup) |
| `PipelineNode` | `pysyun-timeline PipelineNode` | Graph node with profiling and fan-out |
| `Processor` | `.process(data)` duck type | Universal processing contract |
| `ProcessorFunc` | — | Adapt a function to Processor |
| `Pipe()` | `a | b | c` | Variadic chaining helper |

## Credits

**© [PySyun](https://github.com/pysyun)**

## License

LGPL-2.1 — same as [pysyun_chain](https://github.com/pysyun/pysyun_chain).
