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
	"fmt"
	"strings"
	"time"

	"github.com/porjo/go-flowrate/flowrate"
)

type limitRange struct {
	start time.Time
	end   time.Time
}

type limitChecker struct {
	limitRange
	reader *flowrate.Reader
}

func (lc *limitChecker) Read(p []byte) (n int, err error) {
	if lc.start.IsZero() || lc.end.IsZero() {
		lc.reader.SetLimit(int64(*rate * 125))
		return lc.reader.Read(p)
	}

	now := time.Now()

	if now.Sub(lc.start) >= time.Hour*24 {
		lc.start = lc.start.AddDate(0, 0, 1)
		lc.end = lc.end.AddDate(0, 0, 1)
	}

	if lc.start.Before(now) && lc.end.After(now) {
		// kbit/s to B/s = 1000/8 = 125
		lc.reader.SetLimit(int64(*rate * 125))
	} else {
		lc.reader.SetLimit(0)
	}

	return lc.reader.Read(p)
}

func (lc *limitChecker) Close() error {
	return nil
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
