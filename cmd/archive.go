package cmd

import (
	"fmt"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	archiveUndo bool
	archiveID   string
)

var archiveCmd = &cobra.Command{
	Use:   "archive <project>",
	Short: "Archive a session (hide from default list)",
	Long:  "Archives a session by project name (fuzzy match). Archived sessions are hidden from 'list' but visible with 'list --all'. Use --undo to unarchive. Use --id to target a specific session ID.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runArchive,
}

func init() {
	archiveCmd.Flags().BoolVar(&archiveUndo, "undo", false, "Unarchive (restore) sessions instead")
	archiveCmd.Flags().StringVar(&archiveID, "id", "", "Target a specific session by composite ID")
	rootCmd.AddCommand(archiveCmd)
}

func runArchive(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// If --id is given, operate on a single session
	if archiveID != "" {
		if archiveUndo {
			if err := store.UnarchiveSession(db, archiveID); err != nil {
				return fmt.Errorf("unarchive: %w", err)
			}
			fmt.Printf("Unarchived: %s\n", archiveID)
		} else {
			if err := store.ArchiveSession(db, archiveID); err != nil {
				return fmt.Errorf("archive: %w", err)
			}
			fmt.Printf("Archived: %s\n", archiveID)
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("project name or --id required")
	}
	query := args[0]

	sessions, err := store.FindSessionByRepository(db, query)
	if err != nil {
		return fmt.Errorf("searching sessions: %w", err)
	}

	if len(sessions) == 0 {
		return fmt.Errorf("no sessions found matching %q", query)
	}

	var count int
	for _, s := range sessions {
		if archiveUndo {
			if !s.Archived {
				continue
			}
			if err := store.UnarchiveSession(db, s.ID); err != nil {
				fmt.Printf("warning: could not unarchive %s: %v\n", s.ID, err)
				continue
			}
			fmt.Printf("Unarchived: %s (%s)\n", s.Repository, s.ID)
		} else {
			if s.Archived {
				continue
			}
			if err := store.ArchiveSession(db, s.ID); err != nil {
				fmt.Printf("warning: could not archive %s: %v\n", s.ID, err)
				continue
			}
			fmt.Printf("Archived: %s (%s)\n", s.Repository, s.ID)
		}
		count++
	}

	action := "Archived"
	if archiveUndo {
		action = "Unarchived"
	}
	if count == 0 {
		fmt.Printf("No sessions to %s\n", action)
	} else {
		fmt.Printf("%s %d session(s)\n", action, count)
	}

	return nil
}
