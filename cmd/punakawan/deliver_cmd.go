package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/storage"
)

// deliverInputFile is the minimal --file schema for this phase: a flat
// list of reference strings, exactly what start_delivery/StartDelivery
// itself takes. Anything richer (per-reference metadata, structured
// answers to known questions) is deferred to a later phase.
type deliverInputFile struct {
	References []string `json:"references"`
}

// openDeliveryStoreForCLI opens the same shared, machine-wide storage
// kernel the start_delivery MCP tool uses (openDeliveryStore in
// internal/mcpserver/tools_delivery.go, via a.OpenStorage), by resolving
// storage.DBPath() directly instead of going through loadApp/app.Load.
//
// loadApp calls workspace.Discover, which requires a workspace.yaml or a
// .git directory somewhere above the current directory and fails
// outside one - but storage.DBPath() resolves to one fixed, per-OS-user
// location regardless of the current directory (internal/app.App.
// OpenStorage's own doc comment: "one database shared by every local
// project checkout on this machine"). Since `deliver`'s whole point is
// bootstrapping a brand new, not-yet-scoped orchestration - the thing
// AC6 calls "startup outside a project" - requiring an existing project
// checkout first would defeat it. There is no existing CLI-as-MCP-client
// precedent to follow instead: the daemon's own HTTP surface
// (internal/daemon/transport.go) only exposes /healthz and /readyz today,
// nothing that runs an MCP tool call, so building one from scratch here
// would be new daemon-transport surface, not reuse. Opening the storage
// kernel directly is exactly what every existing storage-backed
// command already does (a.OpenStorage's own implementation, mirrored
// here without the workspace-discovery step that command doesn't need).
func openDeliveryStoreForCLI(ctx context.Context) (*delivery.Store, error) {
	path, err := storage.DBPath()
	if err != nil {
		return nil, err
	}
	if err := storage.CheckLocation(path); err != nil {
		return nil, err
	}
	db, err := storage.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	return delivery.NewStore(db), nil
}

func newDeliverCmd() *cobra.Command {
	var references []string
	var urls []string
	var texts []string
	var file string

	cmd := &cobra.Command{
		Use:   "deliver [reference...]",
		Short: "Start a new delivery orchestration from one or more requirement references",
		Long: "Bootstraps one new delivery orchestration and captures each reference as a requirement source. " +
			"A reference this cannot confidently classify (not a recognizable Jira key, GitHub owner/repo#number, " +
			"or URL) becomes a pending question instead of failing the whole call; resolve it later via the " +
			"answer_delivery_question MCP tool. Works from any directory - it does not require an existing project checkout.",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs := append([]string{}, args...)
			refs = append(refs, references...)
			refs = append(refs, urls...)
			refs = append(refs, texts...)

			if file != "" {
				data, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("read --file %s: %w", file, err)
				}
				var in deliverInputFile
				if err := json.Unmarshal(data, &in); err != nil {
					return fmt.Errorf("parse --file %s: %w", file, err)
				}
				refs = append(refs, in.References...)
			}

			if len(refs) == 0 {
				return fmt.Errorf("deliver requires at least one reference (positional arg, --reference, --url, --text, or --file)")
			}

			ctx := cmd.Context()
			store, err := openDeliveryStoreForCLI(ctx)
			if err != nil {
				return err
			}

			view, err := store.StartDelivery(ctx, "", refs)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "orchestration: %s\n", view.Orchestration.Id)
			fmt.Fprintf(out, "next action: %s\n", view.NextAction)
			if len(view.PendingQuestions) > 0 {
				fmt.Fprintln(out, "pending questions:")
				for _, q := range view.PendingQuestions {
					fmt.Fprintf(out, "  - %s\n", q)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&references, "reference", nil, "a requirement reference (Jira key, GitHub owner/repo#number, or URL); repeatable")
	cmd.Flags().StringArrayVar(&urls, "url", nil, "a requirement source URL; repeatable")
	cmd.Flags().StringArrayVar(&texts, "text", nil, "a free-text requirement reference; repeatable")
	cmd.Flags().StringVar(&file, "file", "", `path to a JSON file of the form {"references": ["..."]}`)
	return cmd
}
