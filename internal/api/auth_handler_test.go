package api

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginAttemptLimiterRemainsBounded(t *testing.T) {
	handler := NewAuthHandler(nil, 10)
	resetAt := time.Now().Add(time.Minute)
	for i := 0; i < maxTrackedLoginSources; i++ {
		handler.attempts[fmt.Sprintf("source-%d", i)] = loginAttemptWindow{Count: 1, ResetAt: resetAt}
	}
	if handler.allowAttempt("new-source") {
		t.Fatal("new source must be rejected while the bounded limiter is full")
	}
	if len(handler.attempts) != maxTrackedLoginSources {
		t.Fatalf("tracked sources=%d, want %d", len(handler.attempts), maxTrackedLoginSources)
	}
}
