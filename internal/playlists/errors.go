package playlists

import (
	"errors"
	"time"
)

type ErrorCode string

const (
	Input              ErrorCode = "input"
	Access             ErrorCode = "access"
	Restricted         ErrorCode = "restricted"
	Incomplete         ErrorCode = "incomplete"
	Temporary          ErrorCode = "temporary"
	Cancelled          ErrorCode = "cancelled"
	RuntimeUnavailable ErrorCode = "runtime_unavailable"
	Resource           ErrorCode = "resource"
)

// FetchError contains only user-safe diagnostics, never tokens or raw responses.
type FetchError struct {
	Code       ErrorCode
	Message    string
	RetryAfter time.Duration
}

func (e *FetchError) Error() string                { return e.Message }
func Failure(code ErrorCode, message string) error { return &FetchError{Code: code, Message: message} }
func Code(err error) ErrorCode {
	var e *FetchError
	if errors.As(err, &e) {
		return e.Code
	}
	return Temporary
}
