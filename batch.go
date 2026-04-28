package youtubeuploader

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/porjo/youtubeuploader/internal/limiter"
)

// UploadTask represents one upload unit for batch execution.
type UploadTask struct {
	ID        string
	AccountID string
	Config    Config
	Transport *limiter.LimitTransport
	OpenVideo func() (io.ReadCloser, error)
	OnAlert   func(alert UploadAlert)
}

// UploadResult captures execution status for a single task.
type UploadResult struct {
	TaskID             string
	AccountID          string
	EstimatedQuotaCost int
	Err                error
}

// RunBatch executes upload tasks with bounded concurrency.
func RunBatch(ctx context.Context, tasks []UploadTask, maxConcurrent int) ([]UploadResult, error) {
	return runBatchWithRunner(ctx, tasks, maxConcurrent, Run)
}

type runFunc func(ctx context.Context, transport *limiter.LimitTransport, config Config, videoReader io.ReadCloser) error

func runBatchWithRunner(ctx context.Context, tasks []UploadTask, maxConcurrent int, runner runFunc) ([]UploadResult, error) {
	if maxConcurrent <= 0 {
		return nil, fmt.Errorf("maxConcurrent must be greater than zero")
	}
	if len(tasks) == 0 {
		return []UploadResult{}, nil
	}

	for i, task := range tasks {
		if task.OpenVideo == nil {
			return nil, fmt.Errorf("task %d has nil OpenVideo", i)
		}
		if task.Transport == nil {
			return nil, fmt.Errorf("task %d has nil Transport", i)
		}
	}

	results := make([]UploadResult, len(tasks))
	queued := make([]bool, len(tasks))
	for i, task := range tasks {
		results[i] = UploadResult{
			TaskID:             task.ID,
			AccountID:          task.AccountID,
			EstimatedQuotaCost: estimateUploadQuotaCost(task.Config),
		}
	}
	jobs := make(chan int)
	wg := sync.WaitGroup{}

	workerCount := maxConcurrent
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				task := tasks[idx]
				result := results[idx]

				if err := ctx.Err(); err != nil {
					result.Err = err
					results[idx] = result
					emitTaskAlert(task, result, "error", result.Err)
					continue
				}

				reader, err := task.OpenVideo()
				if err != nil {
					result.Err = fmt.Errorf("open video failed: %w", err)
					results[idx] = result
					emitTaskAlert(task, result, "error", result.Err)
					continue
				}

				err = runTaskSafely(ctx, runner, task, reader)
				closeErr := reader.Close()
				if err != nil {
					result.Err = err
				} else if closeErr != nil {
					result.Err = fmt.Errorf("close video reader failed: %w", closeErr)
				}

				results[idx] = result
				if result.Err != nil {
					emitTaskAlert(task, result, "error", result.Err)
				}
			}
		}()
	}

enqueueLoop:
	for i := range tasks {
		select {
		case <-ctx.Done():
			break enqueueLoop
		case jobs <- i:
			queued[i] = true
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		for i, result := range results {
			if !queued[i] && result.Err == nil {
				result.Err = err
				results[i] = result
				emitTaskAlert(tasks[i], result, "error", result.Err)
			}
		}
	}

	return results, nil
}

func runTaskSafely(ctx context.Context, runner runFunc, task UploadTask, reader io.ReadCloser) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("upload task panic: %v", r)
		}
	}()
	return runner(ctx, task.Transport, task.Config, reader)
}

// EstimateQuotaByAccount returns an estimated quota cost grouped by account ID.
func EstimateQuotaByAccount(tasks []UploadTask) map[string]int {
	usage := make(map[string]int)
	for _, task := range tasks {
		usage[task.AccountID] += estimateUploadQuotaCost(task.Config)
	}
	return usage
}

func emitTaskAlert(task UploadTask, result UploadResult, severity string, err error) {
	if task.OnAlert == nil || err == nil {
		return
	}
	task.OnAlert(UploadAlert{
		Time:      time.Now().UTC(),
		TaskID:    result.TaskID,
		AccountID: result.AccountID,
		Severity:  severity,
		Message:   err.Error(),
		Reasons:   extractYouTubeErrorReasons(err),
	})
}
