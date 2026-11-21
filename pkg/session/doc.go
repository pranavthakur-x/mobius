// Package session manages active agent sessions, session switching, and
// disk-persisted chat transcript storage and legacy event imports.
//
// Key types and entry points are [Session], [NewSession], [NewRoleSession],
// [Manager], [NewManager], [ChatRecord], [TranscriptTurn], [LoadChats],
// [EnsureChat], [DeleteChat], and [ImportEventChats].
//
// # Concurrency
//
// [Session] and [Manager] are safe for concurrent use via internal RWMutex
// synchronization. File-backed chat operations ([LoadChats], [EnsureChat],
// [DeleteChat], [ImportEventChats], and related helpers) are serialized
// process-wide by an internal mutex.
//
// # Boundaries
//
// Default and role sessions load workspace guides from "config/guides.toml"
// via internal/guides when present and append the agent's configured limits to
// the system prompt.
package session
