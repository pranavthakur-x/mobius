// Package guides loads prioritized workspace Markdown guides from TOML
// configuration and renders them into system prompt sections.
//
// Key types and entry points are [Guide], [GuideSet], [NewGuideSet],
// [GuidesConfig], [Candidate], [LoadFromWorkSpace], and
// [GuideSet.RenderSystemPrompt].
//
// # Concurrency
//
// [GuideSet] is not synchronized for concurrent writes; callers should build
// and sort it before sharing it read-only across goroutines.
//
// # Boundaries
//
// [LoadFromWorkSpace] joins configured candidate paths with the workspace
// root, silently skipping files that do not exist. Depends only on
// github.com/pelletier/go-toml/v2 and the standard library.
package guides
