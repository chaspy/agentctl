package cmd

import (
	"fmt"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	archiveID  string
	archiveAll bool
)

var archiveCmd = &cobra.Command{
	Use:   "archive [project]",
	Short: "Archive non-alive sessions to archive table",
	Long: `Moves non-alive (alive=false) sessions from the active sessions table to sessions_archive.
Without arguments, archives all sessions where alive=false regardless of status.
Use --id to archive a specific session by ID.
Use a project name argument to archive matching non-alive sessions.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runArchive,
}

func init() {
	archiveCmd.Flags().StringVar(&archiveID, "id", "", "Target a specific session by composite ID")
	archiveCmd.Flags().BoolVar(&archiveAll, "all", false, "Archive all non-alive sessions")
	rootCmd.AddCommand(archiveCmd)
}

func runArchive(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// If --id is given, move a single session to archive
	if archiveID != "" {
		if err := store.MoveToArchive(db, archiveID); err != nil {
			return fmt.Errorf("archive: %w", err)
		}
		fmt.Printf("Archived: %s\n", archiveID)
		return nil
	}

	// If --all or no arguments, archive all dead/error sessions
	if archiveAll || len(args) == 0 {
		count, err := store.ArchiveDeadSessions(db)
		if err != nil {
			return fmt.Errorf("archive: %w", err)
		}
		if count == 0 {
			fmt.Println("No non-alive sessions to archive")
		} else {
			fmt.Printf("Archived %d session(s)\n", count)
		}
		return nil
	}

	// Archive by project name match
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
		if s.Alive {
			continue
		}
		if err := store.MoveToArchive(db, s.ID); err != nil {
			fmt.Printf("warning: could not archive %s: %v\n", s.ID, err)
			continue
		}
		fmt.Printf("Archived: %s (%s)\n", s.Repository, s.ID)
		count++
	}

	if count == 0 {
		fmt.Println("No non-alive sessions to archive")
	} else {
		fmt.Printf("Archived %d session(s)\n", count)
	}

	return nil
}
