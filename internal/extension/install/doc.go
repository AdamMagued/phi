// Package install owns the on-disk extension store: what lives in the
// extensions directory (typically ~/.phi/extensions), where each entry came
// from, and how to add, refresh or delete it.
//
// Install prefers the platform release archive (verified against the release
// checksum file) and falls back to a shallow git clone. Managed entries carry
// .phi-install.json (InstallMeta) recording the GitHub source; directories
// without it are treated as manual and are never overwritten or removed.
// Update and Remove re-resolve that recorded source.
//
// This layer is offline and CLI-facing: it never starts an extension.
package install
