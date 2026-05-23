// Package runtime is Kleiber's Runtime Monitor: it connects to Delve via
// DAP for debugging, parses pprof profiles, and (in later milestones) will
// consume OpenTelemetry traces and eBPF probe data. It exposes the
// resulting data to the UI as structured events suitable for inline
// rendering on source code.
//
// The intended public API is sketched in docs/architecture/components.md
// (section "Runtime Monitor"). Debugger lands in Milestone 2 (Phase 5),
// profiler in Milestone 4 (Phase 7).
package runtime
