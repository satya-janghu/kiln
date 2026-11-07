package runner

import "errors"

// ErrBusy means another kiln holds the lock (its text contains "busy").
var ErrBusy = errors.New("lock busy: another kiln is running")
