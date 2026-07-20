package tools

import (
	"context"
	"time"
)

// Search runs ripgrep (rg) with the given argv (pattern and any flags) in
// dir, through the same supervised execution path as any other tool, per
// §2.1 and §14.1 ("ripgrep for repository search").
func (s *Supervisor) Search(ctx context.Context, dir string, args []string, timeout time.Duration) (*Result, error) {
	return s.Run(ctx, Spec{Name: "rg", Args: args, Dir: dir, Timeout: timeout})
}
