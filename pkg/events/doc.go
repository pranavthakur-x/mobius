// Package events defines immutable session trajectory facts and provides an
// asynchronous JSONL-backed [EventStore].
//
// Key types and entry points are [Event], [EventType], [EventStore],
// [FileEventStore], and [NewFileEventStore].
//
// # Concurrency
//
// [FileEventStore] is safe for concurrent use. [FileEventStore.Append]
// enqueues events to a background worker goroutine (falling back to a
// mutex-guarded synchronous write when the buffer is full). Calling
// [FileEventStore.Close] drains all queued events and waits for the worker
// to exit.
//
// # Boundaries
//
// Events are persisted per thread as "<threadID>.jsonl" files under the
// configured directory (default ".mobius/events"). Depends on pkg/llm and
// internal/id.
package events
