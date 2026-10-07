// Package core is the extension kernel: one Proc per extension subprocess.
//
// It owns the PXB transport (frames over stdin/stdout), the handshake
// (Hello → HelloAck → Register*/Subscribe → Ready), request/response RPC with
// per-tool timeouts, and teardown (graceful Shutdown, then kill). It knows
// nothing about phi's tools, TUI or CLI: callers pass a HostSink to receive
// spontaneous frames and read registrations back through Tools/Commands.
//
// The counterpart of a cordis Fiber: a Proc goes from started, to registered,
// to closed, and every pending RPC fails once it is closed.
package core
