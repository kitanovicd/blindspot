package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// InstallGambit verifies and installs the pinned release in a user-owned directory.
func InstallGambit(ctx context.Context, output, progress io.Writer) error {
	var asset, checksum string
	switch runtime.GOOS {
	case "darwin":
		asset = "gambit-macos-v1.0.6"
		checksum = "cf8b48e9dad8d10fa2ea7c5a4497fa3eda17eac2d77c653c2699872484aa1a01"
	case "linux":
		if runtime.GOARCH != "amd64" {
			return fmt.Errorf("upstream Gambit has no Linux ARM binary. Build Gambit v1.0.6 from source and pass --gambit PATH")
		}
		asset = "gambit-linux-v1.0.6"
		checksum = "76321d84564ff4d8222bfb359f1fe67eae69f21e689dbc9c23760257a94864fa"
	default:
		return fmt.Errorf("use macOS, Linux x86-64, or WSL for this release")
	}
	file, err := gambitPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(file)
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if data, err := os.ReadFile(file); err == nil && digest(data) == checksum {
		if err := os.Chmod(file, 0755); err != nil {
			return err
		}
		fmt.Fprintln(output, "Gambit v1.0.6 is installed and checksum verified:", file)
		return nil
	}
	url := "https://github.com/Certora/gambit/releases/download/v1.0.6/" + asset
	fmt.Fprintln(progress, "  Downloading Gambit v1.0.6 from Certora’s GitHub release…")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Gambit download returned %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 30*1024*1024))
	if err != nil {
		return err
	}
	if digest(data) != checksum {
		return fmt.Errorf("Gambit checksum mismatch; installation refused")
	}
	tmp, err := os.CreateTemp(dir, ".gambit-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), file); err != nil {
		return err
	}
	fmt.Fprintln(output, "Gambit v1.0.6 installed and SHA-256 verified:", file)
	return nil
}

func gambitPath() (string, error) {
	dir := os.Getenv("BLINDSPOT_TOOLS_DIR")
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("find user cache: %w; set BLINDSPOT_TOOLS_DIR to a writable directory", err)
		}
		dir = filepath.Join(cache, "blindspot", "tools")
	}
	return filepath.Abs(filepath.Join(dir, "gambit-v1.0.6", "gambit"))
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
