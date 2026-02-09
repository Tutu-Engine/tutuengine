package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Progress Bar ───────────────────────────────────────────────────────────
// A production-quality terminal progress bar for model downloads.
// Shows: [████████████░░░░░░░░] 42% │ 1.2 GB / 2.8 GB │ 45.2 MB/s │ ETA 35s

const barWidth = 30 // Characters for the progress bar

type progressBar struct {
	started  time.Time
	total    int64
}

func newProgressBar() *progressBar {
	return &progressBar{
		started: time.Now(),
	}
}

// callback returns a function compatible with Manager.Pull's progress callback.
func (p *progressBar) callback(status string, pct float64) {
	now := time.Now()

	// Parse downloaded bytes from status (format: "downloading X / Y")
	if !strings.HasPrefix(status, "downloading") {
		// Not a download status — show simple text
		p.renderSimple(status, pct)
		return
	}

	p.renderBar(status, pct, now)
}

func (p *progressBar) renderSimple(status string, pct float64) {
	// For non-download phases (resolving, verifying, done)
	switch {
	case pct >= 100:
		clearLine()
		fmt.Fprintf(os.Stderr, "[done] %s\n", status)
	case strings.Contains(status, "resolving"):
		clearLine()
		fmt.Fprintf(os.Stderr, "[...] %s", status)
	case strings.Contains(status, "verifying"):
		clearLine()
		fmt.Fprintf(os.Stderr, "[...] verifying download...")
	case strings.Contains(status, "already"):
		clearLine()
		fmt.Fprintf(os.Stderr, "[ok] %s\n", status)
	case strings.Contains(status, "resuming"):
		clearLine()
		fmt.Fprintf(os.Stderr, "[resume] %s\n", status)
	default:
		clearLine()
		fmt.Fprintf(os.Stderr, "  %s", status)
	}
}

func (p *progressBar) renderBar(status string, pct float64, now time.Time) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	// Build the bar: [=======>............]
	filled := int(pct / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	var bar string
	if filled == barWidth {
		bar = strings.Repeat("=", filled)
	} else if filled > 0 {
		bar = strings.Repeat("=", filled-1) + ">" + strings.Repeat(".", empty)
	} else {
		bar = strings.Repeat(".", barWidth)
	}

	// Calculate speed from status
	// Status format: "downloading 123.4 MB / 456.7 MB"
	speed := p.calculateSpeed(pct, now)
	eta := p.calculateETA(pct, now)

	clearLine()
	fmt.Fprintf(os.Stderr, "  %s %3.0f%% | %s | %s | %s",
		bar, pct, extractSizeInfo(status), speed, eta)
}

func (p *progressBar) calculateSpeed(pct float64, now time.Time) string {
	elapsed := now.Sub(p.started).Seconds()
	if elapsed < 0.5 {
		return "-- MB/s"
	}

	// pct-based speed estimate
	if p.total > 0 {
		bytesDownloaded := float64(p.total) * pct / 100
		bytesPerSec := bytesDownloaded / elapsed
		return formatSpeed(int64(bytesPerSec))
	}

	return "-- MB/s"
}

func (p *progressBar) calculateETA(pct float64, now time.Time) string {
	if pct <= 0 || pct >= 100 {
		return "ETA --"
	}

	elapsed := now.Sub(p.started).Seconds()
	if elapsed < 1 {
		return "ETA --"
	}

	totalEstimated := elapsed / (pct / 100)
	remaining := totalEstimated - elapsed

	if remaining < 0 {
		remaining = 0
	}

	if remaining < 60 {
		return fmt.Sprintf("ETA %ds", int(remaining))
	}
	if remaining < 3600 {
		return fmt.Sprintf("ETA %dm%ds", int(remaining)/60, int(remaining)%60)
	}
	return fmt.Sprintf("ETA %dh%dm", int(remaining)/3600, (int(remaining)%3600)/60)
}

func extractSizeInfo(status string) string {
	// "downloading 123.4 MB / 456.7 MB" → "123.4 MB / 456.7 MB"
	if strings.HasPrefix(status, "downloading ") {
		return strings.TrimPrefix(status, "downloading ")
	}
	return status
}

func formatSpeed(bytesPerSec int64) string {
	return domain.HumanSize(bytesPerSec) + "/s"
}

func clearLine() {
	fmt.Fprintf(os.Stderr, "\r\033[K")
}
