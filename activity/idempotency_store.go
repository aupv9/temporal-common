package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// IdempotencyStore deduplicates activity executions across retries.
//
// Every activity that calls external services MUST check the store before
// executing and record the result after success. This prevents double-execution
// when Temporal retries an activity whose result was lost in transit.
//
// Implementations must be safe for concurrent use.
type IdempotencyStore interface {
	// Check returns the previously recorded result for key, if any.
	// found=false means the activity has not been executed yet — proceed normally.
	// found=true means return the stored result immediately (skip re-execution).
	Check(ctx context.Context, key string) (result []byte, found bool, err error)

	// Record persists the result for key after successful execution.
	// Must be idempotent — calling Record twice for the same key is safe.
	Record(ctx context.Context, key string, result []byte, ttl time.Duration) error
}

// DefaultTTL is the default time-to-live for recorded idempotency results.
// After this duration the record is eligible for eviction.
// Should be longer than the workflow's maximum execution time.
const DefaultTTL = 7 * 24 * time.Hour // 7 days

// ExecuteIdempotent wraps an activity body with idempotency check-and-record.
// If the store already has a result for key, it is deserialised into result and
// returned immediately without calling fn. Otherwise fn is called, its output
// serialised, stored, and returned.
//
// Usage inside an activity:
//
//	func TransferActivity(ctx context.Context, input TransferInput) (TransferResult, error) {
//	    key := temporalcommon.IdempotencyKeyNoRetry(activity.GetInfo(ctx))
//	    var result TransferResult
//	    err := activity.ExecuteIdempotent(ctx, store, key, &result, func() error {
//	        r, err := bankAPI.Transfer(input)
//	        if err != nil { return err }
//	        result = r
//	        return nil
//	    })
//	    return result, err
//	}
func ExecuteIdempotent(
	ctx context.Context,
	store IdempotencyStore,
	key string,
	resultPtr interface{},
	fn func() error,
) error {
	return ExecuteIdempotentWithTTL(ctx, store, key, resultPtr, DefaultTTL, fn)
}

// ExecuteIdempotentWithTTL is like ExecuteIdempotent but lets the caller
// specify a custom TTL for the stored result.
func ExecuteIdempotentWithTTL(
	ctx context.Context,
	store IdempotencyStore,
	key string,
	resultPtr interface{},
	ttl time.Duration,
	fn func() error,
) error {
	// 1. Check store first.
	data, found, err := store.Check(ctx, key)
	if err != nil {
		// Store read failure is treated as a miss — execute normally.
		// This prevents a broken store from blocking all activities.
		// Log the error but continue; the activity is still idempotent
		// via the key if the store recovers and we record after execution.
		_ = err // caller's logger should have context via ctx
	}
	if found && data != nil {
		// Deserialise the cached result.
		if resultPtr != nil {
			if jsonErr := json.Unmarshal(data, resultPtr); jsonErr != nil {
				// Corrupted cache entry — re-execute rather than return garbage.
				found = false
			}
		}
		if found {
			return nil
		}
	}

	// 2. Execute the activity body.
	if execErr := fn(); execErr != nil {
		return execErr
	}

	// 3. Record the result for future retries.
	if resultPtr != nil {
		data, err = json.Marshal(resultPtr)
		if err != nil {
			// Serialisation failure is non-fatal — the activity succeeded.
			// Future retries may re-execute, but that's better than failing now.
			return nil
		}
	} else {
		data = []byte("null")
	}

	_ = store.Record(ctx, key, data, ttl) // best-effort — ignore record errors
	return nil
}

// ---------------------------------------------------------------------------
// In-memory implementation (for tests and local development)
// ---------------------------------------------------------------------------

type memEntry struct {
	data      []byte
	expiresAt time.Time
}

// InMemoryIdempotencyStore is a thread-safe in-memory IdempotencyStore.
// Suitable for tests and single-process local development only.
// NOT suitable for production (data lost on restart, not shared across workers).
type InMemoryIdempotencyStore struct {
	mu      sync.RWMutex
	entries map[string]memEntry
}

// NewInMemoryIdempotencyStore creates a new in-memory store.
func NewInMemoryIdempotencyStore() *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{
		entries: make(map[string]memEntry),
	}
}

func (s *InMemoryIdempotencyStore) Check(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}
	if time.Now().After(entry.expiresAt) {
		return nil, false, nil // expired
	}
	return entry.data, true, nil
}

func (s *InMemoryIdempotencyStore) Record(_ context.Context, key string, result []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[key] = memEntry{
		data:      result,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}

// Flush removes all entries. Useful between tests.
func (s *InMemoryIdempotencyStore) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]memEntry)
}

// Len returns the number of stored entries (including expired ones).
func (s *InMemoryIdempotencyStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// ---------------------------------------------------------------------------
// Redis-backed implementation (production)
// ---------------------------------------------------------------------------

// RedisClient is the minimal Redis interface required by RedisIdempotencyStore.
// Compatible with go-redis v8/v9 (*redis.Client) and any mock.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
}

// ErrRedisNil is returned by redis Get when the key does not exist.
// This mirrors go-redis's redis.Nil sentinel.
const ErrRedisNil = redisNilSentinel("redis: nil")

type redisNilSentinel string

func (r redisNilSentinel) Error() string { return string(r) }

// RedisIdempotencyStore stores idempotency results in Redis.
// Use this in production environments with a Redis cluster or Sentinel.
//
// Setup:
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	store := activity.NewRedisIdempotencyStore(rdb, "myservice")
type RedisIdempotencyStore struct {
	client    RedisClient
	keyPrefix string
}

// NewRedisIdempotencyStore creates a Redis-backed store.
// keyPrefix namespaces keys, e.g. "loan-service" → "loan-service:idem:{key}".
func NewRedisIdempotencyStore(client RedisClient, keyPrefix string) *RedisIdempotencyStore {
	return &RedisIdempotencyStore{client: client, keyPrefix: keyPrefix}
}

func (s *RedisIdempotencyStore) redisKey(key string) string {
	return fmt.Sprintf("%s:idem:%s", s.keyPrefix, key)
}

func (s *RedisIdempotencyStore) Check(ctx context.Context, key string) ([]byte, bool, error) {
	val, err := s.client.Get(ctx, s.redisKey(key))
	if err != nil {
		if err.Error() == ErrRedisNil.Error() {
			return nil, false, nil // key not found
		}
		return nil, false, fmt.Errorf("idempotency store check: %w", err)
	}
	return []byte(val), true, nil
}

func (s *RedisIdempotencyStore) Record(ctx context.Context, key string, result []byte, ttl time.Duration) error {
	if err := s.client.Set(ctx, s.redisKey(key), string(result), ttl); err != nil {
		return fmt.Errorf("idempotency store record: %w", err)
	}
	return nil
}
