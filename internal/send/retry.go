package send

import "time"

// MaxAttempts counts the inline attempt plus the queued retries.
const MaxAttempts = 4

var retryDelays = [...]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}

// RetryDelay reports how long to wait after attemptsMade failed retryable
// attempts, and false once the schedule is exhausted.
func RetryDelay(attemptsMade int) (time.Duration, bool) {
	if attemptsMade < 1 || attemptsMade > len(retryDelays) {
		return 0, false
	}
	return retryDelays[attemptsMade-1], true
}
