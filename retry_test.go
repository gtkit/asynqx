package asynqx

import (
	"testing"
	"time"
)

const backoffSamples = 1000

func TestCappedExponentialBackoffStaysWithinBounds(t *testing.T) {
	base := time.Second
	maxDelay := time.Minute
	delayFunc := CappedExponentialBackoff(base, maxDelay)

	cases := []struct {
		retry    int
		lowBound time.Duration
		topBound time.Duration
	}{
		{retry: 0, lowBound: base / 2, topBound: base},
		{retry: 1, lowBound: base, topBound: base * 2},
		{retry: 2, lowBound: base * 2, topBound: base * 4},
		{retry: 100, lowBound: maxDelay / 2, topBound: maxDelay},
	}

	for _, tc := range cases {
		for range backoffSamples {
			got := delayFunc(tc.retry, nil, nil)
			if got < tc.lowBound || got > tc.topBound {
				t.Fatalf("retry=%d: delay %v out of bounds [%v, %v]", tc.retry, got, tc.lowBound, tc.topBound)
			}
		}
	}
}

func TestCappedExponentialBackoffNeverExceedsCap(t *testing.T) {
	maxDelay := 30 * time.Second
	delayFunc := CappedExponentialBackoff(time.Second, maxDelay)

	for retry := range 50 {
		for range backoffSamples {
			got := delayFunc(retry, nil, nil)
			if got > maxDelay {
				t.Fatalf("retry=%d: delay %v exceeds cap %v", retry, got, maxDelay)
			}
		}
	}
}

func TestCappedExponentialBackoffCapBelowBase(t *testing.T) {
	base := time.Minute
	maxDelay := time.Second
	delayFunc := CappedExponentialBackoff(base, maxDelay)

	for range backoffSamples {
		got := delayFunc(0, nil, nil)
		if got > maxDelay {
			t.Fatalf("delay %v exceeds cap %v when maxDelay < base", got, maxDelay)
		}
	}
}

func TestCappedExponentialBackoffNonPositiveInputs(t *testing.T) {
	cases := []struct {
		base     time.Duration
		maxDelay time.Duration
	}{
		{base: 0, maxDelay: time.Minute},
		{base: time.Second, maxDelay: 0},
		{base: -time.Second, maxDelay: time.Minute},
	}

	for _, tc := range cases {
		delayFunc := CappedExponentialBackoff(tc.base, tc.maxDelay)
		if got := delayFunc(3, nil, nil); got != 0 {
			t.Fatalf("base=%v maxDelay=%v: expected 0, got %v", tc.base, tc.maxDelay, got)
		}
	}
}
