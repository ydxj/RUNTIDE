// Command helper is the fake project tooling used by the end-to-end scenario:
// a tiny HTTP server, a "compiler" that writes an artifact, and an editor.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"ok"}`) })
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "listen failed:", err)
			os.Exit(4)
		}
		fmt.Println("api listening on", l.Addr(), "token=abcdef0123456789abcdef")
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() { <-c; fmt.Println("shutting down"); os.Exit(0) }()
		go func() {
			for range time.Tick(time.Second) {
				fmt.Println("heartbeat")
			}
		}()
		_ = http.Serve(l, mux)
	case "write":
		_ = os.MkdirAll(filepath.Dir(os.Args[2]), 0o755)
		if err := os.WriteFile(os.Args[2], []byte(os.Args[3]+"\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("wrote", os.Args[2])
	case "append":
		f, err := os.OpenFile(os.Args[2], os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			os.Exit(1)
		}
		fmt.Fprintln(f, os.Args[3])
		f.Close()
		fmt.Println("appended")
	case "fail":
		fmt.Fprintln(os.Stderr, "FAIL: assertion failed")
		os.Exit(1)
	case "echo":
		fmt.Println(os.Args[2:])
	}
}
