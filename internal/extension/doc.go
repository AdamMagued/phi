// Package extension is the host side of the PXB extension mechanism: it
// discovers extension directories, spawns each binary, and exposes what the
// extensions registered (tools, slash commands, event intercepts) to the rest
// of phi.
//
// # Layers
//
// The package is split into layers with a one-way dependency graph, so an
// extension feature lands in exactly one place:
//
//	extension/          host surface: Runner, Load, BusUI        — what agent/tui/cmd import
//	├── create/         scaffolding: Materialize (dev/test helper)
//	├── install/        distribution: ~/.phi/extensions contents — install, update, remove, list
//	├── core/           kernel: one extension subprocess         — spawn, handshake, RPC, dispose
//	└── loader/         config: phi.yaml manifest + discovery    — which extensions exist
//
// Arrows point down only: extension → core → loader, and install → loader.
// Nothing imports upward, and the kernel knows nothing about phi's tools, TUI
// or CLI. install and create are leaves that never reach the host surface;
// cmd is the only consumer of install.
//
// See doc/extensions.md for the protocol and the author-facing SDK.
package extension
