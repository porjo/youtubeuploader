package youtubeuploader

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestEstimateUploadQuotaCost(t *testing.T) {
	cfg := Config{
		Thumbnail:   "thumb.jpg",
		Caption:     "cap.srt",
		PlaylistIDs: []string{"p1", "p2"},
	}
	got := estimateUploadQuotaCost(cfg)
	want := 100 + 50 + 400 + 2*50
	if got != want {
		t.Fatalf("estimateUploadQuotaCost() = %d, want %d", got, want)
	}
}

func TestWrapUploadError_WithQuotaReason(t *testing.T) {
	err := &googleapi.Error{
		Code: 403,
		Errors: []googleapi.ErrorItem{
			{Reason: "quotaExceeded"},
		},
		Message: "quota exceeded",
	}

	wrapped := wrapUploadError(err, "videos.insert")
	if wrapped == nil {
		t.Fatal("wrapped error is nil")
	}
	msg := wrapped.Error()
	for _, token := range []string{YouTubeQuotaCostDocURL, YouTubeQuotaAuditDocURL, YouTubeQuotaExtensionFormURL, YouTubeResumableUploadDocURL} {
		if !strings.Contains(msg, token) {
			t.Fatalf("expected wrapped message to include %q, got %s", token, msg)
		}
	}
}

func TestExtractYouTubeErrorReasons(t *testing.T) {
	err := &googleapi.Error{
		Errors: []googleapi.ErrorItem{
			{Reason: "quotaExceeded"},
			{Reason: "quotaExceeded"},
			{Reason: "dailyLimitExceeded"},
		},
	}
	reasons := extractYouTubeErrorReasons(err)
	if len(reasons) != 2 {
		t.Fatalf("reasons length = %d, want 2", len(reasons))
	}
	if reasons[0] != "dailyLimitExceeded" || reasons[1] != "quotaExceeded" {
		t.Fatalf("unexpected reasons order/content: %+v", reasons)
	}
}

func TestWrapUploadError_NonGoogleError(t *testing.T) {
	wrapped := wrapUploadError(errors.New("x"), "videos.insert")
	if !strings.Contains(wrapped.Error(), "videos.insert failed") {
		t.Fatalf("unexpected wrapped message: %s", wrapped.Error())
	}
}
