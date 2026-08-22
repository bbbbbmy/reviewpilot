package reviewengine

import "errors"

// ErrMissingLLM is returned by NewDefaultEngine when deps.LLM is nil.
var ErrMissingLLM = errors.New("reviewengine: LLM provider is required")

// ErrMissingPlatform is returned by NewDefaultEngine when deps.Platform
// is nil.
var ErrMissingPlatform = errors.New("reviewengine: Platform adapter is required")
