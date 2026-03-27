package pipeline_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pysyun/go_pysyun_pipeline/pipeline"
)

// ---------------------------------------------------------------------------
// Test processors
// ---------------------------------------------------------------------------

// doubleProcessor multiplies every int in a []any by 2.
type doubleProcessor struct{}

func (doubleProcessor) Process(data any) any {
	items := data.([]any)
	out := make([]any, len(items))
	for i, v := range items {
		out[i] = v.(int) * 2
	}
	return out
}

// addProcessor adds a constant to every int in a []any.
type addProcessor struct{ n int }

func (a addProcessor) Process(data any) any {
	items := data.([]any)
	out := make([]any, len(items))
	for i, v := range items {
		out[i] = v.(int) + a.n
	}
	return out
}

// squareItemProcessor squares a single int (for ChainableGroup).
type squareItemProcessor struct{}

func (squareItemProcessor) Process(data any) any {
	n := data.(int)
	return n * n
}

// slowSquareProcessor simulates a heavy per-item operation.
type slowSquareProcessor struct{}

func (slowSquareProcessor) Process(data any) any {
	time.Sleep(10 * time.Millisecond)
	n := data.(int)
	return n * n
}

// counterProcessor counts how many times Process is called (thread-safe).
type counterProcessor struct{ calls *atomic.Int64 }

func (c counterProcessor) Process(data any) any {
	c.calls.Add(1)
	return data
}

// ---------------------------------------------------------------------------
// Chainable (synchronous pipeline)
// ---------------------------------------------------------------------------

func TestChainable_SingleStage(t *testing.T) {
	p := pysyun.NewChainable(doubleProcessor{})
	result := p.Process([]any{1, 2, 3}).([]any)

	expected := []int{2, 4, 6}
	for i, v := range result {
		if v.(int) != expected[i] {
			t.Errorf("index %d: got %d, want %d", i, v.(int), expected[i])
		}
	}
}

func TestChainable_PipeTwoStages(t *testing.T) {
	// double → add(10): 1→2→12, 2→4→14, 3→6→16
	pipeline := pysyun.NewChainable(doubleProcessor{}).
		Pipe(pysyun.NewChainable(addProcessor{n: 10}))

	result := pipeline.Process([]any{1, 2, 3}).([]any)

	expected := []int{12, 14, 16}
	for i, v := range result {
		if v.(int) != expected[i] {
			t.Errorf("index %d: got %d, want %d", i, v.(int), expected[i])
		}
	}
}

func TestChainable_ThreeStages(t *testing.T) {
	// double → add(10) → double: 1→2→12→24
	pipeline := pysyun.Pipe(
		pysyun.NewChainable(doubleProcessor{}),
		pysyun.NewChainable(addProcessor{n: 10}),
		pysyun.NewChainable(doubleProcessor{}),
	)

	result := pipeline.Process([]any{1}).([]any)
	if result[0].(int) != 24 {
		t.Errorf("got %d, want 24", result[0].(int))
	}
}

func TestPipe_Empty(t *testing.T) {
	pipeline := pysyun.Pipe()
	result := pipeline.Process(42)
	if result.(int) != 42 {
		t.Errorf("empty Pipe should pass through; got %v", result)
	}
}

func TestProcessorFunc(t *testing.T) {
	negate := pysyun.ProcessorFunc(func(data any) any {
		items := data.([]any)
		out := make([]any, len(items))
		for i, v := range items {
			out[i] = -v.(int)
		}
		return out
	})

	pipeline := pysyun.NewChainable(negate).
		Pipe(pysyun.NewChainable(doubleProcessor{}))

	result := pipeline.Process([]any{3, 7}).([]any)
	if result[0].(int) != -6 || result[1].(int) != -14 {
		t.Errorf("unexpected result: %v", result)
	}
}

// ---------------------------------------------------------------------------
// ChainableGroup (parallel fan-out via goroutines + WaitGroup)
// ---------------------------------------------------------------------------

func TestChainableGroup_Basic(t *testing.T) {
	group := pysyun.NewChainableGroup(4).
		Pipe(pysyun.NewChainable(squareItemProcessor{}))

	input := make([]any, 100)
	for i := range input {
		input[i] = i + 1
	}

	results := group.Process(input)

	for i, v := range results {
		expected := (i + 1) * (i + 1)
		if v.(int) != expected {
			t.Errorf("index %d: got %d, want %d", i, v.(int), expected)
		}
	}
}

func TestChainableGroup_PreservesOrder(t *testing.T) {
	group := pysyun.NewChainableGroup(2).
		Pipe(pysyun.NewChainable(slowSquareProcessor{}))

	input := []any{5, 3, 9, 1}
	results := group.Process(input)

	expected := []int{25, 9, 81, 1}
	for i, v := range results {
		if v.(int) != expected[i] {
			t.Errorf("order broken at %d: got %d, want %d", i, v.(int), expected[i])
		}
	}
}

func TestChainableGroup_Speedup(t *testing.T) {
	const n = 40

	input := make([]any, n)
	for i := range input {
		input[i] = i
	}

	// Sequential baseline
	seqStart := time.Now()
	for _, item := range input {
		slowSquareProcessor{}.Process(item)
	}
	seqDur := time.Since(seqStart)

	// Parallel with 8 goroutines
	group := pysyun.NewChainableGroup(8).
		Pipe(pysyun.NewChainable(slowSquareProcessor{}))

	parStart := time.Now()
	group.Process(input)
	parDur := time.Since(parStart)

	speedup := float64(seqDur) / float64(parDur)
	t.Logf("sequential=%v  parallel=%v  speedup=%.1fx", seqDur, parDur, speedup)

	if speedup < 2.0 {
		t.Errorf("expected meaningful speedup, got %.1fx", speedup)
	}
}

func TestChainableGroup_UnlimitedConcurrency(t *testing.T) {
	// concurrency <= 0 means one goroutine per item
	group := pysyun.NewChainableGroup(0).
		Pipe(pysyun.NewChainable(squareItemProcessor{}))

	input := []any{2, 3, 4}
	results := group.Process(input)

	expected := []int{4, 9, 16}
	for i, v := range results {
		if v.(int) != expected[i] {
			t.Errorf("index %d: got %d, want %d", i, v.(int), expected[i])
		}
	}
}

func TestChainableGroup_MultiStagePipeline(t *testing.T) {
	// Per-item pipeline: square → square  (x → x² → x⁴)
	group := pysyun.NewChainableGroup(4).
		Pipe(pysyun.NewChainable(squareItemProcessor{})).
		Pipe(pysyun.NewChainable(squareItemProcessor{}))

	input := []any{2, 3}
	results := group.Process(input)

	// 2⁴ = 16, 3⁴ = 81
	if results[0].(int) != 16 || results[1].(int) != 81 {
		t.Errorf("unexpected: %v", results)
	}
}

func TestChainableGroup_ConcurrencyLimit(t *testing.T) {
	// Verify that concurrency is actually limited to N goroutines.
	var peak atomic.Int64
	var current atomic.Int64

	tracker := pysyun.ProcessorFunc(func(data any) any {
		cur := current.Add(1)
		// Track peak
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		current.Add(-1)
		return data
	})

	const limit = 3
	group := pysyun.NewChainableGroup(limit).
		Pipe(pysyun.NewChainable(tracker))

	input := make([]any, 20)
	for i := range input {
		input[i] = i
	}
	group.Process(input)

	if peak.Load() > int64(limit) {
		t.Errorf("peak concurrency %d exceeded limit %d", peak.Load(), limit)
	}
}

// ---------------------------------------------------------------------------
// PipelineNode (graph-based, with profiling)
// ---------------------------------------------------------------------------

func TestPipelineNode_LinearChain(t *testing.T) {
	a := pysyun.NewPipelineNode(doubleProcessor{})
	b := pysyun.NewPipelineNode(addProcessor{n: 100})
	a.Add(b)

	a.Write([]any{5})
	a.Activate()

	result := b.Read().([]any)
	// 5 → double → 10 → add(100) → 110
	if result[0].(int) != 110 {
		t.Errorf("got %d, want 110", result[0].(int))
	}
}

func TestPipelineNode_FanOut(t *testing.T) {
	source := pysyun.NewPipelineNode(doubleProcessor{})
	branchA := pysyun.NewPipelineNode(addProcessor{n: 1})
	branchB := pysyun.NewPipelineNode(addProcessor{n: 1000})

	source.Add(branchA)
	source.Add(branchB)

	source.Write([]any{5})
	source.Activate()

	// Both branches receive doubled input [10]
	resA := branchA.Read().([]any)
	resB := branchB.Read().([]any)

	if resA[0].(int) != 11 {
		t.Errorf("branch A: got %d, want 11", resA[0].(int))
	}
	if resB[0].(int) != 1010 {
		t.Errorf("branch B: got %d, want 1010", resB[0].(int))
	}
}

func TestPipelineNode_Profiling(t *testing.T) {
	slow := pysyun.ProcessorFunc(func(data any) any {
		time.Sleep(50 * time.Millisecond)
		items := data.([]any)
		// Return more items than input to test delta
		return append(items, 999)
	})

	node := pysyun.NewPipelineNode(slow)
	node.Write([]any{1, 2, 3})
	node.Activate()

	if node.LastDelta.Duration < 50*time.Millisecond {
		t.Errorf("expected duration >= 50ms, got %v", node.LastDelta.Duration)
	}
	// Input 3 items, output 4 → delta = +1
	if node.LastDelta.ItemDelta != 1 {
		t.Errorf("expected item delta 1, got %d", node.LastDelta.ItemDelta)
	}
}

// ---------------------------------------------------------------------------
// Example (appears in go doc)
// ---------------------------------------------------------------------------

func Example() {
	// Synchronous pipeline: double → add(10)
	pipeline := pysyun.NewChainable(doubleProcessor{}).
		Pipe(pysyun.NewChainable(addProcessor{n: 10}))

	result := pipeline.Process([]any{1, 2, 3}).([]any)
	fmt.Println("sync:", result)

	// Parallel fan-out: square each item across 4 goroutines
	group := pysyun.NewChainableGroup(4).
		Pipe(pysyun.NewChainable(squareItemProcessor{}))

	items := []any{2, 3, 4, 5}
	parallel := group.Process(items)
	fmt.Println("parallel:", parallel)

	// Output:
	// sync: [12 14 16]
	// parallel: [4 9 16 25]
}
