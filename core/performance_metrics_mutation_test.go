package core

import (
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY at line 174:36
// Original: len(pm.containerWaitDurations) > 1000
// Mutant:   len(pm.containerWaitDurations) >= 1000
// =============================================================================

func TestContainerWaitDuration_BoundaryAt1000(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Fill exactly 1000 durations
	for i := range 1000 {
		pm.RecordContainerWaitDuration(float64(i))
	}

	// With > 1000, we should keep all 1000 (not truncated yet)
	// With >= 1000 (mutant), it would truncate to 1000 from the end
	pm.containerMutex.RLock()
	count := len(pm.containerWaitDurations)
	first := pm.containerWaitDurations[0]
	pm.containerMutex.RUnlock()

	if count != 1000 {
		t.Errorf("Expected 1000 durations, got %d", count)
	}
	// The first element should still be 0.0 (no truncation occurred)
	if first != 0.0 {
		t.Errorf("Expected first duration to be 0.0 (no truncation at exactly 1000), got %f", first)
	}

	// Now add one more to trigger truncation (1001 > 1000 is true)
	pm.RecordContainerWaitDuration(9999.0)

	pm.containerMutex.RLock()
	count = len(pm.containerWaitDurations)
	firstAfter := pm.containerWaitDurations[0]
	lastAfter := pm.containerWaitDurations[count-1]
	pm.containerMutex.RUnlock()

	if count != 1000 {
		t.Errorf("After adding 1001st, expected 1000 durations, got %d", count)
	}
	// After truncation, first element should be 1.0 (element at index 1 of original)
	if firstAfter != 1.0 {
		t.Errorf("Expected first duration after truncation to be 1.0, got %f", firstAfter)
	}
	if lastAfter != 9999.0 {
		t.Errorf("Expected last duration after truncation to be 9999.0, got %f", lastAfter)
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY + CONDITIONALS_NEGATION at lines 214, 217
// Line 214: if duration < tracker.Min || tracker.Min == 0
// Line 217: if duration > tracker.Max
// =============================================================================

func TestDockerLatency_MinMaxBoundary(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// First record sets both Min and Max
	pm.RecordDockerLatency("op", 50*time.Millisecond)

	pm.dockerMutex.RLock()
	tracker := pm.dockerLatencies["op"]
	pm.dockerMutex.RUnlock()

	tracker.mutex.RLock()
	min1 := tracker.Min
	max1 := tracker.Max
	tracker.mutex.RUnlock()

	if min1 != 50*time.Millisecond {
		t.Errorf("Expected Min=50ms, got %v", min1)
	}
	if max1 != 50*time.Millisecond {
		t.Errorf("Expected Max=50ms, got %v", max1)
	}

	// Record duration equal to current Min -- should NOT update Min
	// (< is strict, equal does not trigger)
	pm.RecordDockerLatency("op", 50*time.Millisecond)

	tracker.mutex.RLock()
	min2 := tracker.Min
	max2 := tracker.Max
	tracker.mutex.RUnlock()

	if min2 != 50*time.Millisecond {
		t.Errorf("Min should stay 50ms when recording equal value, got %v", min2)
	}
	if max2 != 50*time.Millisecond {
		t.Errorf("Max should stay 50ms when recording equal value, got %v", max2)
	}

	// Record duration strictly less than Min -- should update Min
	pm.RecordDockerLatency("op", 30*time.Millisecond)

	tracker.mutex.RLock()
	min3 := tracker.Min
	tracker.mutex.RUnlock()

	if min3 != 30*time.Millisecond {
		t.Errorf("Min should be 30ms after recording lower value, got %v", min3)
	}

	// Record duration strictly greater than Max -- should update Max
	pm.RecordDockerLatency("op", 80*time.Millisecond)

	tracker.mutex.RLock()
	max4 := tracker.Max
	tracker.mutex.RUnlock()

	if max4 != 80*time.Millisecond {
		t.Errorf("Max should be 80ms after recording higher value, got %v", max4)
	}
}

func TestDockerLatency_MinEqualToZero(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// When a tracker is first created, Min is set to the first duration.
	// The second condition (tracker.Min == 0) handles the case where
	// Min was initialized to zero (should not happen normally but is a guard).
	// Record with a non-zero duration, then verify Min was set correctly.
	pm.RecordDockerLatency("op2", 100*time.Millisecond)

	pm.dockerMutex.RLock()
	tracker := pm.dockerLatencies["op2"]
	pm.dockerMutex.RUnlock()

	tracker.mutex.RLock()
	min := tracker.Min
	tracker.mutex.RUnlock()

	if min != 100*time.Millisecond {
		t.Errorf("Expected Min=100ms on first record, got %v", min)
	}

	// Now record a higher duration -- Min should NOT change
	pm.RecordDockerLatency("op2", 200*time.Millisecond)

	tracker.mutex.RLock()
	min2 := tracker.Min
	max2 := tracker.Max
	tracker.mutex.RUnlock()

	if min2 != 100*time.Millisecond {
		t.Errorf("Min should remain 100ms, got %v", min2)
	}
	if max2 != 200*time.Millisecond {
		t.Errorf("Max should be 200ms, got %v", max2)
	}
}

// Test negation: if duration < tracker.Min is negated to duration >= tracker.Min,
// then equal values would update Min (which they shouldn't).
func TestDockerLatency_NegationMinCondition(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	pm.RecordDockerLatency("neg", 50*time.Millisecond)
	// Record a HIGHER value; should NOT change Min
	pm.RecordDockerLatency("neg", 70*time.Millisecond)

	pm.dockerMutex.RLock()
	tracker := pm.dockerLatencies["neg"]
	pm.dockerMutex.RUnlock()

	tracker.mutex.RLock()
	min := tracker.Min
	tracker.mutex.RUnlock()

	if min != 50*time.Millisecond {
		t.Errorf("Min should be 50ms (not updated by higher value), got %v", min)
	}
}

// Test negation: if duration > tracker.Max is negated to duration <= tracker.Max,
// then a higher value would NOT update Max (but it should).
func TestDockerLatency_NegationMaxCondition(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	pm.RecordDockerLatency("neg2", 50*time.Millisecond)
	pm.RecordDockerLatency("neg2", 100*time.Millisecond)

	pm.dockerMutex.RLock()
	tracker := pm.dockerLatencies["neg2"]
	pm.dockerMutex.RUnlock()

	tracker.mutex.RLock()
	max := tracker.Max
	tracker.mutex.RUnlock()

	if max != 100*time.Millisecond {
		t.Errorf("Max should be 100ms after recording higher value, got %v", max)
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY + CONDITIONALS_NEGATION at lines 249, 252
// Line 249: if duration < metrics.MinDuration || metrics.MinDuration == 0
// Line 252: if duration > metrics.MaxDuration
// Also: INCREMENT_DECREMENT at lines 257, 260
// Line 257: metrics.SuccessCount++ (mutant: --)
// Line 260: metrics.FailureCount++ (mutant: --)
// =============================================================================

func TestJobExecution_MinMaxDurationBoundary(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// First execution sets both Min and Max
	pm.RecordJobExecution("job_bound", 100*time.Millisecond, true)

	pm.jobMutex.RLock()
	metrics := pm.jobExecutions["job_bound"]
	min1 := metrics.MinDuration
	max1 := metrics.MaxDuration
	pm.jobMutex.RUnlock()

	if min1 != 100*time.Millisecond {
		t.Errorf("Expected MinDuration=100ms, got %v", min1)
	}
	if max1 != 100*time.Millisecond {
		t.Errorf("Expected MaxDuration=100ms, got %v", max1)
	}

	// Record duration equal to current Min -- should NOT update Min (< is strict)
	pm.RecordJobExecution("job_bound", 100*time.Millisecond, true)

	pm.jobMutex.RLock()
	min2 := metrics.MinDuration
	max2 := metrics.MaxDuration
	pm.jobMutex.RUnlock()

	if min2 != 100*time.Millisecond {
		t.Errorf("Min should stay 100ms when recording equal value, got %v", min2)
	}
	if max2 != 100*time.Millisecond {
		t.Errorf("Max should stay 100ms when recording equal value, got %v", max2)
	}

	// Record strictly lower -- should update Min
	pm.RecordJobExecution("job_bound", 50*time.Millisecond, true)

	pm.jobMutex.RLock()
	min3 := metrics.MinDuration
	pm.jobMutex.RUnlock()

	if min3 != 50*time.Millisecond {
		t.Errorf("Min should be 50ms after recording lower value, got %v", min3)
	}

	// Record strictly higher -- should update Max
	pm.RecordJobExecution("job_bound", 200*time.Millisecond, true)

	pm.jobMutex.RLock()
	max4 := metrics.MaxDuration
	pm.jobMutex.RUnlock()

	if max4 != 200*time.Millisecond {
		t.Errorf("Max should be 200ms after recording higher value, got %v", max4)
	}
}

func TestJobExecution_MinDurationNegation(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	pm.RecordJobExecution("neg_job", 100*time.Millisecond, true)
	// Higher duration should NOT update Min
	pm.RecordJobExecution("neg_job", 200*time.Millisecond, true)

	pm.jobMutex.RLock()
	min := pm.jobExecutions["neg_job"].MinDuration
	pm.jobMutex.RUnlock()

	if min != 100*time.Millisecond {
		t.Errorf("Min should remain 100ms, got %v", min)
	}
}

func TestJobExecution_MaxDurationNegation(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	pm.RecordJobExecution("neg_job2", 200*time.Millisecond, true)
	// Lower should NOT update Max
	pm.RecordJobExecution("neg_job2", 100*time.Millisecond, true)

	pm.jobMutex.RLock()
	max := pm.jobExecutions["neg_job2"].MaxDuration
	pm.jobMutex.RUnlock()

	if max != 200*time.Millisecond {
		t.Errorf("Max should remain 200ms, got %v", max)
	}
}

func TestJobExecution_MinDurationEqualToZero(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// The condition `metrics.MinDuration == 0` guards against zero-initialized Min.
	// On first call, Min is set to the duration in the constructor.
	// Verify that after first call, Min is set (not zero).
	pm.RecordJobExecution("zero_job", 300*time.Millisecond, true)

	pm.jobMutex.RLock()
	min := pm.jobExecutions["zero_job"].MinDuration
	pm.jobMutex.RUnlock()

	if min != 300*time.Millisecond {
		t.Errorf("Expected MinDuration=300ms on first record, got %v", min)
	}
}

// =============================================================================
// Tests targeting INCREMENT_DECREMENT at lines 257 and 260
// Line 257: metrics.SuccessCount++ (mutant: metrics.SuccessCount--)
// Line 260: metrics.FailureCount++ (mutant: metrics.FailureCount--)
// =============================================================================

func TestJobExecution_SuccessCountIncrement(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record 3 successful executions
	pm.RecordJobExecution("succ_job", 10*time.Millisecond, true)
	pm.RecordJobExecution("succ_job", 10*time.Millisecond, true)
	pm.RecordJobExecution("succ_job", 10*time.Millisecond, true)

	pm.jobMutex.RLock()
	successCount := pm.jobExecutions["succ_job"].SuccessCount
	pm.jobMutex.RUnlock()

	// With ++: 3. With -- (mutant): -3
	if successCount != 3 {
		t.Errorf("Expected SuccessCount=3, got %d", successCount)
	}
}

func TestJobExecution_FailureCountIncrement(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record 2 failed executions
	pm.RecordJobExecution("fail_job", 10*time.Millisecond, false)
	pm.RecordJobExecution("fail_job", 10*time.Millisecond, false)

	pm.jobMutex.RLock()
	failureCount := pm.jobExecutions["fail_job"].FailureCount
	pm.jobMutex.RUnlock()

	// With ++: 2. With -- (mutant): -2
	if failureCount != 2 {
		t.Errorf("Expected FailureCount=2, got %d", failureCount)
	}
}

// Verify that a single success gives SuccessCount == 1 (catches ++ -> -- immediately)
func TestJobExecution_SingleSuccessIncrement(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	pm.RecordJobExecution("single_succ", 10*time.Millisecond, true)

	pm.jobMutex.RLock()
	sc := pm.jobExecutions["single_succ"].SuccessCount
	pm.jobMutex.RUnlock()

	// With ++: 1. With -- (mutant): -1
	if sc != 1 {
		t.Errorf("Expected SuccessCount=1 after one success, got %d", sc)
	}
}

// Verify that a single failure gives FailureCount == 1
func TestJobExecution_SingleFailureIncrement(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	pm.RecordJobExecution("single_fail", 10*time.Millisecond, false)

	pm.jobMutex.RLock()
	fc := pm.jobExecutions["single_fail"].FailureCount
	pm.jobMutex.RUnlock()

	// With ++: 1. With -- (mutant): -1
	if fc != 1 {
		t.Errorf("Expected FailureCount=1 after one failure, got %d", fc)
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_NEGATION at line 293:17
// Line 293: if skipReasons == nil
// Mutant:   if skipReasons != nil
// =============================================================================

func TestJobSkipped_SkipReasonsInitialization(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// First skip should initialize the map (skipReasons == nil is true on first call)
	pm.RecordJobSkipped("job1", "disabled")

	pm.customMutex.RLock()
	skipReasons := pm.customMetrics["job_skip_reasons"]
	pm.customMutex.RUnlock()

	if skipReasons == nil {
		t.Fatal("Expected job_skip_reasons to be initialized, got nil")
	}

	reasonMap, ok := skipReasons.(map[string]int64)
	if !ok {
		t.Fatal("job_skip_reasons is not map[string]int64")
	}

	if reasonMap["disabled"] != 1 {
		t.Errorf("Expected disabled reason count=1, got %d", reasonMap["disabled"])
	}

	// Second skip with same reason should increment (skipReasons != nil, so map already exists)
	pm.RecordJobSkipped("job2", "disabled")

	pm.customMutex.RLock()
	skipReasons2 := pm.customMetrics["job_skip_reasons"]
	pm.customMutex.RUnlock()

	reasonMap2, ok := skipReasons2.(map[string]int64)
	if !ok {
		t.Fatal("job_skip_reasons is not map[string]int64 after second call")
	}

	if reasonMap2["disabled"] != 2 {
		t.Errorf("Expected disabled reason count=2, got %d", reasonMap2["disabled"])
	}
}

// =============================================================================
// Tests targeting INCREMENT_DECREMENT at line 298:20
// Line 298: reasonMap[reason]++ (mutant: reasonMap[reason]--)
// =============================================================================

func TestJobSkipped_ReasonCountIncrement(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	pm.RecordJobSkipped("j1", "overlap")
	pm.RecordJobSkipped("j2", "overlap")
	pm.RecordJobSkipped("j3", "overlap")

	pm.customMutex.RLock()
	skipReasons := pm.customMetrics["job_skip_reasons"]
	pm.customMutex.RUnlock()

	reasonMap, ok := skipReasons.(map[string]int64)
	if !ok {
		t.Fatal("job_skip_reasons is not map[string]int64")
	}

	// With ++: 3. With -- (mutant): -1 (first call sets it to 1 via init + 0++, then -- twice)
	// Actually: zero value 0, first ++ = 1, second ++ = 2, third ++ = 3
	// Mutant: 0-- = -1, -1-- = -2, -2-- = -3
	if reasonMap["overlap"] != 3 {
		t.Errorf("Expected overlap count=3, got %d", reasonMap["overlap"])
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY at lines 310:12 and 326:12
// Line 310: if count <= peak  (RecordConcurrentJobs)
// Line 326: if bytes <= peak  (RecordMemoryUsage)
// Mutant:   count < peak / bytes < peak
// =============================================================================

func TestRecordConcurrentJobs_BoundaryEqual(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Set peak to 5
	pm.RecordConcurrentJobs(5)
	peak1 := atomic.LoadInt64(&pm.maxConcurrentJobs)
	if peak1 != 5 {
		t.Errorf("Expected peak=5, got %d", peak1)
	}

	// Record same value (count == peak). With <=, we break (no update needed).
	// With < (mutant), count == peak would NOT break, and we'd try CAS which
	// would succeed but set peak to the same value (no behavioral difference for equal).
	// Actually, the key test: record count=5 again, then record count=4.
	// Peak should remain 5.
	pm.RecordConcurrentJobs(5)
	peak2 := atomic.LoadInt64(&pm.maxConcurrentJobs)
	if peak2 != 5 {
		t.Errorf("Expected peak to remain 5, got %d", peak2)
	}

	// Record lower value
	pm.RecordConcurrentJobs(3)
	peak3 := atomic.LoadInt64(&pm.maxConcurrentJobs)
	if peak3 != 5 {
		t.Errorf("Expected peak to remain 5 after lower value, got %d", peak3)
	}

	// Record higher value
	pm.RecordConcurrentJobs(10)
	peak4 := atomic.LoadInt64(&pm.maxConcurrentJobs)
	if peak4 != 10 {
		t.Errorf("Expected peak=10 after higher value, got %d", peak4)
	}
}

func TestRecordMemoryUsage_BoundaryEqual(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Set peak to 1MB
	pm.RecordMemoryUsage(1024 * 1024)
	peak1 := atomic.LoadInt64(&pm.peakMemoryUsage)
	if peak1 != 1024*1024 {
		t.Errorf("Expected peak=1MB, got %d", peak1)
	}

	// Record same value -- peak should not change
	pm.RecordMemoryUsage(1024 * 1024)
	peak2 := atomic.LoadInt64(&pm.peakMemoryUsage)
	if peak2 != 1024*1024 {
		t.Errorf("Expected peak to remain 1MB, got %d", peak2)
	}

	// Record lower value -- peak should not change
	pm.RecordMemoryUsage(512 * 1024)
	peak3 := atomic.LoadInt64(&pm.peakMemoryUsage)
	if peak3 != 1024*1024 {
		t.Errorf("Expected peak to remain 1MB after lower value, got %d", peak3)
	}

	// Record higher value -- peak should update
	pm.RecordMemoryUsage(2 * 1024 * 1024)
	peak4 := atomic.LoadInt64(&pm.peakMemoryUsage)
	if peak4 != 2*1024*1024 {
		t.Errorf("Expected peak=2MB after higher value, got %d", peak4)
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY at line 394:14
// Line 394: if totalOps > 0
// Mutant:   if totalOps >= 0 (always true since totalOps is a count)
// =============================================================================

func TestGetDockerMetrics_ErrorRateBoundary(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// No operations recorded. totalOps = 0.
	// With > 0: false, errorRate stays 0.0
	// With >= 0 (mutant): true, division by zero (0/0) or errorRate = NaN
	metrics := pm.GetDockerMetrics()
	errorRate, ok := metrics["error_rate_percent"].(float64)
	if !ok {
		t.Fatal("error_rate_percent not found")
	}
	if errorRate != 0.0 {
		t.Errorf("Expected error_rate=0.0 when no operations, got %f", errorRate)
	}

	// With exactly 1 operation, totalOps = 1 > 0 is true
	pm.RecordDockerOperation("test")
	metrics2 := pm.GetDockerMetrics()
	errorRate2, ok := metrics2["error_rate_percent"].(float64)
	if !ok {
		t.Fatal("error_rate_percent not found")
	}
	if errorRate2 != 0.0 {
		t.Errorf("Expected error_rate=0.0 with 1 op and 0 errors, got %f", errorRate2)
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY at line 419:19
// Line 419: if totalExecuted > 0
// Mutant:   if totalExecuted >= 0
// =============================================================================

func TestGetJobMetrics_SuccessRateBoundary(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// No jobs executed. totalExecuted = 0.
	// With > 0: false, successRate stays 0.0
	// With >= 0 (mutant): true, division by zero
	metrics := pm.GetJobMetrics()
	successRate, ok := metrics["success_rate_percent"].(float64)
	if !ok {
		t.Fatal("success_rate_percent not found")
	}
	if successRate != 0.0 {
		t.Errorf("Expected success_rate=0.0 when no jobs executed, got %f", successRate)
	}

	// With 1 successful execution
	pm.RecordJobExecution("test", 10*time.Millisecond, true)
	metrics2 := pm.GetJobMetrics()
	successRate2, ok := metrics2["success_rate_percent"].(float64)
	if !ok {
		t.Fatal("success_rate_percent not found")
	}
	if successRate2 != 100.0 {
		t.Errorf("Expected success_rate=100.0 with 1 successful job, got %f", successRate2)
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY + CONDITIONALS_NEGATION at line 426:29
// and ARITHMETIC_BASE at line 427:51 and 427:85
// Line 426: if metrics.ExecutionCount > 0
// Line 427: jobSuccessRate = float64(metrics.SuccessCount) / float64(metrics.ExecutionCount) * 100
// =============================================================================

func TestGetJobMetrics_PerJobSuccessRateBoundary(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record a job with no executions is impossible through normal API since
	// RecordJobExecution always increments ExecutionCount.
	// But we can test the boundary by having exactly 1 execution.

	// 1 success, 0 failures -> success rate should be 100%
	pm.RecordJobExecution("rate_job", 10*time.Millisecond, true)

	metrics := pm.GetJobMetrics()
	jobDetails := metrics["job_details"].(map[string]any)
	job := jobDetails["rate_job"].(map[string]any)

	successRate := job["success_rate"].(float64)
	if successRate != 100.0 {
		t.Errorf("Expected per-job success_rate=100.0, got %f", successRate)
	}
}

func TestGetJobMetrics_PerJobSuccessRateArithmetic(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// 3 successes, 1 failure -> ExecutionCount=4, SuccessCount=3
	// Correct: 3/4 * 100 = 75.0
	// ARITHMETIC_BASE at 427:51 (/ -> *): 3*4*100 = 1200
	// ARITHMETIC_BASE at 427:85 (* -> /): 3/4/100 = 0.0075
	pm.RecordJobExecution("arith_job", 10*time.Millisecond, true)
	pm.RecordJobExecution("arith_job", 20*time.Millisecond, true)
	pm.RecordJobExecution("arith_job", 30*time.Millisecond, true)
	pm.RecordJobExecution("arith_job", 40*time.Millisecond, false)

	metrics := pm.GetJobMetrics()
	jobDetails := metrics["job_details"].(map[string]any)
	job := jobDetails["arith_job"].(map[string]any)

	successRate := job["success_rate"].(float64)
	if successRate != 75.0 {
		t.Errorf("Expected per-job success_rate=75.0, got %f", successRate)
	}
}

// Test negation of metrics.ExecutionCount > 0 to metrics.ExecutionCount <= 0
func TestGetJobMetrics_PerJobSuccessRateNegation(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// With ExecutionCount=2, SuccessCount=1
	// Original: 2 > 0 is true, rate = 1/2*100 = 50.0
	// Negated: 2 <= 0 is false, rate stays 0.0
	pm.RecordJobExecution("neg_rate", 10*time.Millisecond, true)
	pm.RecordJobExecution("neg_rate", 10*time.Millisecond, false)

	metrics := pm.GetJobMetrics()
	jobDetails := metrics["job_details"].(map[string]any)
	job := jobDetails["neg_rate"].(map[string]any)

	successRate := job["success_rate"].(float64)
	if successRate != 50.0 {
		t.Errorf("Expected per-job success_rate=50.0, got %f", successRate)
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY + CONDITIONALS_NEGATION at line 487:28
// and ARITHMETIC_BASE at line 488:53
// Line 487: if metrics.TotalAttempts > 0
// Line 488: successRate = float64(metrics.SuccessfulRetries) / float64(metrics.TotalAttempts) * 100
// =============================================================================

func TestGetRetryMetrics_SuccessRateBoundary(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// No retries -> getRetryMetrics returns empty map, no division
	retryMetrics := pm.getRetryMetrics()
	if len(retryMetrics) != 0 {
		t.Errorf("Expected empty retry metrics, got %v", retryMetrics)
	}

	// 1 attempt, 1 success -> rate = 100%
	pm.RecordJobRetry("retry_job", 1, true)

	retryMetrics = pm.getRetryMetrics()
	job := retryMetrics["retry_job"].(map[string]any)
	rate := job["success_rate"].(float64)
	if rate != 100.0 {
		t.Errorf("Expected retry success_rate=100.0, got %f", rate)
	}
}

func TestGetRetryMetrics_SuccessRateArithmetic(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// 2 attempts: 1 success, 1 failure -> rate = 1/2 * 100 = 50.0
	// Arithmetic mutant (/ -> *): 1*2*100 = 200
	// Arithmetic mutant (* -> /): 1/2/100 = 0.005
	pm.RecordJobRetry("arith_retry", 1, true)
	pm.RecordJobRetry("arith_retry", 2, false)

	retryMetrics := pm.getRetryMetrics()
	job := retryMetrics["arith_retry"].(map[string]any)
	rate := job["success_rate"].(float64)
	if rate != 50.0 {
		t.Errorf("Expected retry success_rate=50.0, got %f", rate)
	}
}

func TestGetRetryMetrics_SuccessRateNegation(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// TotalAttempts=3, SuccessfulRetries=2
	// Original: 3 > 0 is true, rate = 2/3*100 ≈ 66.67
	// Negated: 3 <= 0 is false, rate stays 0.0
	pm.RecordJobRetry("neg_retry", 1, true)
	pm.RecordJobRetry("neg_retry", 2, true)
	pm.RecordJobRetry("neg_retry", 3, false)

	retryMetrics := pm.getRetryMetrics()
	job := retryMetrics["neg_retry"].(map[string]any)
	rate := job["success_rate"].(float64)

	expected := float64(2) / float64(3) * 100
	if rate < expected-0.01 || rate > expected+0.01 {
		t.Errorf("Expected retry success_rate≈%.2f, got %f", expected, rate)
	}
}

// =============================================================================
// Tests targeting CONDITIONALS_BOUNDARY + CONDITIONALS_NEGATION at line 511:20
// Line 511: if len(durations) > 0
// Mutant boundary: if len(durations) >= 0 (always true)
// Mutant negation: if len(durations) <= 0 (skip calculation when there are durations)
// =============================================================================

func TestGetContainerMetrics_AvgWaitDurationBoundary(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// No durations recorded. len(durations) = 0.
	// With > 0: false, avgWaitDuration stays 0.0
	// With >= 0 (mutant): true, division by zero (sum=0 / len=0)
	metrics := pm.getContainerMetrics()
	avg, ok := metrics["avg_wait_duration"].(float64)
	if !ok {
		t.Fatal("avg_wait_duration not found")
	}
	if avg != 0.0 {
		t.Errorf("Expected avg_wait_duration=0.0 with no durations, got %f", avg)
	}
}

func TestGetContainerMetrics_AvgWaitDurationNegation(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Record some durations
	pm.RecordContainerWaitDuration(10.0)
	pm.RecordContainerWaitDuration(20.0)
	pm.RecordContainerWaitDuration(30.0)

	// With len > 0 (true): avg = (10+20+30)/3 = 20.0
	// With negation (len <= 0, false): avg stays 0.0
	metrics := pm.getContainerMetrics()
	avg, ok := metrics["avg_wait_duration"].(float64)
	if !ok {
		t.Fatal("avg_wait_duration not found")
	}
	if avg != 20.0 {
		t.Errorf("Expected avg_wait_duration=20.0, got %f", avg)
	}
}

func TestGetContainerMetrics_AvgWaitDurationSingleValue(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Single duration: boundary test for exactly 1 element
	pm.RecordContainerWaitDuration(42.0)

	metrics := pm.getContainerMetrics()
	avg, ok := metrics["avg_wait_duration"].(float64)
	if !ok {
		t.Fatal("avg_wait_duration not found")
	}
	if avg != 42.0 {
		t.Errorf("Expected avg_wait_duration=42.0, got %f", avg)
	}
}

// =============================================================================
// Additional tests to ensure all arithmetic mutations at line 427 and 488 are killed
// =============================================================================

func TestGetJobMetrics_OverallSuccessRateArithmetic(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// 4 executed, 1 failed -> successRate = (4-1)/4 * 100 = 75.0
	// If - becomes + at the global level: (4+1)/4 * 100 = 125.0
	pm.RecordJobExecution("a1", 10*time.Millisecond, true)
	pm.RecordJobExecution("a2", 10*time.Millisecond, true)
	pm.RecordJobExecution("a3", 10*time.Millisecond, true)
	pm.RecordJobExecution("a4", 10*time.Millisecond, false)

	metrics := pm.GetJobMetrics()
	successRate := metrics["success_rate_percent"].(float64)
	if successRate != 75.0 {
		t.Errorf("Expected overall success_rate=75.0, got %f", successRate)
	}
}

// =============================================================================
// Test to ensure RecordConcurrentJobs correctly tracks peak with exact equal value
// Targets: CONDITIONALS_BOUNDARY at line 310
// count <= peak (original) vs count < peak (mutant)
// When count == peak, original breaks out. Mutant does NOT break, tries CAS
// which sets peak to same value. The difference is subtle - test increments.
// =============================================================================

func TestRecordConcurrentJobs_EqualToPeak(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Set peak to exactly 1
	pm.RecordConcurrentJobs(1)
	peak := atomic.LoadInt64(&pm.maxConcurrentJobs)
	if peak != 1 {
		t.Fatalf("Expected peak=1, got %d", peak)
	}

	// Record 1 again (equal to peak). Both original and mutant yield same peak.
	pm.RecordConcurrentJobs(1)
	peak = atomic.LoadInt64(&pm.maxConcurrentJobs)
	if peak != 1 {
		t.Errorf("Expected peak still 1 after recording equal, got %d", peak)
	}

	// Record 0 (less than peak). Should NOT change peak.
	pm.RecordConcurrentJobs(0)
	peak = atomic.LoadInt64(&pm.maxConcurrentJobs)
	if peak != 1 {
		t.Errorf("Expected peak still 1 after recording 0, got %d", peak)
	}
}

// =============================================================================
// Test to ensure RecordMemoryUsage correctly tracks peak with exact equal value
// Targets: CONDITIONALS_BOUNDARY at line 326
// =============================================================================

func TestRecordMemoryUsage_EqualToPeak(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	pm.RecordMemoryUsage(100)
	peak := atomic.LoadInt64(&pm.peakMemoryUsage)
	if peak != 100 {
		t.Fatalf("Expected peak=100, got %d", peak)
	}

	pm.RecordMemoryUsage(100)
	peak = atomic.LoadInt64(&pm.peakMemoryUsage)
	if peak != 100 {
		t.Errorf("Expected peak still 100 after recording equal, got %d", peak)
	}

	pm.RecordMemoryUsage(50)
	peak = atomic.LoadInt64(&pm.peakMemoryUsage)
	if peak != 100 {
		t.Errorf("Expected peak still 100 after recording lower, got %d", peak)
	}
}

// =============================================================================
// Additional edge case: Docker latency tracker Min==0 guard (line 214:43)
// Covers CONDITIONALS_NEGATION at 214:43 (tracker.Min == 0 -> tracker.Min != 0)
// =============================================================================

func TestDockerLatency_MinZeroGuard(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Manually create a tracker with Min=0 to test the zero guard
	pm.dockerMutex.Lock()
	tracker := &LatencyTracker{
		Min: 0,
		Max: 100 * time.Millisecond,
	}
	pm.dockerLatencies["zero_min"] = tracker
	pm.dockerMutex.Unlock()

	// Record a duration. Since tracker.Min == 0, the condition is true
	// regardless of whether duration < tracker.Min.
	// With == 0 (original): true, Min gets set to 50ms
	// With != 0 (mutant): false (since Min is 0), Min stays 0
	pm.RecordDockerLatency("zero_min", 50*time.Millisecond)

	tracker.mutex.RLock()
	min := tracker.Min
	tracker.mutex.RUnlock()

	if min != 50*time.Millisecond {
		t.Errorf("Expected Min=50ms when previous Min was 0 (zero guard), got %v", min)
	}
}

// =============================================================================
// Additional edge case: Job MinDuration==0 guard (line 249:59)
// Covers CONDITIONALS_NEGATION at 249:59 (metrics.MinDuration == 0 -> != 0)
// =============================================================================

func TestJobExecution_MinDurationZeroGuard(t *testing.T) {
	t.Parallel()
	pm := NewPerformanceMetrics()

	// Manually create a job metrics entry with MinDuration=0
	pm.jobMutex.Lock()
	pm.jobExecutions["zero_min_job"] = &JobMetrics{
		MinDuration:    0,
		MaxDuration:    100 * time.Millisecond,
		ExecutionCount: 1,
		TotalDuration:  100 * time.Millisecond,
	}
	pm.jobMutex.Unlock()

	// Record execution. Since MinDuration == 0, the condition triggers regardless
	// of duration < MinDuration.
	pm.RecordJobExecution("zero_min_job", 50*time.Millisecond, true)

	pm.jobMutex.RLock()
	min := pm.jobExecutions["zero_min_job"].MinDuration
	pm.jobMutex.RUnlock()

	if min != 50*time.Millisecond {
		t.Errorf("Expected MinDuration=50ms when previous was 0 (zero guard), got %v", min)
	}
}
