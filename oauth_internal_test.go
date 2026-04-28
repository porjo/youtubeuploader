package youtubeuploader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTokenCachePath_ExistingCustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom.token")
	if err := os.WriteFile(customPath, []byte("{}"), 0600); err != nil {
		t.Fatalf("failed to create custom cache file: %v", err)
	}

	got, err := resolveTokenCachePath(customPath)
	if err != nil {
		t.Fatalf("resolveTokenCachePath returned error: %v", err)
	}
	if got != customPath {
		t.Fatalf("resolveTokenCachePath returned %q, want %q", got, customPath)
	}
}
