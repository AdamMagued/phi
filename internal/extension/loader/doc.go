// Package loader is the extension config layer: it answers "which extensions
// exist", without spawning anything.
//
// An extension directory is an entry: <dir>/<id>/phi.yaml (Manifest) plus the
// binary it points at. Discover scans the user dir and the project dir, lets
// the project win on ID collisions, and reports unreadable entries as
// warnings. PHI_EXTENSIONS=off disables the whole layer.
package loader
