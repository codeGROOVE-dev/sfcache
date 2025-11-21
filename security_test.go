package bdcache

import (
	"context"
	"testing"
)

func TestSecurity_PathTraversal_Get(t *testing.T) {
	ctx := context.Background()
	cache, err := New[string, string](ctx, WithLocalStore("security-test"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Set a valid key first
	if err := cache.Set(ctx, "valid", "data", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Try path traversal attacks - should all fail silently
	maliciousKeys := []string{
		"../../../etc/passwd",
		"..\\..\\..\\windows\\system32",
		"../../secret",
		"/etc/passwd",
		"C:\\Windows\\System32",
	}

	for _, key := range maliciousKeys {
		val, found, err := cache.Get(ctx, key)
		if err != nil {
			t.Errorf("Get(%q) returned error: %v (should handle gracefully)", key, err)
		}
		if found {
			t.Errorf("Get(%q) found value: %v (should not find malicious path)", key, val)
		}
	}
}

func TestSecurity_PathTraversal_Delete(t *testing.T) {
	ctx := context.Background()
	cache, err := New[string, string](ctx, WithLocalStore("security-test"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Logf("Close error: %v", err)
		}
	}()

	// Try to delete with path traversal - should not panic or succeed
	maliciousKeys := []string{
		"../../../etc/passwd",
		"../../important/file",
	}

	for _, key := range maliciousKeys {
		// Should not panic
		cache.Delete(ctx, key)
	}
}

func TestSecurity_InvalidCacheID(t *testing.T) {
	ctx := context.Background()

	maliciousCacheIDs := []string{
		"../../../etc",
		"../../passwd",
		"test\x00null",
		"/absolute/path",
		"path/with/slash",
		"path\\with\\backslash",
	}

	for _, cacheID := range maliciousCacheIDs {
		cache, err := New[string, string](ctx, WithLocalStore(cacheID))
		if err != nil {
			t.Errorf("New with cacheID %q failed: %v", cacheID, err)
			continue
		}
		func(c *Cache[string, string]) {
			defer func() {
				if err := c.Close(); err != nil {
					t.Logf("Close error: %v", err)
				}
			}()

			// Cache should have been created but persistence should be nil (graceful degradation)
			if c.persist != nil {
				t.Errorf("Cache with malicious cacheID %q should not have persistence enabled", cacheID)
			}

			// Memory-only cache should still work
			if err := c.Set(ctx, "test", "value", 0); err != nil {
				t.Errorf("Set failed on memory-only cache: %v", err)
			}
		}(cache)
	}
}
