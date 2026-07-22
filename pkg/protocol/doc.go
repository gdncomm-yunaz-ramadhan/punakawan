// Package protocol contains Go types generated from the canonical JSON
// Schemas in protocol/*.schema.json. Do not edit types_generated.go by hand;
// run `go generate ./...` (or `make generate`) after changing a schema.
package protocol

//go:generate go tool go-jsonschema -p protocol -o types_generated.go --struct-name-from-title ../../protocol/adapter.schema.json ../../protocol/event.schema.json ../../protocol/workflow.schema.json ../../protocol/knowledge.schema.json ../../protocol/task.schema.json ../../protocol/evidence.schema.json ../../protocol/approval.schema.json ../../protocol/flow.schema.json ../../protocol/capsule.schema.json
