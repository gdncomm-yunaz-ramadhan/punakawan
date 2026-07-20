package tools

import (
	"context"
	"time"
)

// RunViaRTK executes argv through the user's rtk CLI for token-compressed
// model-facing output, per §11.4 ("RTK-compressed model-facing output") and
// §2.1 ("RTK for compact command output"). rtk transparently runs the
// wrapped command and filters its output; callers that need the raw,
// uncompressed result should call Supervisor.Run directly instead.
func (s *Supervisor) RunViaRTK(ctx context.Context, dir string, argv []string, timeout time.Duration) (*Result, error) {
	return s.Run(ctx, Spec{Name: "rtk", Args: argv, Dir: dir, Timeout: timeout})
}

// RTKAvailable reports whether the rtk binary is installed and responsive.
// Callers should fall back to running a command directly via Supervisor.Run
// when this returns false.
func (s *Supervisor) RTKAvailable(ctx context.Context, dir string) bool {
	res, err := s.Run(ctx, Spec{Name: "rtk", Args: []string{"--version"}, Dir: dir, Timeout: 5 * time.Second})
	return err == nil && res.ExitCode == 0
}
