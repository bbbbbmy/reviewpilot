package reviewengine

import "errors"

// ErrInvalidBudget is returned by Split when the budget is zero or
// negative.
var ErrInvalidBudget = errors.New("reviewengine: budget must be positive")

// ErrNoChunkFitsBudget is returned by Split when the entire diff
// exceeds the budget and no individual file fits either.
var ErrNoChunkFitsBudget = errors.New("reviewengine: no chunk fits within budget")
