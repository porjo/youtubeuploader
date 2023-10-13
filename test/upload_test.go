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

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	yt "github.com/porjo/youtubeuploader"
	"github.com/porjo/youtubeuploader/internal/limiter"
	"github.com/porjo/youtubeuploader/internal/utils"
	"google.golang.org/api/youtube/v3"
)

const (
	fileSize int = 1e7 // 10MB

	oAuthResponse = `{
		"access_token": "xxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"expires_in": 3599,
		"scope": "https://www.googleapis.com/auth/youtube https://www.googleapis.com/auth/youtube.upload https://www.googleapis.com/auth/youtubepartner",
		"token_type": "Bearer"
	  }`
)

var (
	testServer *httptest.Server

	config    yt.Config
	transport *mockTransport
)

type mockTransport struct {
	url *url.URL
}

type mockReader struct {
	read     int
	fileSize int
}

func (m *mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	fmt.Printf("original request URL %s\n", r.URL.String())
	r.URL = m.url
	return http.DefaultTransport.RoundTrip(r)
}

func (m *mockReader) Close() error {
	return nil
}

func (m *mockReader) Read(p []byte) (int, error) {

	l := len(p)
	if m.read+l >= m.fileSize {
		diff := m.fileSize - m.read
		m.read += diff
		return diff, io.EOF
	}
	m.read += l
	return l, nil
}

func TestMain(m *testing.M) {

	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// be sure to read the request body otherwise the client gets confused
		// body, err := io.ReadAll(r.Body)
		_, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading body: %v", err)
			http.Error(w, "can't read body", http.StatusBadRequest)
			return
		}
		//		log.Printf("Mock server: request body length %d", len(body))

		w.Header().Set("Content-Type", "application/json")
		if r.Host == "oauth2.googleapis.com" {
			fmt.Fprintln(w, oAuthResponse)
		} else if r.Host == "youtube.googleapis.com" {
			video := youtube.Video{
				Id: "test",
			}
			videoJ, err := json.Marshal(video)
			if err != nil {
				fmt.Printf("json marshall error %s\n", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fmt.Fprintln(w, string(videoJ))
		}
	}))
	defer testServer.Close()

	url, err := url.Parse(testServer.URL)
	if err != nil {
		log.Fatal(err)
	}

	transport = &mockTransport{url: url}

	config = yt.Config{}
	//config.Logger = utils.NewLogger(true)
	config.Logger = utils.NewLogger(false)
	config.Filename = "test.mp4"

	ret := m.Run()

	os.Exit(ret)
}

func TestRateLimit(t *testing.T) {

	runTimeWant := 2

	rateLimit := int(fileSize / 125 / runTimeWant)

	t.Logf("File size %d bytes", fileSize)
	t.Logf("Ratelimit %d Kbps", rateLimit)

	transport, err := limiter.NewLimitTransport(config.Logger, transport, limiter.LimitRange{}, fileSize, rateLimit)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	videoReader := &mockReader{fileSize: fileSize}
	defer videoReader.Close()

	start := time.Now()
	err = yt.Run(ctx, transport, config, videoReader)
	if err != nil {
		log.Fatal(err)
	}

	runTimeGot := time.Since(start)
	t.Logf("run time: %s\n", runTimeGot)

	// Allow runtime 100ms either side
	startLimit := time.Duration(runTimeWant*1000-100) * time.Millisecond
	endLimit := time.Duration(runTimeWant*1000+100) * time.Millisecond
	if runTimeGot < startLimit || runTimeGot > endLimit {
		t.Fatalf("run time took longer/shorter than expected: %s", runTimeGot)
	}

}
