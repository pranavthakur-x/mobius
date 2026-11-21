// Package id provides collision-resistant, prefixed identifier generators
// for Mobius threads, events, and artifacts.
//
// Key entry points are [NewID], [NewThreadID], [NewEventID], and
// [NewArtifactID].
//
// # Concurrency
//
// All functions in this package are stateless and safe for concurrent use.
//
// # Boundaries
//
// Depends only on the Go standard library (crypto/rand, fmt, time).
package id
