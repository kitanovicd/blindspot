package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed ui/report.html
var reportTemplate string

func writeReport(file string, run *Run) error {
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	// encoding/json escapes HTML delimiters, preventing source code from closing the script tag.
	html := strings.Replace(reportTemplate, "__RUN_DATA__", string(data), 1)
	return os.WriteFile(file, []byte(html), 0600)
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", target)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	} else {
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Run()
}

func loadRun(root, id string) (*Run, string, error) {
	if id == "" {
		data, err := os.ReadFile(filepath.Join(root, ".blindspot", "latest.json"))
		if err != nil {
			return nil, "", fmt.Errorf("no report yet. Run `blindspot run` first")
		}
		var latest struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(data, &latest); err != nil {
			return nil, "", err
		}
		id = latest.ID
	}
	dir := filepath.Join(root, ".blindspot", "runs", id)
	if filepath.Base(id) != id || id == "." || id == ".." {
		return nil, "", fmt.Errorf("invalid run ID")
	}
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return nil, "", err
	}
	var run Run
	if err = json.Unmarshal(data, &run); err != nil {
		return nil, "", err
	}
	if run.SchemaVersion != 1 {
		return nil, "", fmt.Errorf("unsupported report version: %d", run.SchemaVersion)
	}
	return &run, dir, nil
}

func serveReport(ctx context.Context, file string, port int, open bool) error {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	url := "http://" + listener.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, file)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("\n  Report: %s\n  Local, read-only. Press Ctrl+C to stop.\n\n", url)
	if open {
		_ = openBrowser(url)
	}
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
