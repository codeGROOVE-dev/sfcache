package bdcache

import (
	"context"
	"os"
	"testing"
	"time"
)

// Note: These tests require DATASTORE_EMULATOR_HOST to be set or actual GCP credentials.
// They will be skipped if the environment is not configured.

func skipIfNoDatastore(t *testing.T) {
	if os.Getenv("DATASTORE_EMULATOR_HOST") == "" && os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		t.Skip("Skipping datastore tests: no emulator or credentials configured")
	}
}

func TestDatastorePersist_StoreLoad(t *testing.T) {
	skipIfNoDatastore(t)

	ctx := context.Background()
	dp, err := newDatastorePersist[string, int](ctx, "test-cache")
	if err != nil {
		t.Fatalf("newDatastorePersist: %v", err)
	}
	defer dp.Close()

	// Store a value
	if err := dp.Store(ctx, "key1", 42, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Load the value
	val, expiry, found, err := dp.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("key1 not found")
	}
	if val != 42 {
		t.Errorf("Load value = %d; want 42", val)
	}
	if !expiry.IsZero() {
		t.Error("expiry should be zero")
	}

	// Cleanup
	dp.Delete(ctx, "key1")
}

func TestDatastorePersist_LoadMissing(t *testing.T) {
	skipIfNoDatastore(t)

	ctx := context.Background()
	dp, err := newDatastorePersist[string, int](ctx, "test-cache")
	if err != nil {
		t.Fatalf("newDatastorePersist: %v", err)
	}
	defer dp.Close()

	// Load non-existent key
	_, _, found, err := dp.Load(ctx, "missing-key-12345")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("missing key should not be found")
	}
}

func TestDatastorePersist_TTL(t *testing.T) {
	skipIfNoDatastore(t)

	ctx := context.Background()
	dp, err := newDatastorePersist[string, string](ctx, "test-cache")
	if err != nil {
		t.Fatalf("newDatastorePersist: %v", err)
	}
	defer dp.Close()

	// Store with past expiry
	past := time.Now().Add(-1 * time.Second)
	if err := dp.Store(ctx, "expired", "value", past); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Should not be loadable
	_, _, found, err := dp.Load(ctx, "expired")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("expired key should not be found")
	}
}

func TestDatastorePersist_Delete(t *testing.T) {
	skipIfNoDatastore(t)

	ctx := context.Background()
	dp, err := newDatastorePersist[string, int](ctx, "test-cache")
	if err != nil {
		t.Fatalf("newDatastorePersist: %v", err)
	}
	defer dp.Close()

	// Store and delete
	if err := dp.Store(ctx, "key1", 42, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if err := dp.Delete(ctx, "key1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Should not be loadable
	_, _, found, err := dp.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("deleted key should not be found")
	}

	// Deleting non-existent key should not error
	if err := dp.Delete(ctx, "missing-key-99999"); err != nil {
		t.Errorf("Delete missing key: %v", err)
	}
}

func TestDatastorePersist_Update(t *testing.T) {
	skipIfNoDatastore(t)

	ctx := context.Background()
	dp, err := newDatastorePersist[string, string](ctx, "test-cache")
	if err != nil {
		t.Fatalf("newDatastorePersist: %v", err)
	}
	defer dp.Close()

	// Store initial value
	if err := dp.Store(ctx, "key", "value1", time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Update value
	if err := dp.Store(ctx, "key", "value2", time.Time{}); err != nil {
		t.Fatalf("Store update: %v", err)
	}

	// Load and verify updated value
	val, _, found, err := dp.Load(ctx, "key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("key not found")
	}
	if val != "value2" {
		t.Errorf("Load value = %s; want value2", val)
	}

	// Cleanup
	dp.Delete(ctx, "key")
}

func TestDatastorePersist_ComplexValue(t *testing.T) {
	skipIfNoDatastore(t)

	type User struct {
		Name  string
		Email string
		Age   int
	}

	ctx := context.Background()
	dp, err := newDatastorePersist[string, User](ctx, "test-cache")
	if err != nil {
		t.Fatalf("newDatastorePersist: %v", err)
	}
	defer dp.Close()

	user := User{
		Name:  "Alice",
		Email: "alice@example.com",
		Age:   30,
	}

	// Store complex value
	if err := dp.Store(ctx, "user1", user, time.Time{}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Load and verify
	loaded, _, found, err := dp.Load(ctx, "user1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("user1 not found")
	}
	if loaded.Name != user.Name || loaded.Email != user.Email || loaded.Age != user.Age {
		t.Errorf("Load value = %+v; want %+v", loaded, user)
	}

	// Cleanup
	dp.Delete(ctx, "user1")
}
