package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func serverStatus(base string) error {
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := c.Get(base + "/healthz")
	if err != nil {
		return &exitError{exitNoEngine, "server unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	var h map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&h)
	if jsonOut {
		return printJSON(h)
	}
	fmt.Printf("%s: HTTP %d %v\n", base, resp.StatusCode, h)
	if resp.StatusCode != http.StatusOK {
		return &exitError{exitFailure, ""}
	}
	return nil
}
