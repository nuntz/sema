package reddit

import "errors"

// EndedExplanation tells a user why Sema refuses a Reddit address. Reddit
// ended its public feeds in November 2026; see ADR 0009.
const EndedExplanation = "Reddit ended its public feeds, so Sema can’t follow subreddits."

// ErrEnded is returned for any input Matches recognizes, before any request
// is made to Reddit.
var ErrEnded = errors.New(EndedExplanation)
