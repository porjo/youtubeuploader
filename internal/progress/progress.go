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
	Filesize  int64
	Quiet     bool

	erase int
}

func NewProgress(transport *limiter.LimitTransport) *Progress {
	return &Progress{
		transport: transport,
	}
}

func (p *Progress) Progress(ctx context.Context, signalChan chan os.Signal) {
	ticker := time.Tick(time.Second)
	for {
		select {
		case <-ticker:
			if !p.Quiet {
				p.progressOut()
			}
		case <-signalChan:
			p.progressOut()
		case <-ctx.Done():
			return
		}
	}
}

func (p *Progress) progressOut() {
	s := p.transport.GetMonitorStatus()
	avgRate := float64(s.AvgRate)
	var status string
	if avgRate >= 125000 {
		status = fmt.Sprintf("Progress: %8.2f Mbps, %d / %d (%s) ETA %8s", avgRate/125000, s.Bytes, p.Filesize, s.Progress, s.TimeRem)
	} else {
		status = fmt.Sprintf("Progress: %8.2f Kbps, %d / %d (%s) ETA %8s", avgRate/125, s.Bytes, p.Filesize, s.Progress, s.TimeRem)
	}
	if p.Quiet {
		fmt.Printf("%s\n", status)
	} else {
		// erase to start of line, then output status
		fmt.Printf("\r%s\r%s", strings.Repeat(" ", p.erase), status)
		p.erase = len(status)
	}
}
