// Package dbwait blocks service startup until the database accepts connections.
//
// The notes service and PostgreSQL are deployed together, so on first start the
// database is usually not reachable yet. Failing on the first ping would put the
// pod into CrashLoopBackOff even though the database comes up moments later.
package dbwait

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// Ping retries db.PingContext every interval until it succeeds, ctx is done, or
// timeout elapses. It returns the last ping error if the database never became reachable.
func Ping(ctx context.Context, db *sql.DB, timeout, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for attempt := 1; ; attempt++ {
		err := db.PingContext(ctx)
		if err == nil {
			if attempt > 1 {
				log.Printf("Database reachable after %d attempts", attempt)
			}
			return nil
		}
		log.Printf("Database not reachable yet (attempt %d): %v", attempt, err)

		select {
		case <-ctx.Done():
			return fmt.Errorf("database not reachable within %s: %w", timeout, err)
		case <-ticker.C:
		}
	}
}
