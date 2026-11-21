// Package sensors defines verification sensors and runs computational
// build, test, and lint checks extracted from workspace guides.
//
// Key types and entry points are [Sensor], [Verdict], [Registry],
// [NewRegistry], [CommandSensor], [NewCommandSensor],
// [NewRegistryFromGuide], [ExtractCommands], and [ExtractAllCommands].
//
// # Concurrency
//
// [Registry] is not synchronized for concurrent registration; register all
// sensors during setup before calling [Registry.Count] or [Registry.RunAll].
// [CommandSensor.Check] holds no mutable state across calls and runs each
// check in a context-bound subprocess.
//
// # Boundaries
//
// [CommandSensor] executes commands via "sh -c" in the configured workspace
// directory using [exec.CommandContext]. Guide inputs used to populate
// sensors must therefore come from trusted workspace sources.
package sensors
