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

package progress

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/porjo/youtubeuploader/internal/limiter"
)

type Progress struct {
	transport *limiter.LimitTransport
	interval  time.Duration
	quiet     bool

	erase int
}

func NewProgress(transport *limiter.LimitTransport, interval time.Duration) (*Progress, error) {
	if transport == nil {
		return nil, fmt.Errorf("transport cannot be nil")
	}

	p := &Progress{
		transport: transport,
	}

	if interval == 0 {
		p.quiet = true
	} else {
		p.interval = interval
	}

	return p, nil
}

func (p *Progress) Run(ctx context.Context, signalChan chan os.Signal) {

	var ticker *time.Ticker

	if p.interval == 0 {
		// set a 1 second ticker by default
		ticker = time.NewTicker(time.Second)
	} else {
		ticker = time.NewTicker(p.interval)
	}

	for {
		select {
		case <-ticker.C:
			// output on time interval
			if !p.quiet {
				p.Output()
			}
		case <-signalChan:
			// output on demand
			p.Output()
		case <-ctx.Done():
			return
		}
	}
}

func (p *Progress) Output() {
	s := p.transport.GetMonitorStatus()
	avgRate := float64(s.AvgRate)
	elapsed := time.Since(s.Start).Round(time.Second)
	var status string
	if avgRate >= 125000 {
		// Bytes/s -> Megabits/s = Bbps/125000
		status = fmt.Sprintf("Progress: %6.2f Mbit/s (%5.2f MiB/s), %dk / %dk (%s) ETA %4s, Elapsed %s", avgRate/125000, avgRate/(1024*1024), s.Bytes/1024, s.TotalBytes/1024, s.Progress, s.TimeRem, elapsed)
	} else {
		// Bytes/s -> Kilobits/s = Bbps/125
		status = fmt.Sprintf("Progress: %6.f Kbit/s (%5.f KiB/s), %dk / %dk (%s) ETA %4s, Elapsed %s", avgRate/125, avgRate/1024, s.Bytes/1024, s.TotalBytes/1024, s.Progress, s.TimeRem, elapsed)
	}

	if p.quiet {
		// Don't erase to start of line for on-demand status output
		fmt.Printf("%s\n", status)
	} else {
		// erase to start of line, then output status
		fmt.Printf("\r%s\r%s", strings.Repeat(" ", p.erase), status)
		p.erase = len(status)
	}
}
