// Package tools defines the [Tool] contract, JSON schema metadata, a
// thread-safe [Registry], and built-in filesystem and shell tools
// (view_file, edit_file, write_file, list_dir, grep_search, run_command).
//
// Key types and entry points are [Tool], [ToolSchema], [Property], [Registry],
// [NewRegistry], [NewDefaultRegistry], and [NewRegistrySubset].
//
// # Concurrency
//
// [Registry] is safe for concurrent use via an internal RWMutex and must not
// be copied after first use. Built-in tools are safe for concurrent Execute
// calls once configured; [Registry.SetWorkDir] updates tool working
// directories under the registry lock and should not race with active calls.
//
// # Boundaries
//
// Built-in file tools resolve relative paths against their working directory
// while accepting explicit absolute paths. [GrepTool] skips .git, .mobius,
// .cursor, node_modules, and vendor directories. [BashTool] runs "bash -c"
// under [exec.CommandContext] and caps combined output at 50 KB.
package tools
