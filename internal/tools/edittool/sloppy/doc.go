// Package sloppy parses the "sloppy" anchored edit format: tolerant
// FIND/PUT/AFTER payloads addressed at file sections. It covers the front
// half of the pipeline — lexing and parsing into an operation IR — and stops
// before matching and applying, which belong to an apply stage.
//
// # Format
//
//	*** SM:EDIT relative/path.ts [all]
//	*** SM:FIND
//	<current file text>
//	*** SM:PUT
//	<replacement text>
//
// A "*** SM:EDIT" header opens a file section; a bare "*** SM:EDIT" continues
// the current one. Paths with spaces or a trailing "all" word may be
// JSON-quoted: *** SM:EDIT "my file.ts" all. Each edit is a "*** SM:FIND"
// body followed by either "*** SM:PUT" (replace the whole match; an empty
// body deletes it) or "*** SM:AFTER" (keep the match and insert the body
// after it). Bodies are raw text running to the next header.
//
// Inside a FIND body:
//
//   - "…" is a gap that captures file text. A gap with content after it on
//     its line stays line-bounded; a gap at line end may span lines.
//   - "⟪old│new⟫" selects old text and rewrites it to new. A FIND body
//     carrying selections needs no action header.
//   - "…" at the body edges captures nothing and is dropped.
//
// # Tolerance
//
// Foreign patch envelopes ("*** Begin Patch", "*** Update File:", "*** End
// Patch" and split variants), markdown fences wrapping the payload, and
// read-output chrome ("[Showing lines 1-20 of 80]", "[3 more lines in a.ts.
// use read to continue]", elided-line markers) are stripped. Body lines all
// carrying read-output line numbers ("12| code") are unnumbered. Content
// inside an "*** SM:AFTER" body passes through untouched.
//
// # Deviations
//
// A selection without a "│" divider, a second action header without an
// intervening FIND, and an "*** SM:AFTER" without a FIND anchor are errors;
// the reference engine silently drops or reinterprets those. "…" replay
// inside PUT bodies is left to the apply stage.
//
// # Performance
//
// Lexing and pattern scanning are single passes with no regular expressions:
// header recognition is byte-prefix matching, literal runs jump between
// marker lead bytes with strings.IndexByte, and tokens reference the input as
// substrings, so parsing allocates only the token slices and result
// containers. See BenchmarkParse.
package sloppy
