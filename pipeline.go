// Package pipeline implements data processing pipelines in Go.
//
// This is a Go port of the PySyun pipeline ecosystem:
//
//   - PySyun Timeline — https://github.com/pysyun/pysyun-timeline
//     Graph-based time-series analysis pipelines (sources, converters,
//     filters, segmenters, algebra, reducers).
//
//   - PySyun Chain — https://github.com/pysyun/pysyun_chain
//     Linear pipeline composition with synchronous, asynchronous, and
//     parallel execution modes.
//
// The Go implementation preserves the core PySyun design — every
// processing stage is a Processor with a single Process method, and
// pipelines are assembled by chaining processors together. Goroutines
// and sync.WaitGroup replace Python's ThreadPoolExecutor and asyncio.
//
// © PySyun — https://github.com/pysyun
package pipeline

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Processor interface
// ---------------------------------------------------------------------------

// Processor is the universal processing contract shared by every component
// in the PySyun ecosystem. Any value that implements Process can participate
// in a pipeline — sources, filters, segmenters, reducers, or anything else.
type Processor interface {
	Process(data any) any
}

// ProcessorFunc adapts a plain function to the Processor interface.
type ProcessorFunc func(data any) any

// Process calls the underlying function.
func (f ProcessorFunc) Process(data any) any { return f(data) }

// ---------------------------------------------------------------------------
// Chainable — synchronous linear pipeline
// ---------------------------------------------------------------------------

// Chainable wraps a Processor and supports pipe-style composition.
//
// Equivalent to pysyun_chain.Chainable in Python.
type Chainable struct {
	processor Processor
}

// NewChainable creates a pipeline stage from any Processor.
func NewChainable(p Processor) *Chainable {
	return &Chainable{processor: p}
}

// Process delegates to the wrapped Processor.
func (c *Chainable) Process(data any) any {
	return c.processor.Process(data)
}

// Pipe connects two Chainable stages into a sequential pipeline.
// This is the Go equivalent of Python's __or__ (|) operator:
//
//	pipeline := a.Pipe(b).Pipe(c)
//	result := pipeline.Process(input)
func (c *Chainable) Pipe(next *Chainable) *Chainable {
	return NewChainable(&chainedProcessor{
		first:  c.processor,
		second: next.processor,
	})
}

// chainedProcessor glues two processors into a sequential pair.
// Equivalent to pysyun_chain.ChainedProcessor.
type chainedProcessor struct {
	first  Processor
	second Processor
}

func (cp *chainedProcessor) Process(data any) any {
	return cp.second.Process(cp.first.Process(data))
}

// ---------------------------------------------------------------------------
// ChainableGroup — parallel fan-out over goroutines + WaitGroup
// ---------------------------------------------------------------------------

// ChainableGroup distributes input items across goroutines, applying the
// attached pipeline to each item independently. It uses sync.WaitGroup
// for coordination and a buffered semaphore channel for concurrency control.
//
// Equivalent to pysyun_chain.ChainableGroup, replacing Python's
// ThreadPoolExecutor with goroutines.
type ChainableGroup struct {
	concurrency int
	pipeline    *Chainable
}

// NewChainableGroup creates a parallel executor.
// If concurrency <= 0, every input item gets its own goroutine.
func NewChainableGroup(concurrency int) *ChainableGroup {
	return &ChainableGroup{concurrency: concurrency}
}

// Pipe appends a processing stage to the group's internal pipeline.
// Calling group.Pipe(a).Pipe(b) builds the per-item pipeline a → b.
func (g *ChainableGroup) Pipe(next *Chainable) *ChainableGroup {
	if g.pipeline == nil {
		g.pipeline = next
	} else {
		g.pipeline = g.pipeline.Pipe(next)
	}
	return g
}

// Process fans out over the input slice, applying the pipeline to each
// element concurrently. Results preserve input order.
func (g *ChainableGroup) Process(data []any) []any {
	n := len(data)
	results := make([]any, n)

	workers := g.concurrency
	if workers <= 0 {
		workers = n
	}

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{} // acquire slot
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }() // release slot
			results[idx] = g.pipeline.Process(data[idx])
		}(i)
	}

	wg.Wait()
	return results
}

// ---------------------------------------------------------------------------
// PipelineNode — graph-based node with profiling
// ---------------------------------------------------------------------------

// Delta holds profiling data produced by a PipelineNode after processing.
// Mirrors the delta dict from pysyun-timeline's pipeline.py.
type Delta struct {
	ItemDelta int           // change in item count (output − input)
	Duration  time.Duration // wall-clock time spent in Process
}

// PipelineNode wraps a Processor with read/write buffers and profiling,
// replicating the graph-node model from pysyun-timeline's pipeline.py.
//
// Neighbors are connected with Add; Activate triggers processing and
// propagates data along the graph edges.
type PipelineNode struct {
	processor Processor
	neighbors []*PipelineNode
	buffer    any
	LastDelta Delta
}

// NewPipelineNode creates a graph node from any Processor.
func NewPipelineNode(p Processor) *PipelineNode {
	return &PipelineNode{processor: p}
}

// Add connects a downstream neighbor and returns the receiver for chaining.
func (n *PipelineNode) Add(neighbor *PipelineNode) *PipelineNode {
	n.neighbors = append(n.neighbors, neighbor)
	return n
}

// Write stores data into the node's buffer (used by upstream nodes).
func (n *PipelineNode) Write(data any) {
	n.buffer = data
}

// Read returns the current buffer contents.
func (n *PipelineNode) Read() any {
	return n.buffer
}

// Activate processes the buffered data, records profiling deltas, stores
// the result back into the node's own buffer, pushes the result to all
// neighbors, and recursively activates them.
func (n *PipelineNode) Activate() {
	input := n.Read()

	startLen := sliceLen(input)
	start := time.Now()

	output := n.processor.Process(input)

	n.LastDelta = Delta{
		ItemDelta: sliceLen(output) - startLen,
		Duration:  time.Since(start),
	}

	// Store processed result so Read() returns output, not input.
	n.buffer = output

	for _, neighbor := range n.neighbors {
		neighbor.Write(output)
		neighbor.Activate()
	}
}

// sliceLen returns the length of data if it is a slice, otherwise 0.
func sliceLen(v any) int {
	if v == nil {
		return 0
	}
	if s, ok := v.([]any); ok {
		return len(s)
	}
	return 0
}

// ---------------------------------------------------------------------------
// Pipe — top-level helper for fluent pipeline construction
// ---------------------------------------------------------------------------

// Pipe is a convenience function for chaining multiple processors:
//
//	pipeline := pysyun.Pipe(a, b, c)
//	result := pipeline.Process(input)
func Pipe(stages ...*Chainable) *Chainable {
	if len(stages) == 0 {
		return NewChainable(ProcessorFunc(func(d any) any { return d }))
	}
	result := stages[0]
	for _, s := range stages[1:] {
		result = result.Pipe(s)
	}
	return result
}
