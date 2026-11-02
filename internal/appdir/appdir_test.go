package appdir

import (
	"os"
	"path/filepath"
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
