package appdir

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestDirOverride checks that the environment names the folder, which is how
// a capture, a test or the command line chooses one.
func TestDirOverride(t *testing.T) {
	t.Setenv("WADA_TEST_DATA_DIR", "/tmp/wada-somewhere")
	got, err := Dir("wada-test", "WADA_TEST")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/wada-somewhere" {
		t.Fatalf("Dir() = %q, want the override", got)
	}
}

func TestTerminalContext(t *testing.T) {
	env := TerminalEnv([]string{"PATH=/bin", "MYGO_ENV=production", "WADATERM_DATA_DIR=/production", "WADATERM_CONTEXT_DATA_DIR=/old"}, "/development")
	if slices.Contains(env, "WADATERM_DATA_DIR=/production") || slices.Contains(env, "WADATERM_CONTEXT_DATA_DIR=/old") || slices.Contains(env, "MYGO_ENV=production") || !slices.Contains(env, "WADATERM_CONTEXT_DATA_DIR=/development") {
		t.Fatalf("terminal environment = %q", env)
	}
	t.Setenv("WADATERM_DATA_DIR", "")
	t.Setenv("WADATERM_CONTEXT_DATA_DIR", "/development")
	UseTerminalContext()
	if got := os.Getenv("WADATERM_DATA_DIR"); got != "/development" {
		t.Fatalf("CLI data directory = %q", got)
	}
	t.Setenv("WADATERM_DATA_DIR", "/explicit")
	UseTerminalContext()
	if got := os.Getenv("WADATERM_DATA_DIR"); got != "/explicit" {
		t.Fatalf("explicit override lost: %q", got)
	}
}

// TestDirConfig checks the fallback: the folder under the user's config
// directory, with the development suffix this test binary runs with.
func TestDirConfig(t *testing.T) {
	got, err := Dir("wada-test", "WADA_TEST_MISSING")
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || filepath.Dir(got) != base {
		t.Fatalf("Dir() = %q, not under %q", got, base)
	}
}

func TestDevelopmentAndProductionDirectories(t *testing.T) {
	t.Setenv("WADA_TEST_DATA_DIR", "")
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ environment, folder string }{
		{"development", "wada-test-dev"},
		{"production", "wada-test"},
	} {
		t.Setenv("MYGO_ENV", tc.environment)
		got, err := Dir("wada-test", "WADA_TEST")
		if err != nil || got != filepath.Join(base, tc.folder) {
			t.Fatalf("%s: Dir() = %q, %v", tc.environment, got, err)
		}
	}
}
