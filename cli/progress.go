package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/netresearch/ofelia/core"
)

// ProgressIndicator provides visual feedback for long-running operations
type ProgressIndicator struct {
	logger     core.Logger
	writer     io.Writer
	message    string
	done       chan struct{}
	mu         sync.Mutex
	isTerminal bool
	ticker     *time.Ticker
	started    bool
}

// NewProgressIndicator creates a new progress indicator
// If output is not a terminal (e.g., piped to file), it uses simple log messages instead of spinners
func NewProgressIndicator(logger core.Logger, message string) *ProgressIndicator {
	writer := os.Stdout
	isTerminal := term.IsTerminal(int(writer.Fd()))

	return &ProgressIndicator{
		logger:     logger,
		writer:     writer,
		message:    message,
		done:       make(chan struct{}),
		isTerminal: isTerminal,
	}
}

// Start begins displaying the progress indicator
func (p *ProgressIndicator) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return
	}
	p.started = true

	if !p.isTerminal {
		// Non-terminal: just log the start message
		p.logger.Noticef("%s...", p.message)
		return
	}

	// Terminal: show animated spinner
	p.ticker = time.NewTicker(100 * time.Millisecond)
	go p.animate()
}

// Stop stops the progress indicator and shows completion message
func (p *ProgressIndicator) Stop(success bool, resultMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return
	}

	p.started = false

	// Safely close done channel only once
	select {
	case <-p.done:
		// Already closed
	default:
		close(p.done)
	}

	if p.ticker != nil {
		p.ticker.Stop()
	}

	if !p.isTerminal {
		// Non-terminal: just log the result
		if success {
			p.logger.Noticef("✅ %s", resultMsg)
		} else {
			p.logger.Errorf("❌ %s", resultMsg)
		}
		return
	}

	// Terminal: clear spinner line and show result
	fmt.Fprintf(p.writer, "\r%s\r", strings.Repeat(" ", len(p.message)+10))
	if success {
		fmt.Fprintf(p.writer, "✅ %s\n", resultMsg)
	} else {
		fmt.Fprintf(p.writer, "❌ %s\n", resultMsg)
	}
}

// Update changes the progress message (for multi-step operations)
func (p *ProgressIndicator) Update(newMessage string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isTerminal {
		p.logger.Noticef("%s...", newMessage)
		p.message = newMessage
		return
	}

	// Clear current line
	fmt.Fprintf(p.writer, "\r%s\r", strings.Repeat(" ", len(p.message)+10))
	p.message = newMessage
}

// animate runs the spinner animation (only for terminal output)
func (p *ProgressIndicator) animate() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0

	// Get ticker channel under mutex to prevent race
	p.mu.Lock()
	if p.ticker == nil {
		p.mu.Unlock()
		return
	}
	tickerC := p.ticker.C
	p.mu.Unlock()

	for {
		select {
		case <-p.done:
			return
		case <-tickerC:
			p.mu.Lock()
			fmt.Fprintf(p.writer, "\r%s %s", frames[i], p.message)
			p.mu.Unlock()
			i = (i + 1) % len(frames)
		}
	}
}

// ProgressReporter provides structured progress reporting for multi-step operations
type ProgressReporter struct {
	logger      core.Logger
	totalSteps  int
	currentStep int
	mu          sync.Mutex
	isTerminal  bool
}

// NewProgressReporter creates a new multi-step progress reporter
func NewProgressReporter(logger core.Logger, totalSteps int) *ProgressReporter {
	return &ProgressReporter{
		logger:     logger,
		totalSteps: totalSteps,
		isTerminal: term.IsTerminal(int(os.Stdout.Fd())),
	}
}

// Step reports progress for a single step
func (pr *ProgressReporter) Step(stepNum int, message string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.currentStep = stepNum

	// Protect against division by zero
	if pr.totalSteps == 0 {
		return
	}

	if pr.isTerminal {
		// Terminal: show progress bar
		progress := float64(stepNum) / float64(pr.totalSteps) * 100
		bar := pr.renderProgressBar(progress)
		fmt.Fprintf(os.Stdout, "\r[%d/%d] %s %s", stepNum, pr.totalSteps, bar, message)
		if stepNum == pr.totalSteps {
			fmt.Fprintln(os.Stdout) // New line on completion
		}
	} else {
		// Non-terminal: simple log messages
		pr.logger.Noticef("[%d/%d] %s", stepNum, pr.totalSteps, message)
	}
}

// renderProgressBar creates a visual progress bar
func (pr *ProgressReporter) renderProgressBar(percent float64) string {
	barWidth := 20
	filled := int(percent / 100.0 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return fmt.Sprintf("%s %.0f%%", bar, percent)
}

// Complete marks all steps as complete
func (pr *ProgressReporter) Complete(message string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if pr.isTerminal {
		fmt.Fprintln(os.Stdout)
	}
	pr.logger.Noticef("✅ %s", message)
}
