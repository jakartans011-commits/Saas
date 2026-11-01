// core/scheduler/worker_pool.go
package scheduler

import (
	"context"
	"sync"

	"trailseal/sdk"
)

// WorkerPool adalah pool worker untuk mengeksekusi executable
type WorkerPool struct {
	mu        sync.RWMutex
	workers   int
	taskQueue chan *executionRequest
	stopCh    chan struct{}
	wg        sync.WaitGroup
	isRunning bool
	executor  *Executor
}

// executionRequest adalah request untuk mengeksekusi executable
type executionRequest struct {
	Executable sdk.Executable
	Context    sdk.ExecutableContext
}

// NewWorkerPool membuat worker pool baru
func NewWorkerPool(workers int, executor *Executor) *WorkerPool {
	return &WorkerPool{
		workers:   workers,
		taskQueue: make(chan *executionRequest, 1000),
		stopCh:    make(chan struct{}),
		executor:  executor,
		isRunning: false,
	}
}

// Start memulai worker pool
func (p *WorkerPool) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isRunning {
		return
	}

	p.isRunning = true
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

// Stop menghentikan worker pool (graceful)
func (p *WorkerPool) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isRunning {
		return
	}

	p.isRunning = false
	close(p.stopCh)
	p.wg.Wait()
}

// Submit menambahkan task ke queue
func (p *WorkerPool) Submit(executable sdk.Executable, ctx sdk.ExecutableContext) {
	select {
	case p.taskQueue <- &executionRequest{
		Executable: executable,
		Context:    ctx,
	}:
	default:
		// Queue penuh — reject task
		// Log akan ditangani oleh scheduler
	}
}

// worker adalah goroutine worker
func (p *WorkerPool) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.stopCh:
			return
		case req := <-p.taskQueue:
			p.executor.Execute(req.Executable, req.Context)
		case <-ctx.Done():
			return
		}
	}
}

// IsRunning mengembalikan status worker pool
func (p *WorkerPool) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isRunning
}

// QueueSize mengembalikan ukuran queue
func (p *WorkerPool) QueueSize() int {
	return len(p.taskQueue)
}
