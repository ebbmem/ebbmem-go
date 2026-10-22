// Copyright 2026 Spectrayan — Proprietary

package ebbmem

import "fmt"

// Error is the typed failure for an Ebb HTTP call.
// errors.As matches *Error and the more specific wrappers.
type Error struct {
	Status     int
	Code       string
	Message    string
	Details    any
	RequestID  string
	RetryAfter *float64
}

func (e *Error) Error() string {
	if e == nil {
		return "ebbmem error"
	}
	if e.Code != "" && e.Status != 0 {
		return fmt.Sprintf("%s [%s] status=%d", e.Message, e.Code, e.Status)
	}
	if e.Status != 0 {
		return fmt.Sprintf("%s status=%d", e.Message, e.Status)
	}
	return e.Message
}

// AuthError is HTTP 401 or 403.
type AuthError struct{ Base *Error }

func (e *AuthError) Error() string { return e.Base.Error() }
func (e *AuthError) Unwrap() error { return e.Base }

// NotFoundError is HTTP 404.
type NotFoundError struct{ Base *Error }

func (e *NotFoundError) Error() string { return e.Base.Error() }
func (e *NotFoundError) Unwrap() error { return e.Base }

// ValidationError is HTTP 400 or 422.
type ValidationError struct{ Base *Error }

func (e *ValidationError) Error() string { return e.Base.Error() }
func (e *ValidationError) Unwrap() error { return e.Base }

// RateLimitError is HTTP 429.
type RateLimitError struct{ Base *Error }

func (e *RateLimitError) Error() string { return e.Base.Error() }
func (e *RateLimitError) Unwrap() error { return e.Base }

// ServerError is HTTP 5xx.
type ServerError struct{ Base *Error }

func (e *ServerError) Error() string { return e.Base.Error() }
func (e *ServerError) Unwrap() error { return e.Base }

// ConnectionError means the origin could not be reached.
type ConnectionError struct{ Base *Error }

func (e *ConnectionError) Error() string { return e.Base.Error() }
func (e *ConnectionError) Unwrap() error { return e.Base }
