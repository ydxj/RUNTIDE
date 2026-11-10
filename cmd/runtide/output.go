package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

var (
	jsonOut bool
	noColor bool
)

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func table(headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	_ = tw.Flush()
}

func bytesStr(b uint64) string {
	const u = 1024
	if b < u {
		return fmt.Sprintf("%d B", b)
	}
	f, i := float64(b), 0
	for f >= u && i < 4 {
		f /= u
		i++
	}
	return fmt.Sprintf("%.1f %s", f, []string{"B", "KB", "MB", "GB", "TB"}[i])
}

func durStr(sec float64) string {
	d := time.Duration(sec * float64(time.Second))
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func color(code, s string) string {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func sev(level string) string {
	switch level {
	case "error":
		return color("31", "error")
	case "warning", "warn":
		return color("33", level)
	}
	return level
}
