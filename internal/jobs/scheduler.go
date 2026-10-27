// Periodic tasks run inside the server process (one instance, so no distributed lock is needed).
package jobs

import (
	"context"
	"log/slog"
	"time"
)

// Every runs task now and then every interval until ctx is cancelled; errors are logged, never fatal.
func Every(ctx context.Context, logger *slog.Logger, name string, interval time.Duration,
	task func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := task(ctx); err != nil && ctx.Err() == nil {
			logger.Error("scheduled task failed", "task", name, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
