// Copyright 2026 Spectrayan — Proprietary

package ebbmem

import (
	"errors"
	"math"
	"math/rand"
	"time"
)

var idempotent = map[string]struct{}{
	"recall": {}, "recallFederated": {}, "get": {}, "list": {}, "status": {},
	"whyNot": {}, "health": {}, "ready": {}, "tasks.get": {}, "tasks.list": {},
	"tasks.cancel": {}, "forget": {},
}

func isIdempotent(name, idempotencyKey string) bool {
	if _, ok := idempotent[name]; ok {
		return true
	}
	return (name == "remember" || name == "rememberBatch") && idempotencyKey != ""
}

func retryable(err error, idempotentCall bool) bool {
	if !idempotentCall || err == nil {
		return false
	}
	var conn *ConnectionError
	if errors.As(err, &conn) {
		return true
	}
	var rate *RateLimitError
	if errors.As(err, &rate) {
		return true
	}
	var server *ServerError
	if errors.As(err, &server) && server.Base != nil {
		switch server.Base.Status {
		case 502, 503, 504:
			return true
		}
	}
	return false
}

func backoff(attempt int, err error) time.Duration {
	var rate *RateLimitError
	if errors.As(err, &rate) && rate.Base != nil && rate.Base.RetryAfter != nil {
		if *rate.Base.RetryAfter <= 0 {
			return 0
		}
		return time.Duration(*rate.Base.RetryAfter * float64(time.Second))
	}
	base := math.Min(8, 0.2*math.Pow(2, float64(attempt)))
	jitter := rand.Float64() * base / 2
	return time.Duration((base + jitter) * float64(time.Second))
}
