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

	"github.com/spf13/cobra"
)

// pushCmd uploads a receipt to a self-hosted RUNTIDE server. The token comes
// from RUNTIDE_SERVER_TOKEN (never a flag, so it stays out of shell history).
func pushCmd() *cobra.Command {
	var server, project string
	c := &cobra.Command{Use: "push <file>", Short: "Upload a receipt to a RUNTIDE server (token in RUNTIDE_SERVER_TOKEN)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token := os.Getenv("RUNTIDE_SERVER_TOKEN")
			if token == "" {
				return &exitError{exitUsage, "set RUNTIDE_SERVER_TOKEN to an API token created on the server (POST /api/v1/tokens)"}
			}
			if server == "" {
				return &exitError{exitUsage, "--server is required"}
			}
			base := strings.TrimRight(server, "/")
			if !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://127.0.0.1") && !strings.HasPrefix(base, "http://localhost") && !strings.HasPrefix(base, "http://[::1]") {
				return &exitError{exitUsage, "refusing to send a token over plain HTTP to a non-loopback server; use https://"}
			}
			var verr struct{ Valid bool }
			var v struct {
				Valid    bool
				Problems []string
			}
			if err := callAny("receipts.verify", map[string]string{"ref": args[0]}, &v); err != nil {
				return err
			}
			_ = verr
			if !v.Valid {
				return fmt.Errorf("refusing to upload an invalid receipt: %s", strings.Join(v.Problems, "; "))
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			cl := &http.Client{Timeout: 30 * time.Minute, Transport: &http.Transport{Proxy: nil}}
			do := func(method, url string, body []byte, ct string) ([]byte, int, error) {
				req, _ := http.NewRequest(method, base+url, bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				if ct != "" {
					req.Header.Set("Content-Type", ct)
				}
				resp, err := cl.Do(req)
				if err != nil {
					return nil, 0, err
				}
				defer resp.Body.Close()
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
				return b, resp.StatusCode, nil
			}
			b, code, err := do("GET", "/api/v1/projects", nil, "")
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("server rejected the token: HTTP %d", code)
			}
			var ps []struct{ ID, Name string }
			_ = json.Unmarshal(b, &ps)
			pid := ""
			for _, p := range ps {
				if p.ID == project || strings.EqualFold(p.Name, project) {
					pid = p.ID
				}
			}
			if pid == "" {
				return fmt.Errorf("project %q was not found on the server (or you have no access)", project)
			}
			b, code, err = do("POST", "/api/v1/projects/"+pid+"/receipts", data, "application/zip")
			if err != nil {
				return err
			}
			if code != 200 && code != 201 {
				return fmt.Errorf("server refused the receipt: HTTP %d %s", code, strings.TrimSpace(string(b)))
			}
			if jsonOut {
				fmt.Println(string(b))
				return nil
			}
			var r struct{ ID, Status, Verification string }
			_ = json.Unmarshal(b, &r)
			fmt.Printf("Uploaded: execution %s stored on %s (status %s, verification %s)\n", r.ID, base, r.Status, dash(r.Verification))
			return nil
		}}
	c.Flags().StringVar(&server, "server", "", "server base URL, e.g. https://runtide.example.com")
	c.Flags().StringVar(&project, "project", "", "project name or id on the server")
	_ = c.MarkFlagRequired("project")
	return c
}
