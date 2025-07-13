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
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	yt "github.com/porjo/youtubeuploader"
	"github.com/porjo/youtubeuploader/internal/limiter"
	"google.golang.org/api/youtube/v3"
)

const (
	fileSize int64 = 1e7 // 10MB

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

	recordingDate yt.Date
)

type mockTransport struct {
	url *url.URL
}

type mockReader struct {
	read     int64
	fileSize int64
}

func (m *mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	slog.Info("roundtrip", "method", r.Method, "URL", r.URL.String())
	r.URL.Scheme = m.url.Scheme
	r.URL.Host = m.url.Host

	return http.DefaultTransport.RoundTrip(r)
}

func (m *mockReader) Close() error {
	return nil
}

func (m *mockReader) Read(p []byte) (int, error) {

	l := len(p)
	if m.read+int64(l) >= m.fileSize {
		diff := m.fileSize - m.read
		m.read += diff
		return int(diff), io.EOF
	}
	m.read += int64(l)
	return l, nil
}

func TestMain(m *testing.M) {

	logger := slog.Default()

	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		l := logger.With("src", "httptest")

		video, err := handleVideoPost(r, l)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		if video != nil {
			recDateIn, err := time.Parse(time.RFC3339Nano, video.RecordingDetails.RecordingDate)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			if recDateIn.Equal(recordingDate.Time) {
				http.Error(w, "Date didn't match", http.StatusBadRequest)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.Host {
		case "oauth2.googleapis.com":
			fmt.Fprintln(w, oAuthResponse)
		case "youtube.googleapis.com":

			if strings.HasPrefix(r.URL.RequestURI(), "/upload") {
				video := youtube.Video{
					Id: "test",
				}
				videoJ, err := json.Marshal(video)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				fmt.Fprintln(w, string(videoJ))
			} else if strings.HasPrefix(r.URL.RequestURI(), "/youtube/v3/playlists") {
				playlist1 := &youtube.Playlist{
					Id: "xxxx",
					Snippet: &youtube.PlaylistSnippet{
						Title: "Test Playlist 1",
					},
				}
				playlist2 := &youtube.Playlist{
					Id: "yyyy",
					Snippet: &youtube.PlaylistSnippet{
						Title: "Test Playlist 2",
					},
				}
				playlistResponse := youtube.PlaylistListResponse{
					Items: []*youtube.Playlist{playlist1, playlist2},
				}
				playlistJ, err := json.Marshal(playlistResponse)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				fmt.Fprintln(w, string(playlistJ))
			} else if strings.HasPrefix(r.URL.RequestURI(), "/youtube/v3/playlistItems") {
				fmt.Fprintln(w, "{}")
			}
		}

	}))
	defer testServer.Close()

	url, err := url.Parse(testServer.URL)
	if err != nil {
		log.Fatal(err)
	}

	transport = &mockTransport{url: url}

	config = yt.Config{}
	config.Filename = "test.mp4"
	config.PlaylistIDs = []string{"xxxx", "yyyy"}
	recordingDate = yt.Date{}
	recordingDate.Time = time.Now()
	config.RecordingDate = recordingDate

	ret := m.Run()

	os.Exit(ret)
}

func TestRateLimit(t *testing.T) {

	runTimeWant := 2

	rateLimit := int(fileSize / 125 / int64(runTimeWant))

	t.Logf("File size %d bytes", fileSize)
	t.Logf("Ratelimit %d Kbps", rateLimit)

	transport, err := limiter.NewLimitTransport(transport, limiter.LimitRange{}, fileSize, rateLimit)
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

func handleVideoPost(r *http.Request, l *slog.Logger) (*youtube.Video, error) {

	if r.Method != http.MethodPost {
		l.Info("not POST, skipping")
		return nil, nil
	}
	// Parse the Content-Type header
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return nil, fmt.Errorf("Missing Content-Type header")
	}

	// Parse the media type and boundary
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}

	if mediaType != "multipart/related" {
		l.Info("not multipart, skipping")
		return nil, nil
	}

	boundary, ok := params["boundary"]
	if !ok {
		return nil, fmt.Errorf("Missing boundary parameter")
	}

	// Parse the multipart form
	mr := multipart.NewReader(r.Body, boundary)

	video := &youtube.Video{}

	// Iterate through the parts
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		contentType := part.Header.Get("Content-Type")
		switch contentType {
		case "application/json":
			// Parse JSON part
			err := json.NewDecoder(part).Decode(video)
			if err != nil {
				return nil, err
			}
		case "application/octet-stream":
			// Read binary data part
			_, err = io.Copy(io.Discard, part)
			if err != nil {
				return nil, err
			}
		default:
			// Ignore other content types
		}
	}

	return video, nil
}
