package worker

import "context"

type Job func(context.Context)

type Pool struct {
	jobs chan Job
}

func New(size int) *Pool {
	if size <= 0 {
		size = 1
	}
	return &Pool{jobs: make(chan Job, size*8)}
}

func (p *Pool) Start(ctx context.Context, size int) {
	if size <= 0 {
		size = 1
	}
	for i := 0; i < size; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-p.jobs:
					job(ctx)
				}
			}
		}()
	}
}

func (p *Pool) Submit(ctx context.Context, job Job) bool {
	select {
	case <-ctx.Done():
		return false
	case p.jobs <- job:
		return true
	default:
		return false
	}
}
