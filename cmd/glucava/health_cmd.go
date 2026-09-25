package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// defaultHealthURL is where a container's own server listens.
const defaultHealthURL = "http://127.0.0.1:8090/health"

// checkHealth asks a running server's /health endpoint and returns an error
// unless it answers 200 with status "ok".
func checkHealth(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: %s answered %d", url, resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4096)).Decode(&body); err != nil || body.Status != "ok" {
		return fmt.Errorf("health: %s did not report ok", url)
	}
	return nil
}

// runHealthcheck runs "glucava healthcheck" without starting PocketBase, which
// would try to create pb_data in the working directory and fail in a
// read-only container.
func runHealthcheck() {
	root := &cobra.Command{Use: "glucava", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(healthCommand())
	root.SetArgs(os.Args[1:])
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// healthCommand is "glucava healthcheck", used by the container HEALTHCHECK
// (the slim image has no curl). Exit status 0 means healthy.
func healthCommand() *cobra.Command {
	var url string
	cmd := &cobra.Command{
		Use: "healthcheck", Short: "Exit 0 if the local server answers /health (for container health checks)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if url == "" {
				url = os.Getenv("GLUCAVA_HEALTH_URL")
			}
			if url == "" {
				url = defaultHealthURL
			}
			return checkHealth(cmd.Context(), url)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "health URL (default $GLUCAVA_HEALTH_URL, else "+defaultHealthURL+")")
	return cmd
}
