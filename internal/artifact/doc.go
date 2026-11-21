// Package artifact intercepts oversized tool outputs and offloads them to
// disk-backed files while keeping bounded previews in the LLM context.
//
// Key entry points are [NewStore], [Store.Save], [Store.Read], and
// [Intercept].
//
// # Concurrency
//
// [Store] holds only an immutable root directory path and writes each
// artifact to a unique filename generated via internal/id, making [Store] and
// [Intercept] safe for concurrent calls across distinct artifacts.
//
// # Boundaries
//
// Outputs from view_file, list_dir, and grep_search remain inline because
// those tools already bound their own output. Other tool outputs exceeding
// [SmallThreshold] are persisted under the configured directory (default
// ".mobius/artifacts") and referenced via "artifact://" URIs.
package artifact
