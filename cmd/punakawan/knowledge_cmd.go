package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newKnowledgeCmd is §11's `punakawan knowledge recipe ...` CLI surface:
// power-user shortcuts for inspecting and adjusting a retrieval recipe's
// lifecycle without going through the panel/chat guided-discovery flow.
// `update` is deliberately thin here (see recipeUpdateCmd's own doc
// comment) - actually walking a human through corrected constraints is
// discovery/panel UI territory (§8), not this CLI's job.
func newKnowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Inspect and manage durable knowledge records",
	}
	cmd.AddCommand(newRecipeCmd())
	cmd.AddCommand(newKnowledgeResetCmd())
	return cmd
}

// newKnowledgeResetCmd bulk-deletes every knowledge record matching a given
// project/repository/module scope - a deliberate CLI-only action rather than
// an MCP tool: an unbounded scope wipe is destructive enough (and rare
// enough) that it belongs behind an operator explicitly running a command,
// not behind a tool any agent session could reach for. Mirrors the removed
// reset_project_knowledge MCP tool's exact semantics: dry run by default,
// requires at least one scope field, and reports the Dolt commit hash so a
// confirmed delete can be read or restored via delete_knowledge's own
// AS OF/checkout instructions.
func newKnowledgeResetCmd() *cobra.Command {
	var project, repository, module string
	var confirm bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Bulk-delete every knowledge record matching a project/repository/module scope",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" && repository == "" && module == "" {
				return fmt.Errorf("reset requires at least one of --project/--repository/--module - an empty scope would match every record in the store")
			}

			a, err := loadApp()
			if err != nil {
				return err
			}
			defer a.Close()

			store, err := a.OpenKnowledge()
			if err != nil {
				return err
			}
			records, err := store.AllWithUpdatedAt()
			if err != nil {
				return fmt.Errorf("list knowledge records: %w", err)
			}

			var matched []string
			for _, rec := range records {
				if knowledgeScopeMatches(rec.Record.Scope, project, repository, module) {
					matched = append(matched, rec.Record.Id)
				}
			}

			out := cmd.OutOrStdout()
			if !confirm {
				fmt.Fprintf(out, "dry run: %d record(s) match this scope; re-run with --confirm to delete them\n", len(matched))
				for _, id := range matched {
					fmt.Fprintf(out, "  %s\n", id)
				}
				return nil
			}

			ix, err := a.OpenSearchIndex()
			if err != nil {
				return err
			}
			for _, id := range matched {
				if err := store.Delete(id); err != nil {
					return fmt.Errorf("delete knowledge record %q: %w", id, err)
				}
				if err := ix.DeleteRecord(id); err != nil {
					return fmt.Errorf("remove %q from search index: %w", id, err)
				}
			}

			var commitHash string
			if len(matched) > 0 {
				commitHash, err = store.CommitWorkingSet(fmt.Sprintf("punakawan: knowledge reset project=%q repository=%q module=%q (%d records)", project, repository, module, len(matched)))
				if err != nil {
					return fmt.Errorf("commit reset: %w", err)
				}
			}
			fmt.Fprintf(out, "deleted %d record(s)\n", len(matched))
			if commitHash != "" {
				fmt.Fprintf(out, "commit: %s\n", commitHash)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "match records scoped to this project")
	cmd.Flags().StringVar(&repository, "repository", "", "match records scoped to this repository")
	cmd.Flags().StringVar(&module, "module", "", "match records scoped to this module")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete the matched records; without this, only a dry-run preview is printed")
	return cmd
}

// knowledgeScopeMatches reports whether scope satisfies every non-empty
// requested field; unset requested fields are not filtered on.
func knowledgeScopeMatches(scope *protocol.KnowledgeRecordScope, project, repository, module string) bool {
	if project != "" && (scope == nil || scope.Project == nil || *scope.Project != project) {
		return false
	}
	if repository != "" && (scope == nil || scope.Repository == nil || *scope.Repository != repository) {
		return false
	}
	if module != "" && (scope == nil || scope.Module == nil || *scope.Module != module) {
		return false
	}
	return true
}

func newRecipeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recipe",
		Short: "Inspect and manage retrieval recipes (§4-§12)",
	}
	cmd.AddCommand(newRecipeListCmd())
	cmd.AddCommand(newRecipeShowCmd())
	cmd.AddCommand(newRecipeExplainCmd())
	cmd.AddCommand(newRecipeValidateCmd())
	cmd.AddCommand(newRecipeUpdateCmd())
	cmd.AddCommand(newRecipeDisputeCmd())
	cmd.AddCommand(newRecipeSupersedeCmd())
	return cmd
}

func openRecipeRepo() (*recipe.Repository, func() error, error) {
	a, err := loadApp()
	if err != nil {
		return nil, nil, err
	}
	store, err := a.OpenKnowledge()
	if err != nil {
		return nil, nil, err
	}
	return &recipe.Repository{Store: store}, a.Close, nil
}

func newRecipeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every retrieval recipe, in every lifecycle state",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeApp, err := openRecipeRepo()
			if err != nil {
				return err
			}
			defer closeApp()

			recs, err := repo.Store.ListByType(protocol.KnowledgeRecordTypeRetrievalRecipe)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(recs) == 0 {
				fmt.Fprintln(out, "no retrieval recipes found")
				return nil
			}
			for _, rec := range recs {
				version := 1
				if rec.RetrievalRecipe != nil && rec.RetrievalRecipe.RecipeVersion != nil {
					version = *rec.RetrievalRecipe.RecipeVersion
				}
				fmt.Fprintf(out, "%s\n", rec.Id)
				fmt.Fprintf(out, "  title:      %s\n", rec.Title)
				fmt.Fprintf(out, "  capability: %s\n", rec.RetrievalRecipe.Capability)
				fmt.Fprintf(out, "  intent:     %s\n", rec.RetrievalRecipe.Intent)
				fmt.Fprintf(out, "  state:      %s\n", rec.Validity.State)
				fmt.Fprintf(out, "  version:    %d\n", version)
			}
			return nil
		},
	}
}

func newRecipeShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a retrieval recipe's full record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeApp, err := openRecipeRepo()
			if err != nil {
				return err
			}
			defer closeApp()

			rec, err := repo.Store.Get(args[0])
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(rec, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
}

func newRecipeExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <id>",
		Short: "Compile a recipe and show its resolved JQL, ordering, and clause explanations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeApp, err := openRecipeRepo()
			if err != nil {
				return err
			}
			defer closeApp()

			rec, err := repo.Store.Get(args[0])
			if err != nil {
				return err
			}
			cq, err := recipe.CompileOnlyValidate(context.Background(), recipe.NewCompiler(nil), rec.RetrievalRecipe, nil)
			if err != nil {
				return fmt.Errorf("compile: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Compiled JQL\n  %s\n", cq.JQL)
			if cq.OrderBy != "" {
				fmt.Fprintf(out, "  ORDER BY %s\n", cq.OrderBy)
			}
			if len(cq.Explanations) > 0 {
				fmt.Fprintln(out, "\nClause explanations")
				for _, e := range cq.Explanations {
					fmt.Fprintf(out, "  %s\n", e)
				}
			}
			if len(cq.Warnings) > 0 {
				fmt.Fprintln(out, "\nWarnings")
				for _, w := range cq.Warnings {
					fmt.Fprintf(out, "  %s\n", w)
				}
			}
			return nil
		},
	}
}

func newRecipeValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <id>",
		Short: "Recompile a recipe's selector without changing it (§11's compile-time half of validate)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeApp, err := openRecipeRepo()
			if err != nil {
				return err
			}
			defer closeApp()

			rec, err := repo.Store.Get(args[0])
			if err != nil {
				return err
			}
			cq, err := recipe.CompileOnlyValidate(context.Background(), recipe.NewCompiler(nil), rec.RetrievalRecipe, nil)
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "compile failed: %v\n", err)
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "compiles cleanly: %s\n", cq.JQL)
			fmt.Fprintln(cmd.OutOrStdout(), "note: no provider connection is configured in this phase, so this only checks schema/field/operator/resolver validity (§9 steps 1-5), not a live dry run (steps 6-9).")
			return nil
		},
	}
}

// newRecipeUpdateCmd implements §11's `update` command's non-interactive
// half: it moves the recipe to validating (so nothing auto-reuses it
// mid-edit) and prints the current spec as the discovery baseline.
// Actually walking a human through corrected constraints and re-running
// Validator/DiscoverySession against their answers is discovery/panel UI
// territory (§8) - this command only performs the state transition and
// hands back the baseline, rather than faking an interactive loop a
// plain CLI command cannot really offer.
func newRecipeUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update <id>",
		Short: "Start an update: move to validating and print the current spec as a discovery baseline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeApp, err := openRecipeRepo()
			if err != nil {
				return err
			}
			defer closeApp()

			baseline, err := repo.BeginUpdate(args[0])
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(baseline, "", "  ")
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "moved to validating; it will not be auto-reused until it is re-verified.")
			fmt.Fprintln(out, "baseline for a corrected recipe (run guided discovery, then Repository.CreateVersion the result):")
			fmt.Fprintln(out, string(data))
			return nil
		},
	}
}

func newRecipeDisputeCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "dispute <id>",
		Short: "Mark a recipe disputed, preventing automatic reuse",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeApp, err := openRecipeRepo()
			if err != nil {
				return err
			}
			defer closeApp()

			if err := repo.Dispute(args[0], reason); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: disputed\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why this recipe is disputed")
	cmd.MarkFlagRequired("reason")
	return cmd
}

func newRecipeSupersedeCmd() *cobra.Command {
	var with string
	cmd := &cobra.Command{
		Use:   "supersede <id>",
		Short: "Point an old recipe at an already-accepted replacement",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeApp, err := openRecipeRepo()
			if err != nil {
				return err
			}
			defer closeApp()

			if err := repo.Supersede(args[0], with); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: superseded by %s\n", args[0], with)
			return nil
		},
	}
	cmd.Flags().StringVar(&with, "with", "", "id of the replacement recipe (required)")
	cmd.MarkFlagRequired("with")
	return cmd
}
