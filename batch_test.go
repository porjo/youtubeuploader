package youtubeuploader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/porjo/youtubeuploader/internal/limiter"
)

type nopReadCloser struct {
	io.Reader
}

func (n nopReadCloser) Close() error { return nil }

func TestRunBatch_ValidatesInput(t *testing.T) {
	_, err := RunBatch(context.Background(), nil, 0)
	if err == nil || !strings.Contains(err.Error(), "maxConcurrent") {
		t.Fatalf("expected maxConcurrent validation error, got %v", err)
	}

	tasks := []UploadTask{{ID: "a"}}
	_, err = RunBatch(context.Background(), tasks, 1)
	if err == nil || !strings.Contains(err.Error(), "nil OpenVideo") {
		t.Fatalf("expected nil OpenVideo error, got %v", err)
	}

	tasks[0].OpenVideo = func() (io.ReadCloser, error) { return nopReadCloser{Reader: strings.NewReader("x")}, nil }
	_, err = RunBatch(context.Background(), tasks, 1)
	if err == nil || !strings.Contains(err.Error(), "nil Transport") {
		t.Fatalf("expected nil Transport error, got %v", err)
	}
}

func TestRunBatchWithRunner_ExecutesAllTasks(t *testing.T) {
	transport, err := limiter.NewLimitTransport(http.DefaultTransport, limiter.LimitRange{}, 0, 0)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}

	var calls int32
	tasks := []UploadTask{
		{
			ID:        "1",
			AccountID: "acc-a",
			Transport: transport,
			OpenVideo: func() (io.ReadCloser, error) { return nopReadCloser{Reader: strings.NewReader("a")}, nil },
		},
		{
			ID:        "2",
			AccountID: "acc-b",
			Config:    Config{Thumbnail: "thumb.jpg"},
			Transport: transport,
			OpenVideo: func() (io.ReadCloser, error) { return nopReadCloser{Reader: strings.NewReader("b")}, nil },
		},
	}

	runner := func(ctx context.Context, transport *limiter.LimitTransport, config Config, videoReader io.ReadCloser) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}

	results, err := runBatchWithRunner(context.Background(), tasks, 2, runner)
	if err != nil {
		t.Fatalf("runBatchWithRunner returned error: %v", err)
	}
	if len(results) != len(tasks) {
		t.Fatalf("results length = %d, want %d", len(results), len(tasks))
	}
	if calls != int32(len(tasks)) {
		t.Fatalf("runner calls = %d, want %d", calls, len(tasks))
	}
	if results[1].EstimatedQuotaCost != 150 {
		t.Fatalf("EstimatedQuotaCost = %d, want 150", results[1].EstimatedQuotaCost)
	}
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("task %s failed: %v", r.TaskID, r.Err)
		}
	}
}

func TestRunBatchWithRunner_PropagatesTaskErrors(t *testing.T) {
	transport, err := limiter.NewLimitTransport(http.DefaultTransport, limiter.LimitRange{}, 0, 0)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}

	var alertCount int32
	tasks := []UploadTask{
		{
			ID:        "open-fail",
			AccountID: "acc-a",
			Transport: transport,
			OpenVideo: func() (io.ReadCloser, error) { return nil, errors.New("boom") },
			OnAlert: func(alert UploadAlert) {
				if alert.AccountID != "acc-a" {
					t.Fatalf("unexpected AccountID in alert: %s", alert.AccountID)
				}
				atomic.AddInt32(&alertCount, 1)
			},
		},
	}

	results, err := runBatchWithRunner(context.Background(), tasks, 1, func(ctx context.Context, transport *limiter.LimitTransport, config Config, videoReader io.ReadCloser) error {
		return nil
	})
	if err != nil {
		t.Fatalf("runBatchWithRunner returned unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("expected one task error result, got %+v", results)
	}
	if alertCount != 1 {
		t.Fatalf("alertCount = %d, want 1", alertCount)
	}
}
