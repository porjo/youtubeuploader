package youtubeuploader

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func testToken() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  "ya29.test-access-token",
		TokenType:    "Bearer",
		RefreshToken: "1//test-refresh-token",
		Expiry:       time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
	}
}

func TestCacheStringToken(t *testing.T) {
	tok := testToken()
	b, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}

	cs := &CacheString{input: string(b)}
	got, err := cs.Token()
	if err != nil {
		t.Fatalf("CacheString.Token() error: %v", err)
	}

	if got.AccessToken != tok.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, tok.AccessToken)
	}
	if got.RefreshToken != tok.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, tok.RefreshToken)
	}
}

func TestCacheStringTokenInvalidJSON(t *testing.T) {
	cs := &CacheString{input: "not-json"}
	_, err := cs.Token()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestCacheStringPutTokenStderr(t *testing.T) {
	tok := testToken()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	cs := &CacheString{input: "{}", output: "stderr"}
	err = cs.PutToken(tok)

	os.Stderr = origStderr
	w.Close()

	if err != nil {
		t.Fatalf("PutToken() error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if len(output) == 0 {
		t.Fatal("expected output on stderr, got nothing")
	}
	if !bytes.Contains(buf.Bytes(), []byte("REFRESHED_TOKEN=")) {
		t.Errorf("expected REFRESHED_TOKEN= prefix, got %q", output)
	}
}

func TestCacheStringPutTokenStdout(t *testing.T) {
	tok := testToken()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	cs := &CacheString{input: "{}", output: "stdout"}
	err = cs.PutToken(tok)

	os.Stdout = origStdout
	w.Close()

	if err != nil {
		t.Fatalf("PutToken() error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !bytes.Contains(buf.Bytes(), []byte("REFRESHED_TOKEN=")) {
		t.Errorf("expected REFRESHED_TOKEN= prefix on stdout, got %q", buf.String())
	}
}

func TestCacheStringPutTokenFile(t *testing.T) {
	tok := testToken()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "token.json")

	cs := &CacheString{input: "{}", output: outPath}
	err := cs.PutToken(tok)
	if err != nil {
		t.Fatalf("PutToken() error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var got oauth2.Token
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.AccessToken != tok.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, tok.AccessToken)
	}
}

func TestCacheStringPutTokenDefaultStderr(t *testing.T) {
	tok := testToken()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	cs := &CacheString{input: "{}", output: ""}
	err = cs.PutToken(tok)

	os.Stderr = origStderr
	w.Close()

	if err != nil {
		t.Fatalf("PutToken() error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !bytes.Contains(buf.Bytes(), []byte("REFRESHED_TOKEN=")) {
		t.Errorf("empty output should default to stderr, got %q", buf.String())
	}
}
