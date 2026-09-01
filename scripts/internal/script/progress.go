package script

import (
	"log/slog"
	"time"
)

// Progress reports how far through a long pass we are.
//
// These scripts spend minutes walking a thousand releases, several of those seconds
// waiting on a rate-limited API, and until now they printed nothing between "starting"
// and "complete". A run that is working and a run that is wedged looked identical.
type Progress struct {
	label    string
	total    int
	done     int
	every    time.Duration
	started  time.Time
	lastTick time.Time
}

// NewProgress starts a counter that logs at most once every few seconds, so a fast
// pass stays quiet and a slow one keeps talking.
func NewProgress(label string, total int) *Progress {
	now := time.Now()
	slog.Info("Starting pass", slog.String("pass", label), slog.Int("items", total))
	return &Progress{label: label, total: total, every: 3 * time.Second, started: now, lastTick: now}
}

// Step records one item and logs if enough time has passed.
func (p *Progress) Step() {
	if p == nil {
		return
	}
	p.done++

	if time.Since(p.lastTick) < p.every && p.done < p.total {
		return
	}
	p.lastTick = time.Now()

	elapsed := time.Since(p.started)
	attrs := []any{
		slog.String("pass", p.label),
		slog.String("progress", itoa(p.done)+"/"+itoa(p.total)),
		slog.Int("percent", percent(p.done, p.total)),
		slog.Duration("elapsed", elapsed.Truncate(time.Second)),
	}
	if eta, ok := p.eta(elapsed); ok {
		attrs = append(attrs, slog.Duration("eta", eta))
	}
	slog.Info("Working", attrs...)
}

// Done reports the final tally.
func (p *Progress) Done() {
	if p == nil {
		return
	}
	slog.Info("Pass finished",
		slog.String("pass", p.label),
		slog.Int("items", p.done),
		slog.Duration("took", time.Since(p.started).Truncate(time.Second)))
}

func (p *Progress) eta(elapsed time.Duration) (time.Duration, bool) {
	if p.done == 0 || p.total <= p.done {
		return 0, false
	}
	perItem := elapsed / time.Duration(p.done)
	return (perItem * time.Duration(p.total-p.done)).Truncate(time.Second), true
}

func percent(done, total int) int {
	if total <= 0 {
		return 100
	}
	return done * 100 / total
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
