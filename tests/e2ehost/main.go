// Command e2ehost serves the built frontend in a normal browser and bridges
// the Wails binding (window.go.desktop.App) to the REAL engine, so Playwright
// can drive the actual UI against real processes, ports and logs without the
// native shell. It is test tooling: it is never shipped or installed.
//
//	go run ./tests/e2ehost -port 45900
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/api"
	"github.com/runtide/runtide/internal/app"
)

const shim = `
(function () {
  const listeners = {};
  window.runtime = { EventsOn(name, cb) { (listeners[name] = listeners[name] || []).push(cb); return () => {}; } };
  const call = async (method, params) => {
    const r = await fetch('/__call', { method: 'POST', body: JSON.stringify({ method, params: params ? JSON.parse(params) : null }) });
    const j = await r.json();
    if (j.error) throw new Error(j.error);
    return j.result;
  };
  window.go = { desktop: { App: {
    Call: call,
    Info: async () => ({ mode: 'e2e', version: 'e2e' }),
    PickDirectory: async () => window.__pickDir || '',
    PickFile: async () => window.__pickFile || '',
    PickSaveFile: async () => window.__pickSave || '',
    ReadTextFile: async () => window.__fileText || '',
    WriteTextFile: async () => {},
    Quit: async () => {},
  } } };
  const es = new EventSource('/__events');
  es.onmessage = (m) => {
    const ev = JSON.parse(m.data);
    if (ev.topic === 'log') { (listeners['log-batch'] || []).forEach((cb) => cb([ev.data])); }
    else if (ev.topic === 'exec.output') { (listeners['exec-output-batch'] || []).forEach((cb) => cb([ev.data])); }
    else { (listeners['runtide'] || []).forEach((cb) => cb(ev.topic, ev.data)); }
  };
})();
`

func main() {
	port := flag.Int("port", 45900, "listen port (loopback)")
	dist := flag.String("dist", "frontend/dist", "built frontend")
	flag.Parse()

	eng, err := app.New(app.Options{})
	if err != nil {
		log.Fatal(err)
	}
	// Publish the loopback API as the desktop app would, so the CLI can talk to this engine too.
	srv, err := api.Serve(eng, "desktop")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__call", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err := json.Unmarshal(b, &req); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		res, err := eng.Dispatch(r.Context(), req.Method, req.Params)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		out, _ := json.Marshal(res)
		// The Wails binding returns a JSON string; the UI parses it.
		_ = json.NewEncoder(w).Encode(map[string]any{"result": string(out)})
	})
	mux.HandleFunc("/__events", func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		ch, cancel := eng.Subscribe()
		defer cancel()
		fmt.Fprint(w, ": hi\n\n")
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev := <-ch:
				b, _ := json.Marshal(ev)
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			}
		}
	})
	fs := http.FileServer(http.Dir(*dist))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			b, err := os.ReadFile(filepath.Join(*dist, "index.html"))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			html := strings.Replace(string(b), "<head>", "<head><script>"+shim+"</script>", 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		}
		fs.ServeHTTP(w, r)
	})
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("e2ehost ready", ln.Addr())
	go func() {
		_ = http.Serve(ln, mux)
	}()
	// Run until the parent kills us; shut the engine down cleanly on stdin close.
	_, _ = io.Copy(io.Discard, os.Stdin)
	srv.Close()
	_ = eng.Close(true)
	time.Sleep(100 * time.Millisecond)
}
