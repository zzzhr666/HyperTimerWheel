package timerwheel

import "sync"

type WorkerPool struct {
	tasks chan func()
	wg    sync.WaitGroup
}

type WorkerPoolConfig struct {
	WorkerNum int
	QueueSize int
}

func NewWorkerPool(config WorkerPoolConfig) *WorkerPool {
	if config.WorkerNum <= 0 || config.QueueSize <= 0 {
		return nil
	}

	p := &WorkerPool{
		tasks: make(chan func(), config.QueueSize),
	}

	for i := 0; i < config.WorkerNum; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	return p
}

func (p *WorkerPool) worker() {
	defer p.wg.Done()

	for task := range p.tasks {
		task()
	}
}

func (p *WorkerPool) Submit(task func()) bool {
	if task == nil {
		return false
	}

	select {
	case p.tasks <- task:
		return true
	default:
		return false
	}
}

func (p *WorkerPool) Close() {
	close(p.tasks)
	p.wg.Wait()
}
