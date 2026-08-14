package sitessmoke

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateDeterministicStaticSite(t *testing.T) {
	t.Parallel()

	first := t.TempDir()
	second := t.TempDir()
	if err := Generate(first); err != nil {
		t.Fatal(err)
	}
	if err := Generate(second); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{HTMLFileName, DatastarFileName, LicenseFileName} {
		one, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		two, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(one, two) {
			t.Fatalf("%s output is not deterministic", name)
		}
	}
}

func TestGeneratedPageUsesOnlyDeclarativeDatastar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, HTMLFileName))
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)

	for _, want := range []string{
		`data-signals=`,
		`data-init=`,
		`data-bind:region`,
		`data-text=`,
		`data-on:click=`,
		`data-show=`,
		`aria-live="polite"`,
		`aria-controls="sanity-details"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("generated page does not contain %q", want)
		}
	}
	if got := strings.Count(page, "<script"); got != 1 {
		t.Fatalf("script count = %d, want 1", got)
	}
	if !strings.Contains(page, `<script type="module" src="/`+DatastarFileName+`"></script>`) {
		t.Error("generated page does not load the pinned Datastar asset")
	}
	for _, forbidden := range []string{"onclick=", "EventSource", "leaflet", "@get(", "<script>"} {
		if strings.Contains(page, forbidden) {
			t.Errorf("generated page contains forbidden application JavaScript marker %q", forbidden)
		}
	}
}

func TestVendoredDatastarChecksum(t *testing.T) {
	t.Parallel()

	b, err := assets.ReadFile("assets/datastar-v1.0.2.js")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != DatastarSHA256 {
		t.Fatalf("Datastar checksum = %s, want %s", got, DatastarSHA256)
	}
}

func TestGenerateReportsFilesystemErrors(t *testing.T) {
	t.Parallel()

	t.Run("output path is a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "output")
		if err := os.WriteFile(path, []byte("occupied"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Generate(path); err == nil || !strings.Contains(err.Error(), "create output directory") {
			t.Fatalf("Generate() error = %v, want create output directory error", err)
		}
	})

	for _, name := range []string{HTMLFileName, DatastarFileName, LicenseFileName} {
		name := name
		t.Run(name+" is a directory", func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := Generate(dir); err == nil {
				t.Fatalf("Generate() succeeded with directory at output file %s", name)
			}
		})
	}
}
