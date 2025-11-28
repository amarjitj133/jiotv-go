package television

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetCacheExpiry(t *testing.T) {
	// Set a specific expiry time for testing
	expectedExpiry := time.Now().Add(1 * time.Hour)
	cacheMutex.Lock()
	cacheExpiry = expectedExpiry
	cacheMutex.Unlock()

	// Call the function to get the cache expiry time
	actualExpiry := GetCacheExpiry()

	// Assert that the actual expiry time matches the expected expiry time
	assert.Equal(t, expectedExpiry, actualExpiry, "The actual cache expiry time should be equal to the expected expiry time.")
}
