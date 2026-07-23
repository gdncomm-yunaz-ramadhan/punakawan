// Package artifact implements punakawan-artifact-review-plan-mutation-plan-v2.md's
// §4/§6/§7 foundations: immutable plan version storage, stable Markdown
// block-ID anchoring, and the durable review/comment/request/proposal
// records that later phases' review UI and revision workflow build on.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns content's revision hash in the "sha256:<hex>" form every
// schema in this plan uses (ArtifactReference.revision_hash,
// ArtifactComment.anchor.base_revision_hash, and so on).
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
