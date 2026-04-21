package csv

import "errors"

// ErrNoCSVConfig indicates that none of the configured patterns matched a
// given CSV file path. Callers that process multiple files in a pipeline
// should typically log a warning and skip the offending file via
// errors.Is(err, ErrNoCSVConfig), instead of failing the whole batch.
var ErrNoCSVConfig = errors.New("no configuration found for CSV file (no pattern matches)")
