package synchromanager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestItemExponentialFailureAndJitterSlowRateLimter(t *testing.T) {
	limiter := NewItemExponentialFailureAndJitterSlowRateLimter(1*time.Second, 5*time.Second, 10*time.Second, 1.0, 5)

	assert.Equal(t, 1*time.Second, limiter.When("one"))
	assert.Equal(t, 2*time.Second, limiter.When("one"))
	assert.Equal(t, 4*time.Second, limiter.When("one"))
	assert.Equal(t, 5*time.Second, limiter.When("one"))
	assert.Equal(t, 5*time.Second, limiter.When("one"))
	assert.GreaterOrEqual(t, limiter.When("one"), 10*time.Second)

	assert.Equal(t, 6, limiter.NumRequeues("one"))

	assert.Equal(t, 1*time.Second, limiter.When("two"))
	assert.Equal(t, 2*time.Second, limiter.When("two"))

	limiter.Forget("one")
	assert.Equal(t, 1*time.Second, limiter.When("one"))
	assert.Equal(t, 2*time.Second, limiter.When("one"))

	assert.Equal(t, 4*time.Second, limiter.When("two"))
}
