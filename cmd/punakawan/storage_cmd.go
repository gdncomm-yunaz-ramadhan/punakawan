package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/doltimport"
)

// newStorageCmd is the `punakawan storage ...` surface for kernel-level data
// operations. Its one subcommand today, `migrate --to sqlite`, performs the
// final one-way import of a project's legacy Dolt knowledge into the live
// shared SQLite kernel (punokawan-14yn.19).
func newStorageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Inspect and migrate the shared SQLite storage kernel",
	}
	cmd.AddCommand(newStorageMigrateCmd())
	return cmd
}

func newStorageMigrateCmd() *cobra.Command {
	var (
		to        string
		dryRun    bool
		apply     bool
		workspace string
	)
	cmd := &cobra.Command{
		Use:   "migrate --to sqlite [--dry-run|--apply]",
		Short: "Import a project's legacy Dolt knowledge into the live SQLite kernel",
		Long: "Imports a project's durable knowledge from its legacy Dolt store (hub-backed\n" +
			"or per-project) into the live shared SQLite kernel. Knowledge is the only\n" +
			"subsystem that ever lived in Dolt. Defaults to a dry run that mutates nothing;\n" +
			"pass --apply to perform the import. The import is idempotent: re-running it\n" +
			"upserts already-imported records to the identical state.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if to != "sqlite" {
				return fmt.Errorf("--to must be 'sqlite' (the only supported migration target); Dolt is no longer a selectable runtime")
			}
			if dryRun && apply {
				return fmt.Errorf("--dry-run and --apply are mutually exclusive")
			}
			// Default to a dry run when neither flag is given: apply must be
			// explicitly requested.
			doApply := apply

			startDir := workspace
			if startDir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				startDir = cwd
			}
			a, err := app.Load(startDir)
			if err != nil {
				return err
			}
			defer a.Close()

			src, err := doltimport.Discover(a.Workspace.Root)
			if err != nil {
				return err
			}

			ctx := context.Background()
			db, err := a.OpenStorage(ctx)
			if err != nil {
				return err
			}

			rep, err := doltimport.Run(ctx, db, a.Workspace.ID, src, doApply)
			if err != nil {
				return err
			}
			printReport(cmd, rep)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "sqlite", "migration target (only 'sqlite' is supported)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "inventory the import without mutating anything (the default)")
	cmd.Flags().BoolVar(&apply, "apply", false, "perform the import into the live kernel")
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace directory to migrate (default: current directory)")
	return cmd
}

func printReport(cmd *cobra.Command, rep *doltimport.Report) {
	out := cmd.OutOrStdout()
	mode := "DRY RUN (no changes made)"
	if rep.Applied {
		mode = "APPLIED"
	}
	fmt.Fprintf(out, "Dolt -> SQLite knowledge import [%s]\n", mode)
	fmt.Fprintf(out, "  destination project: %s\n", rep.DestProjectID)

	if rep.Kind == doltimport.KindNone {
		fmt.Fprintln(out, "  source: none found (no hub pointer, no legacy .punakawan/knowledge/.dolt)")
		fmt.Fprintln(out, "  nothing to import.")
		return
	}

	fmt.Fprintf(out, "  source: %s (%s), db=%s\n", rep.Kind, rep.SourceDir, rep.SourceDB)
	fmt.Fprintf(out, "  source rows: %d records, %d relations\n", rep.SourceRecordCount, rep.SourceRelationCount)

	if rep.Applied {
		fmt.Fprintf(out, "  imported: %d records, %d relations\n", rep.RecordsImported, rep.RelationsImported)
		fmt.Fprintf(out, "  integrity check: %v\n", rep.IntegrityOK)
	} else {
		importable := rep.SourceRecordCount - len(rep.Skipped)
		fmt.Fprintf(out, "  would import: %d records\n", importable)
	}

	if len(rep.Overwritten) > 0 {
		verb := "would overwrite"
		if rep.Applied {
			verb = "overwrote"
		}
		fmt.Fprintf(out, "  %s %d already-present record(s):\n", verb, len(rep.Overwritten))
		for _, id := range rep.Overwritten {
			fmt.Fprintf(out, "    - %s\n", id)
		}
	}

	if len(rep.Skipped) > 0 {
		fmt.Fprintf(out, "  skipped %d obsolete/malformed record(s):\n", len(rep.Skipped))
		for _, s := range rep.Skipped {
			fmt.Fprintf(out, "    - %s: %s\n", s.ID, s.Reason)
		}
	}
	if !rep.CompletedAt.IsZero() && rep.Applied {
		fmt.Fprintf(out, "  completed at: %s\n", rep.CompletedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
}
