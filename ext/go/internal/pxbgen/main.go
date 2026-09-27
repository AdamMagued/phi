// Command pxbgen writes msg_gen.go from the `pxb` struct tags in schema.go.
//
// It reads and writes the package directory it runs in, so all of these work:
//
//	make generate              # repo root
//	go generate ./pxb           # from ext/go
//	go run ../internal/pxbgen   # from ext/go/pxb
//
// The generator is strict on purpose. A missing, duplicated, out-of-range, or
// unmapped tag fails the run instead of emitting a payload that cannot interop
// with the other SDK. `-check` reports a stale msg_gen.go without writing it.
//
// Generated encoders reserve the exact payload size, which is why the size
// helpers exist: one allocation per message, no append growth, no pooling.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
)

const (
	schemaFile = "schema.go"
	outputFile = "msg_gen.go"

	// maxFieldTag is the last tag an application message may use: 64-127 are
	// reserved for cross-cutting fields and 128+ for experiments, and both
	// ranges are protocol decisions rather than schema edits.
	maxFieldTag = 63
)

func main() {
	check := flag.Bool("check", false, "fail if "+outputFile+" is stale instead of rewriting it")
	flag.Parse()
	if err := run(*check); err != nil {
		log.Fatal(err)
	}
}

func run(check bool) error {
	msgs, err := parseSchema(schemaFile)
	if err != nil {
		return err
	}
	src, err := generate(msgs)
	if err != nil {
		return err
	}
	if !check {
		// Generated source is world-readable, like every other file in the tree.
		return os.WriteFile(outputFile, src, 0o644) //nolint:gosec // G306: not a secret
	}
	old, err := os.ReadFile(outputFile)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, src) {
		return fmt.Errorf("%s is stale: run make generate", outputFile)
	}
	return nil
}
