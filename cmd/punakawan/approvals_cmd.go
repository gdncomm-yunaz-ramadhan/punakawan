package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// newApprovalsCmd is the human-facing surface for §16's approval gate: a
// role (Semar/Gareng/Petruk/Bagong) can only request an approval-gated
// action, never grant it. Deliberately a CLI, not an MCP tool - exposing
// approve/deny as an MCP tool would let the same connected AI client that
// requested an action also grant it to itself, defeating the human-in-the-
// loop point of the gate (§16.2's approved_by is documented as "user", not
// the requesting role).
func newApprovalsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approvals",
		Short: "List and resolve pending approval requests (§16)",
	}
	cmd.AddCommand(newApprovalsListCmd())
	cmd.AddCommand(newApprovalsApproveCmd())
	cmd.AddCommand(newApprovalsDenyCmd())
	return cmd
}

func newApprovalsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending approval requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			defer a.Close()

			pending, err := a.Approvals.Pending()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(pending) == 0 {
				fmt.Fprintln(out, "no pending approval requests")
				return nil
			}
			for _, rec := range pending {
				fmt.Fprintf(out, "%s\n", rec.Id)
				fmt.Fprintf(out, "  run_id:       %s\n", rec.RunId)
				fmt.Fprintf(out, "  operation:    %s\n", rec.Operation)
				if rec.Target != nil {
					fmt.Fprintf(out, "  target:       %s\n", *rec.Target)
				}
				if rec.Reason != nil {
					fmt.Fprintf(out, "  reason:       %s\n", *rec.Reason)
				}
				fmt.Fprintf(out, "  requested_by: %s\n", rec.RequestedBy)
				fmt.Fprintf(out, "  created_at:   %s\n", rec.CreatedAt)
			}
			return nil
		},
	}
}

func newApprovalsApproveCmd() *cobra.Command {
	var approvedBy string
	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a pending approval request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return resolveApproval(cmd, args[0], protocol.ApprovalRecordStatusApproved, approvedBy)
		},
	}
	cmd.Flags().StringVar(&approvedBy, "by", "", "identifier of the human granting approval (required)")
	cmd.MarkFlagRequired("by")
	return cmd
}

func newApprovalsDenyCmd() *cobra.Command {
	var approvedBy string
	cmd := &cobra.Command{
		Use:   "deny <id>",
		Short: "Deny a pending approval request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return resolveApproval(cmd, args[0], protocol.ApprovalRecordStatusDenied, approvedBy)
		},
	}
	cmd.Flags().StringVar(&approvedBy, "by", "", "identifier of the human denying the request (required)")
	cmd.MarkFlagRequired("by")
	return cmd
}

func resolveApproval(cmd *cobra.Command, id string, status protocol.ApprovalRecordStatus, approvedBy string) error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	defer a.Close()

	if err := a.Approvals.Resolve(id, status, approvedBy); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", id, status)
	return nil
}
