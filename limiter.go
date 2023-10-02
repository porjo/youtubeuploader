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

package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type limitRange struct {
	start time.Time
	end   time.Time
}

type limitChecker struct {
	sync.Mutex

	io.ReadCloser

	lr      limitRange
	limiter *rate.Limiter
	Monitor *monitor
}

type monitor struct {
	sync.Mutex

	start time.Time
	size  int64

	status status
}

type status struct {
	AvgRate  int64
	Bytes    int64
	TimeRem  time.Duration
	Progress string
}

const bucketSize = 1000

func NewLimitChecker(lr limitRange, r io.ReadCloser) *limitChecker {
	lc := &limitChecker{
		lr:         lr,
		ReadCloser: r,
	}
	return lc
}

func (m *monitor) Status() status {
	//	m.Lock()
	//defer m.Unlock()
	return m.status
}

func (lc *limitChecker) Read(p []byte) (int, error) {

	//	lc.Monitor.Lock()
	//defer lc.Monitor.Unlock()

	var err error
	var read int

	limit := false

	if lc.Monitor.start.IsZero() {
		lc.Monitor.start = time.Now()
	}

	if *rateLimit > 0 {
		if lc.limiter == nil {
			lc.limiter = rate.NewLimiter(rate.Limit(*rateLimit*125), bucketSize)
		}

		if lc.lr.start.IsZero() || lc.lr.end.IsZero() {
			limit = true
		} else {

			if time.Since(lc.lr.start) >= time.Hour*24 {
				lc.lr.start = lc.lr.start.AddDate(0, 0, 1)
				lc.lr.end = lc.lr.end.AddDate(0, 0, 1)
			}

			now := time.Now()
			if lc.lr.start.Before(now) && lc.lr.end.After(now) {
				limit = true
			} else {
				limit = false
			}
		}
	}

	if limit {

		tokens := bucketSize
		if len(p) < bucketSize {
			tokens = len(p)
		}

		for {
			var readL int

			err = lc.limiter.WaitN(context.Background(), tokens)
			if err != nil {
				break
			}

			readL, err = lc.ReadCloser.Read(p[read : read+tokens])
			read += readL

			if err != nil {
				break
			}

			if read == len(p) {
				break
			}
			if read+tokens > len(p) {
				tokens = len(p) - read
			}
		}
	} else {
		read, err = lc.ReadCloser.Read(p)
	}

	lc.Monitor.status.Bytes += int64(read)
	// bytes read will be greater than filesize due to HTTP headers etc, so reset to filesize
	if lc.Monitor.status.Bytes > lc.Monitor.size {
		lc.Monitor.status.Bytes = lc.Monitor.size
	}
	lc.Monitor.status.Progress = fmt.Sprintf("%.1f%%", float64(lc.Monitor.status.Bytes)/float64(lc.Monitor.size)*100)
	lc.Monitor.status.AvgRate = int64(float64(lc.Monitor.status.Bytes) / time.Since(lc.Monitor.start).Seconds())
	lc.Monitor.status.TimeRem = time.Duration(float64(lc.Monitor.size-lc.Monitor.status.Bytes)/float64(lc.Monitor.status.AvgRate)) * time.Second

	return read, err
}

func (lc *limitChecker) Close() error {
	return lc.ReadCloser.Close()
}

func parseLimitBetween(between string) (limitRange, error) {
	var lr limitRange
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
