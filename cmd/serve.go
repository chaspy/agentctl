package cmd

import (
	"fmt"
	"net/http"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/chaspy/agentctl/internal/web"
	"github.com/spf13/cobra"
)

var servePort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the PWA dashboard web server",
	Long: `Starts a local HTTP server serving the Agents Manager dashboard.
The dashboard shows session status, tasks, actions, and rate limits.
Use with 'tailscale serve' for remote access from mobile devices.

Examples:
  agentctl serve
  agentctl serve --port 3000
  tailscale serve --bg 8080`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port to listen on")
}

func runServe(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	// Note: db is kept open for the lifetime of the server

	srv := web.New(db, syncSessionsToDB)
	addr := fmt.Sprintf(":%d", servePort)
	fmt.Printf("Agents Manager dashboard: http://localhost%s\n", addr)
	fmt.Printf("For mobile access: tailscale serve --bg %d\n", servePort)
	return http.ListenAndServe(addr, srv.Handler())
}
