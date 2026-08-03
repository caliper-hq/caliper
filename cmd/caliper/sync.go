package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pranavkakde/caliper/internal/storage"
	"github.com/spf13/cobra"
)

var (
	controlPlaneURL string
	projectID       string
	apiToken        string
)

var syncCmd = &cobra.Command{
	Use:          "sync",
	Short:        "Upload unsynced local history to a control plane",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		url := firstNonEmpty(controlPlaneURL, os.Getenv("CALIPER_CONTROL_PLANE_URL"))
		project := firstNonEmpty(projectID, os.Getenv("CALIPER_PROJECT_ID"))
		token := firstNonEmpty(apiToken, os.Getenv("CALIPER_API_TOKEN"))
		if url == "" || project == "" || token == "" {
			return fmt.Errorf("sync requires --url, --project-id, and --token (or CALIPER_CONTROL_PLANE_URL, CALIPER_PROJECT_ID, CALIPER_API_TOKEN)")
		}
		return runSync(historyDir, url, project, token)
	},
}

func init() {
	syncCmd.Flags().StringVar(&controlPlaneURL, "url", "", "control plane base URL")
	syncCmd.Flags().StringVar(&projectID, "project-id", "", "control plane project ID")
	syncCmd.Flags().StringVar(&apiToken, "token", "", "project API key (prefer CALIPER_API_TOKEN)")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func runSync(historyDir, baseURL, project, token string) error {
	store := storage.NewLocalStorage(historyDir)
	runs, err := store.LoadUnsynced()
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Fprintln(os.Stdout, "No unsynced runs.")
		return nil
	}
	payload := struct {
		Runs []storage.RunRecord `json:"runs"`
	}{Runs: make([]storage.RunRecord, len(runs))}
	for i, run := range runs {
		payload.Runs[i] = run.Record
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sync: encode runs: %w", err)
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/projects/" + project + "/runs"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sync: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sync: post runs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sync: server returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if err := store.MarkSynced(runs); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Synced %d run(s).\n", len(runs))
	return nil
}
