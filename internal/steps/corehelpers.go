package steps

import "time"

// pollFor runs check every second until it passes or d has gone by, and
// returns the last error. Used where a service needs a moment after a
// (re)start before it answers.
func pollFor(c *Ctx, d time.Duration, check func() error) error {
	deadline := time.Now().Add(d)
	for {
		err := check()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		select {
		case <-c.Context().Done():
			return c.Context().Err()
		case <-time.After(time.Second):
		}
	}
}
