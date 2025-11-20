package bdcache_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tstromberg/bdcache"
)

func ExampleCache_basic() {
	ctx := context.Background()

	// Create a simple in-memory cache
	cache, err := bdcache.New[string, int](ctx)
	if err != nil {
		panic(err)
	}
	defer cache.Close()

	// Store a value
	if err := cache.Set(ctx, "answer", 42, 0); err != nil {
		panic(err)
	}

	// Retrieve it
	val, found, _ := cache.Get(ctx, "answer")
	if found {
		fmt.Printf("The answer is %d\n", val)
	}

	// Output: The answer is 42
}

func ExampleCache_withTTL() {
	ctx := context.Background()

	// Create cache with default TTL
	cache, err := bdcache.New[string, string](ctx,
		bdcache.WithDefaultTTL(5*time.Minute),
	)
	if err != nil {
		panic(err)
	}
	defer cache.Close()

	// Set with default TTL (ttl=0 uses configured DefaultTTL)
	if err := cache.Set(ctx, "session", "user-123", 0); err != nil {
		panic(err)
	}

	// Set with custom TTL (overrides default)
	if err := cache.Set(ctx, "token", "abc123", 1*time.Hour); err != nil {
		panic(err)
	}

	// Retrieve values
	session, found, _ := cache.Get(ctx, "session")
	if found {
		fmt.Printf("Session: %s\n", session)
	}

	// Output: Session: user-123
}

func ExampleCache_withLocalStore() {
	ctx := context.Background()

	// Create cache with local file persistence
	cache, err := bdcache.New[string, string](ctx,
		bdcache.WithLocalStore("myapp"),
		bdcache.WithMemorySize(5000),
	)
	if err != nil {
		panic(err)
	}
	defer cache.Close()

	// Values are cached in memory and persisted to disk
	if err := cache.Set(ctx, "config", "production", 0); err != nil {
		panic(err)
	}

	// After restart, values are loaded from disk automatically
	val, found, _ := cache.Get(ctx, "config")
	if found {
		fmt.Printf("Config: %s\n", val)
	}

	// Output: Config: production
}

func ExampleCache_withBestStore() {
	ctx := context.Background()

	// Automatically selects best storage:
	// - Cloud Datastore if K_SERVICE env var is set (Cloud Run/Knative)
	// - Local files otherwise
	cache, err := bdcache.New[string, int](ctx,
		bdcache.WithBestStore("myapp"),
	)
	if err != nil {
		panic(err)
	}
	defer cache.Close()

	if err := cache.Set(ctx, "counter", 100, 0); err != nil {
		panic(err)
	}

	val, found, _ := cache.Get(ctx, "counter")
	if found {
		fmt.Printf("Counter: %d\n", val)
	}

	// Output: Counter: 100
}

func ExampleCache_structValues() {
	ctx := context.Background()

	type User struct {
		ID    int
		Name  string
		Email string
	}

	// Cache can store any type
	cache, err := bdcache.New[int, User](ctx)
	if err != nil {
		panic(err)
	}
	defer cache.Close()

	// Store a struct
	user := User{
		ID:    1,
		Name:  "Alice",
		Email: "alice@example.com",
	}
	if err := cache.Set(ctx, user.ID, user, 0); err != nil {
		panic(err)
	}

	// Retrieve it
	retrieved, found, _ := cache.Get(ctx, 1)
	if found {
		fmt.Printf("User: %s (%s)\n", retrieved.Name, retrieved.Email)
	}

	// Output: User: Alice (alice@example.com)
}
