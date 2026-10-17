package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSVMPlatformDirectories(t *testing.T) {
	home := filepath.Join(t.TempDir(), "user with spaces")
	data := filepath.Join(t.TempDir(), "custom data")
	for _, c := range []struct{ platform, xdg, appData, want string }{
		{"darwin", data, "", filepath.Join(home, "Library", "Application Support", "svm")},
		{"linux", "", "", filepath.Join(home, ".local", "share", "svm")},
		{"linux", data, "", filepath.Join(data, "svm")},
		{"linux", "relative", "", filepath.Join(home, ".local", "share", "svm")},
		{"windows", "", data, filepath.Join(data, "svm")},
	} {
		dirs := svmDirectories(home, c.platform, c.xdg, c.appData)
		if len(dirs) != 2 || dirs[0] != filepath.Join(home, ".svm") || dirs[1] != c.want {
			t.Fatalf("%+v: got %v", c, dirs)
		}
	}
}

func TestFindSVMCompilerAcrossLocations(t *testing.T) {
	home := t.TempDir()
	dirs := svmDirectories(home, "darwin", "", "")
	install := func(dir, version string) string {
		t.Helper()
		p := filepath.Join(dir, version, "solc-"+version)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("compiler fixture"), 0700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	modern := install(dirs[1], "0.8.34")
	if p := findSVMCompiler(dirs, "0.8.34"); p != modern {
		t.Fatalf("modern compiler missed: %s", p)
	}
	if p := findSVMCompiler(dirs, ""); p != modern {
		t.Fatalf("single compiler missed: %s", p)
	}
	if p := findSVMCompiler(dirs, "0.8.33"); p != "" {
		t.Fatalf("wrong version selected: %s", p)
	}
	legacy := install(dirs[0], "0.8.26")
	if p := findSVMCompiler(dirs, "0.8.26"); p != legacy {
		t.Fatalf("legacy compiler missed: %s", p)
	}
	if p := findSVMCompiler(dirs, ""); p != "" {
		t.Fatalf("ambiguous compilers selected: %s", p)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	// Ignore unfinished installs, directories masquerading as binaries, and lock files.
	if err := os.Mkdir(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs[1], ".lock-solc-0.8.34"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if p := findSVMCompiler(dirs, ""); p != modern {
		t.Fatalf("non-binary entry affected resolution: %s", p)
	}
	alias := filepath.Join(dirs[0], "0.8.34")
	if err := os.Symlink(filepath.Dir(modern), alias); err != nil {
		t.Fatal(err)
	}
	if p := findSVMCompiler(dirs, ""); p == "" {
		t.Fatal("same compiler reached through a symlink counted twice")
	}
}
