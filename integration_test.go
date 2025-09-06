package main

import (
	"context"
	"testing"
	"time"

	"github.com/netresearch/ofelia/cli/config"
	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/web"
)

// TestIntegrationAllPhases validates that all three improvement phases work together
func TestIntegrationAllPhases(t *testing.T) {
	t.Run("SecurityPerformanceArchitecture", func(t *testing.T) {
		// Test that security hardening, performance optimization, and architecture refactoring
		// work together without conflicts

		// 1. Test unified configuration with security controls
		cfg := config.NewUnifiedConfig()
		cfg.Global.AllowHostJobsFromLabels = false
		
		// Should block dangerous host jobs
		if err := cfg.ValidateSecurityPolicy(); err == nil {
			t.Error("Expected security validation to catch dangerous operations")
		}

		// 2. Test performance optimizations work with new architecture
		scheduler := core.NewScheduler(core.NewTestLogger())
		
		// Should use optimized components
		if !scheduler.IsOptimized() {
			t.Error("Expected scheduler to use optimized components")
		}

		// 3. Test secure authentication with performance optimizations
		authManager := web.NewSecureAuthManager(cfg.AuthConfig)
		
		// Should handle high-performance token operations
		start := time.Now()
		for i := 0; i < 1000; i++ {
			token, err := authManager.GenerateToken("testuser")
			if err != nil {
				t.Fatalf("Token generation failed: %v", err)
			}
			
			if !authManager.ValidateToken(token) {
				t.Error("Token validation failed")
			}
		}
		
		elapsed := time.Since(start)
		if elapsed > 100*time.Millisecond {
			t.Errorf("Performance regression: 1000 token ops took %v (expected < 100ms)", elapsed)
		}
	})
}

// TestSystemIntegration performs end-to-end system validation
func TestSystemIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize system with all improvements
	system, err := initializeImprovedSystem(ctx)
	if err != nil {
		t.Fatalf("System initialization failed: %v", err)
	}
	defer system.Shutdown()

	// Test complete workflow
	t.Run("CompleteWorkflow", func(t *testing.T) {
		// 1. Secure authentication
		token, err := system.Auth.Login("admin", "secure_password")
		if err != nil {
			t.Fatalf("Authentication failed: %v", err)
		}

		// 2. Create job with unified configuration
		jobConfig := &config.UnifiedJobConfig{
			Name:     "integration-test",
			Schedule: "@every 1s",
			Command:  "echo 'test'",
			Type:     config.JobTypeExec,
		}

		jobID, err := system.Scheduler.AddUnifiedJob(jobConfig, token)
		if err != nil {
			t.Fatalf("Job creation failed: %v", err)
		}

		// 3. Verify job execution with performance optimizations
		executions := system.Scheduler.WaitForExecution(jobID, 5*time.Second)
		if len(executions) == 0 {
			t.Error("Job execution failed")
		}

		// 4. Verify security controls work
		maliciousConfig := &config.UnifiedJobConfig{
			Name:     "malicious-job",
			Schedule: "@every 1s", 
			Command:  "rm -rf /",  // Should be blocked
			Type:     config.JobTypeLocal,
		}

		_, err = system.Scheduler.AddUnifiedJob(maliciousConfig, token)
		if err == nil {
			t.Error("Expected security controls to block malicious job")
		}
	})
}

// Mock system for integration testing
type ImprovedSystem struct {
	Auth      *web.SecureAuthManager
	Scheduler *core.OptimizedScheduler
	Config    *config.UnifiedConfigManager
}

func (s *ImprovedSystem) Shutdown() {
	if s.Scheduler != nil {
		s.Scheduler.Stop()
	}
}

func initializeImprovedSystem(ctx context.Context) (*ImprovedSystem, error) {
	// Initialize with all improvements enabled
	configManager := config.NewUnifiedConfigManager()
	
	cfg, err := configManager.LoadConfig("test.ini")
	if err != nil {
		return nil, err
	}

	authManager := web.NewSecureAuthManager(cfg.AuthConfig)
	scheduler := core.NewOptimizedScheduler(core.NewTestLogger())

	return &ImprovedSystem{
		Auth:      authManager,
		Scheduler: scheduler,
		Config:    configManager,
	}, nil
}