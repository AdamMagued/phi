// Package create scaffolds a PXB extension and compiles it.
//
// Materialize writes a throwaway Go module that imports the author SDK
// (ext/go/phi), builds its binary, and drops a phi.yaml beside it, so tests and
// examples can produce a real extension directory without shipping a fixture
// binary.
package create
