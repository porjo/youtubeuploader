/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package limiter

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	kbps2bpsMultiplier = 125 // kbps * 125 = bytes/s
)

type LimitTransport struct {
	transport  http.RoundTripper
	limitRange LimitRange
	reader     limitChecker
	readerInit bool
	filesize   int64
	rateLimit  int
}

type LimitRange struct {
	start time.Time
	end   time.Time
}

type limitChecker struct {
	io.ReadCloser
	sync.Mutex

	limitRange LimitRange
	limiter    *rate.Limiter
	status     Status
	rateLimit  int
	burstLimit int

	ctx context.Context
}

type Status struct {
	AvgRate    int // Bytes per second
	Bytes      int64
	TotalBytes int64

	Progress string

	Start   time.Time
	TimeRem time.Duration
}

func (lc *limitChecker) Read(p []byte) (int, error) {

	lc.Lock()
	defer lc.Unlock()

	limit := false

	if lc.status.Start.IsZero() {
		lc.status.Start = time.Now()
	}

	if lc.rateLimit > 0 {
		if lc.limiter == nil {

			// FIXME: setting burst limit to initial buffer size seems about right,
			// but is there a better value?
			lc.burstLimit = len(p)

			slog.Debug("limiter: creating limiter", "burstlimit", lc.burstLimit, "ratelimit", lc.rateLimit)

			// token bucket
			// - starts full and is refilled at the specified rate (tokens per second)
			// - can burst (empty bucket) up to bucket size (burst limit)

			lc.limiter = rate.NewLimiter(rate.Limit(lc.rateLimit*kbps2bpsMultiplier), lc.burstLimit)
		}

		if lc.limitRange.start.IsZero() || lc.limitRange.end.IsZero() {
			limit = true
		} else {

			if time.Since(lc.limitRange.start) >= time.Hour*24 {
				lc.limitRange.start = lc.limitRange.start.AddDate(0, 0, 1)
				lc.limitRange.end = lc.limitRange.end.AddDate(0, 0, 1)
			}

			now := time.Now()
			if lc.limitRange.start.Before(now) && lc.limitRange.end.After(now) {
				limit = true
			} else {
				limit = false
			}
		}
	}

	if limit {

		// tokens cannot exceed burst limit
		if len(p) > lc.burstLimit {
			slog.Debug("limiter: adjusting read buffer to match burst limit", "buf size", len(p), "burst limit", lc.burstLimit)
			p = p[:lc.burstLimit]
		}

		tokens := len(p)

		err := lc.limiter.WaitN(lc.ctx, tokens)
		if err != nil {
			return 0, err
		}

	}

	read, err := lc.ReadCloser.Read(p)
	if err != nil {
		return read, err
	}

	lc.status.Bytes += int64(read)

	if lc.status.TotalBytes > 0 {
		// bytes read may be greater than filesize due to MIME multipart headers in body. Reset to filesize
		if lc.status.Bytes > lc.status.TotalBytes {
			lc.status.Bytes = lc.status.TotalBytes
		}
		lc.status.Progress = fmt.Sprintf("%.1f%%", float64(lc.status.Bytes)/float64(lc.status.TotalBytes)*100)
		lc.status.TimeRem = time.Duration(float64(lc.status.TotalBytes-lc.status.Bytes)/float64(lc.status.AvgRate)) * time.Second
	} else {
		lc.status.Progress = "n/a"
	}
	lc.status.AvgRate = int(float64(lc.status.Bytes) / time.Since(lc.status.Start).Seconds())

	return read, err
}

func (lc *limitChecker) Close() error {
	return lc.ReadCloser.Close()
}

func ParseLimitBetween(between, inputTimeLayout string) (LimitRange, error) {
	var lr LimitRange
	var err error
	var start, end time.Time
	parts := strings.Split(between, "-")
	if len(parts) != 2 {
		return lr, fmt.Errorf("limitBetween should have 2 parts separated by a hyphen")
	}

	now := time.Now()

	start, err = time.ParseInLocation(inputTimeLayout, parts[0], time.Local)
	if err != nil {
		return lr, fmt.Errorf("limitBetween start time was invalid: %v", err)
	}
	lr.start = time.Date(now.Year(), now.Month(), now.Day(), start.Hour(), start.Minute(), 0, 0, time.Local)

	end, err = time.ParseInLocation(inputTimeLayout, parts[1], time.Local)
	if err != nil {
		return lr, fmt.Errorf("limitBetween end time was invalid: %v", err)
	}
	lr.end = time.Date(now.Year(), now.Month(), now.Day(), end.Hour(), end.Minute(), 0, 0, time.Local)

	// handle range spanning midnight
	if lr.end.Before(lr.start) {
		lr.end = lr.end.AddDate(0, 0, 1)
	}

	return lr, nil
}

func NewLimitTransport(rt http.RoundTripper, lr LimitRange, filesize int64, ratelimit int) (*LimitTransport, error) {

	if rt == nil {
		return nil, fmt.Errorf("roundtripper can't be nil")
	}

	lt := &LimitTransport{
		transport:  rt,
		limitRange: lr,
		filesize:   filesize,
		rateLimit:  ratelimit,
	}

	return lt, nil
}

// HasStarted returns whether the LimitTransport has seen use
func (t *LimitTransport) HasStarted() bool {
	t.reader.Lock()
	defer t.reader.Unlock()
	return t.readerInit
}

func (t *LimitTransport) RoundTrip(r *http.Request) (*http.Response, error) {

	contentType := r.Header.Get("Content-Type")

	// FIXME: this is messy. Need a better way to detect roundtrip associated with video upload
	if strings.HasPrefix(contentType, "multipart/related") ||
		strings.HasPrefix(contentType, "video") ||
		strings.HasPrefix(contentType, "application/octet-stream") ||
		r.Header.Get("X-Upload-Content-Type") == "application/octet-stream" {

		t.reader.Lock()
		if !t.readerInit {
			t.reader.ctx = r.Context()
			t.reader.limitRange = t.limitRange
			t.reader.rateLimit = t.rateLimit
			t.reader.status.TotalBytes = t.filesize
			t.readerInit = true
		}

		if t.reader.ReadCloser != nil {
			t.reader.ReadCloser.Close()
		}

		// wrap request body in a limitchecker
		t.reader.ReadCloser = r.Body
		r.Body = &t.reader

		t.reader.Unlock()
	}

	if contentType != "" {
		slog.Debug("content-Type header", "value", contentType)
	}
	slog.Debug("requesting URL", "url", r.URL)

	resp, err := t.transport.RoundTrip(r)
	if err == nil {
		slog.Debug("response status", "code", resp.StatusCode)
		if resp.Body != nil {
			respBytes, err := httputil.DumpResponse(resp, true)
			if err != nil {
				slog.Debug("error reading response", "err", err)
			} else {
				slog.Debug("response dump", "response", respBytes)
			}
		}
	}

	return resp, err
}

func (t *LimitTransport) GetMonitorStatus() Status {
	t.reader.Lock()
	defer t.reader.Unlock()
	return t.reader.status
}
