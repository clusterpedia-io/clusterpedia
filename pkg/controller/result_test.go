package controller

import (
	"math/rand"
	"testing"
)

func TestRequeueResult(t *testing.T) {
	retryCount := rand.Intn(100) + 0
	result := RequeueResult(retryCount)

	if !result.Requeue() {
		t.Errorf("Expected Requeue to be true, but got false")
	}

	if result.MaxRetryCount() != retryCount {
		t.Errorf("Expected MaxRetryCount to be %d, but got %d", retryCount, result.MaxRetryCount())
	}
}

func TestNoRequeueResult(t *testing.T) {
	result := NoRequeueResult

	if result.Requeue() {
		t.Errorf("Expected Requeue to be false, but got true")
	}

	if result.MaxRetryCount() != 0 {
		t.Errorf("Expected MaxRetryCount to be 0, but got %d", result.MaxRetryCount())
	}
}
