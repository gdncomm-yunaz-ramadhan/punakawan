// Package protocol embeds this directory's own canonical JSON Schema
// files read-only, so code elsewhere in the module (e.g. internal/agent,
// which checks a role manifest's output_schema against
// knowledge.schema.json's own field list) can inspect a schema's
// structure without a working-directory-relative disk read. Go's
// //go:embed cannot reach these files from another package's directory
// (patterns may not contain ".." or an absolute path), hence this small
// embed living beside the schemas themselves. This is unrelated to
// pkg/protocol, which holds Go types generated FROM these same schemas
// at `go generate` time via go-jsonschema - this package only exposes
// the raw schema bytes.
package protocol

import "embed"

//go:embed knowledge.schema.json
var FS embed.FS
