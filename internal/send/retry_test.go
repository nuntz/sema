package send

import (
	"testing"
	"time"
)

func TestRetryDelayFollowsTheFourAttemptSchedule(t *testing.T) {
	for _, test := range []struct {
		attemptsMade int
		wantDelay    time.Duration
		wantRetry    bool
	}{
		{attemptsMade: 1, wantDelay: 5 * time.Second, wantRetry: true},
		{attemptsMade: 2, wantDelay: 30 * time.Second, wantRetry: true},
		{attemptsMade: 3, wantDelay: 2 * time.Minute, wantRetry: true},
		{attemptsMade: 4, wantRetry: false},
		{attemptsMade: 0, wantRetry: false},
	} {
		delay, retry := RetryDelay(test.attemptsMade)
		if delay != test.wantDelay || retry != test.wantRetry {
			t.Errorf("RetryDelay(%d) = %v, %v; want %v, %v", test.attemptsMade, delay, retry, test.wantDelay, test.wantRetry)
		}
	}
}
