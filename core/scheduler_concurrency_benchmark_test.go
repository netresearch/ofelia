package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// SimpleControlledJob is a lightweight job for benchmarking concurrency
type SimpleControlledJob struct {
	BareJob
	executionCount    int64
	executionDuration time.Duration
}

func NewSimpleControlledJob(name, schedule string, duration time.Duration) *SimpleControlledJob {
	job := &SimpleControlledJob{
		executionDuration: duration,
	}
	job.BareJob.Name = name
	job.BareJob.Schedule = schedule
	job.BareJob.Command = "benchmark-job"
	return job
}

func (j *SimpleControlledJob) Run(ctx *Context) error {
	atomic.AddInt64(&j.executionCount, 1)
	if j.executionDuration > 0 {
		time.Sleep(j.executionDuration)
	}
	return nil
}

func (j *SimpleControlledJob) GetExecutionCount() int64 {
	return atomic.LoadInt64(&j.executionCount)
}

// BenchmarkSchedulerConcurrency benchmarks scheduler concurrency with various job loads
func BenchmarkSchedulerConcurrency(b *testing.B) {
	testCases := []struct {
		maxConcurrent int
		numJobs       int
		duration      time.Duration
	}{
		{1, 10, time.Millisecond},
		{5, 25, time.Millisecond},
		{10, 50, time.Millisecond},
		{20, 100, time.Millisecond},
	}

	for _, tc := range testCases {
		b.Run(fmt.Sprintf("concurrent_%d_jobs_%d_duration_%v", tc.maxConcurrent, tc.numJobs, tc.duration), func(b *testing.B) {
			scheduler := NewScheduler(&TestLogger{})
			scheduler.SetMaxConcurrentJobs(tc.maxConcurrent)

			// Create jobs
			jobs := make([]*SimpleControlledJob, tc.numJobs)
			for i := 0; i < tc.numJobs; i++ {
				jobs[i] = NewSimpleControlledJob(fmt.Sprintf("bench-job-%d", i), "@daily", tc.duration)
				if err := scheduler.AddJob(jobs[i]); err != nil {
					b.Fatalf("Failed to add job %d: %v", i, err)
				}
			}

			if err := scheduler.Start(); err != nil {
				b.Fatalf("Failed to start scheduler: %v", err)
			}
			defer scheduler.Stop()

			b.ResetTimer()

			// Benchmark the scheduler's ability to handle concurrent job executions
			for n := 0; n < b.N; n++ {
				var wg sync.WaitGroup
				wg.Add(tc.numJobs)

				// Trigger all jobs concurrently
				for i := 0; i < tc.numJobs; i++ {
					go func(jobIndex int) {
						defer wg.Done()
						scheduler.RunJob(fmt.Sprintf("bench-job-%d", jobIndex))
					}(i)
				}

				wg.Wait()

				// Wait for all jobs to complete
				time.Sleep(tc.duration + 10*time.Millisecond)
			}

			b.StopTimer()

			// Report execution stats
			totalExecutions := int64(0)
			for _, job := range jobs {
				totalExecutions += job.GetExecutionCount()
			}
			b.ReportMetric(float64(totalExecutions)/float64(b.N), "executions/op")
		})
	}
}

// BenchmarkSchedulerMemoryUsage benchmarks memory usage under high concurrency
func BenchmarkSchedulerMemoryUsage(b *testing.B) {
	scheduler := NewScheduler(&TestLogger{})
	scheduler.SetMaxConcurrentJobs(50)

	// Create a reasonable number of jobs for memory testing
	const numJobs = 100
	for i := 0; i < numJobs; i++ {
		job := NewSimpleControlledJob(fmt.Sprintf("mem-job-%d", i), "@daily", time.Microsecond*100)
		if err := scheduler.AddJob(job); err != nil {
			b.Fatalf("Failed to add job %d: %v", i, err)
		}
	}

	if err := scheduler.Start(); err != nil {
		b.Fatalf("Failed to start scheduler: %v", err)
	}
	defer scheduler.Stop()

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		// Trigger rapid job executions to stress memory allocation
		for i := 0; i < numJobs; i++ {
			go scheduler.RunJob(fmt.Sprintf("mem-job-%d", i))
		}
		// Allow some jobs to complete before next iteration
		time.Sleep(time.Millisecond * 5)
	}
}

// BenchmarkSchedulerJobManagement benchmarks job add/remove/disable/enable operations
func BenchmarkSchedulerJobManagement(b *testing.B) {
	operations := []string{"add", "remove", "disable", "enable"}

	for _, op := range operations {
		b.Run(op, func(b *testing.B) {
			scheduler := NewScheduler(&TestLogger{})

			if err := scheduler.Start(); err != nil {
				b.Fatalf("Failed to start scheduler: %v", err)
			}
			defer scheduler.Stop()

			b.ResetTimer()

			switch op {
			case "add":
				for n := 0; n < b.N; n++ {
					job := NewSimpleControlledJob(fmt.Sprintf("add-job-%d", n), "@daily", 0)
					if err := scheduler.AddJob(job); err != nil {
						b.Fatalf("AddJob failed: %v", err)
					}
				}

			case "remove":
				// Pre-populate jobs for removal
				jobs := make([]*SimpleControlledJob, b.N)
				for i := 0; i < b.N; i++ {
					jobs[i] = NewSimpleControlledJob(fmt.Sprintf("remove-job-%d", i), "@daily", 0)
					scheduler.AddJob(jobs[i])
				}
				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					if err := scheduler.RemoveJob(jobs[n]); err != nil {
						b.Fatalf("RemoveJob failed: %v", err)
					}
				}

			case "disable":
				// Pre-populate jobs for disabling
				for i := 0; i < b.N; i++ {
					job := NewSimpleControlledJob(fmt.Sprintf("disable-job-%d", i), "@daily", 0)
					scheduler.AddJob(job)
				}
				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					if err := scheduler.DisableJob(fmt.Sprintf("disable-job-%d", n)); err != nil {
						b.Fatalf("DisableJob failed: %v", err)
					}
				}

			case "enable":
				// Pre-populate and disable jobs for enabling
				for i := 0; i < b.N; i++ {
					job := NewSimpleControlledJob(fmt.Sprintf("enable-job-%d", i), "@daily", 0)
					scheduler.AddJob(job)
					scheduler.DisableJob(fmt.Sprintf("enable-job-%d", i))
				}
				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					if err := scheduler.EnableJob(fmt.Sprintf("enable-job-%d", n)); err != nil {
						b.Fatalf("EnableJob failed: %v", err)
					}
				}
			}
		})
	}
}

// BenchmarkSchedulerSemaphoreContention benchmarks semaphore contention under high load
func BenchmarkSchedulerSemaphoreContention(b *testing.B) {
	semaphoreSizes := []int{1, 2, 5, 10, 20, 50}

	for _, size := range semaphoreSizes {
		b.Run(fmt.Sprintf("semaphore_%d", size), func(b *testing.B) {
			scheduler := NewScheduler(&TestLogger{})
			scheduler.SetMaxConcurrentJobs(size)

			// Create jobs that will compete for semaphore slots
			const numCompetingJobs = 100
			for i := 0; i < numCompetingJobs; i++ {
				job := NewSimpleControlledJob(fmt.Sprintf("compete-job-%d", i), "@daily", time.Millisecond*10)
				scheduler.AddJob(job)
			}

			if err := scheduler.Start(); err != nil {
				b.Fatalf("Failed to start scheduler: %v", err)
			}
			defer scheduler.Stop()

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				// Create high contention by triggering many jobs simultaneously
				var wg sync.WaitGroup
				wg.Add(numCompetingJobs)

				for i := 0; i < numCompetingJobs; i++ {
					go func(jobIndex int) {
						defer wg.Done()
						scheduler.RunJob(fmt.Sprintf("compete-job-%d", jobIndex))
					}(i)
				}

				wg.Wait()
				time.Sleep(time.Millisecond * 15) // Allow jobs to complete
			}
		})
	}
}

// BenchmarkSchedulerLookupOperations benchmarks job lookup performance
func BenchmarkSchedulerLookupOperations(b *testing.B) {
	scheduler := NewScheduler(&TestLogger{})

	// Create various numbers of jobs to test lookup performance
	jobCounts := []int{10, 100, 1000}

	for _, count := range jobCounts {
		b.Run(fmt.Sprintf("lookup_%d_jobs", count), func(b *testing.B) {
			// Populate jobs
			for i := 0; i < count; i++ {
				job := NewSimpleControlledJob(fmt.Sprintf("lookup-job-%d", i), "@daily", 0)
				scheduler.AddJob(job)
			}

			// Also create some disabled jobs
			for i := 0; i < count/4; i++ {
				job := NewSimpleControlledJob(fmt.Sprintf("disabled-job-%d", i), "@daily", 0)
				scheduler.AddJob(job)
				scheduler.DisableJob(fmt.Sprintf("disabled-job-%d", i))
			}

			if err := scheduler.Start(); err != nil {
				b.Fatalf("Failed to start scheduler: %v", err)
			}
			defer scheduler.Stop()

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				// Benchmark active job lookups
				jobIndex := n % count
				job := scheduler.GetJob(fmt.Sprintf("lookup-job-%d", jobIndex))
				if job == nil {
					b.Fatalf("Failed to find job %d", jobIndex)
				}

				// Benchmark disabled job lookups
				if jobIndex < count/4 {
					disabledJob := scheduler.GetDisabledJob(fmt.Sprintf("disabled-job-%d", jobIndex))
					if disabledJob == nil {
						b.Fatalf("Failed to find disabled job %d", jobIndex)
					}
				}
			}
		})
	}
}
