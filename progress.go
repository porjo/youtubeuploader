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
	"os"
	"strings"
	"time"
)

type Progress struct {
	Erase     int
	Transport *limitTransport
	Filesize  int64
	Quiet     bool
}

func (p *Progress) Progress(quitChan chanChan, signalChan chan os.Signal) {
	ticker := time.Tick(time.Second)
	for {
		select {
		case <-ticker:
			if !p.Quiet {
				p.progressOut()
			}
		case <-signalChan:
			p.progressOut()
		case ch := <-quitChan:
			// final newline
			fmt.Println()
			close(ch)
			return
		}
	}
}

func (p *Progress) progressOut() {
	if p.Transport.reader != nil {
		s := p.Transport.reader.Monitor.Status()
		curRate := float32(s.CurRate)
		var status string
		if curRate >= 125000 {
			status = fmt.Sprintf("Progress: %8.2f Mbps, %d / %d (%s) ETA %8s", curRate/125000, s.Bytes, p.Filesize, s.Progress, s.TimeRem)
		} else {
			status = fmt.Sprintf("Progress: %8.2f Kbps, %d / %d (%s) ETA %8s", curRate/125, s.Bytes, p.Filesize, s.Progress, s.TimeRem)
		}
		if p.Quiet {
			fmt.Printf("%s\n", status)
		} else {
			// Erase to start of line, then output status
			fmt.Printf("\r%s\r%s", strings.Repeat(" ", p.Erase), status)
			p.Erase = len(status)
		}
	}
}
