package task

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTaskConcurrentJobCompletionDoesNotRace(t *testing.T) {
	const jobCount = 128

	release := make(chan struct{})
	started := make(chan struct{}, jobCount)
	task := New(
		&Handlers{
			Exec: func(id int, args ...interface{}) (interface{}, error) {
				started <- struct{}{}
				<-release
				return id, nil
			},
		},
		Option{
			Name:           "unit-test-concurrent-completion",
			WorkerNums:     jobCount,
			JobQueueLength: jobCount * 2,
			Timeout:        5,
		},
	)

	jobs := make([]*Job, 0, jobCount)
	for i := 0; i < jobCount; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		job := &Job{
			id:     i + 1,
			ctx:    ctx,
			cancel: cancel,
		}
		task.jobs[job.id] = job
		jobs = append(jobs, job)
	}

	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(job *Job) {
			defer wg.Done()
			task.start(job)
		}(job)
	}

	for i := 0; i < jobCount; i++ {
		<-started
	}
	close(release)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for jobs to complete")
	}

	if len(task.jobs) != 0 {
		t.Fatalf("expected all jobs to be removed, got %d", len(task.jobs))
	}
}
