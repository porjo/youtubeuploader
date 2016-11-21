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
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/porjo/go-flowrate/flowrate"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/youtube/v3"
)

var (
	filename    = flag.String("filename", "", "Filename to upload. Can be a URL")
	title       = flag.String("title", "Video Title", "Video title")
	description = flag.String("description", "uploaded by youtubeuploader", "Video description")
	category    = flag.String("category", "", "Video category")
	keywords    = flag.String("keywords", "", "Comma separated list of video keywords")
	privacy     = flag.String("privacy", "private", "Video privacy status")
	quiet       = flag.Bool("quiet", false, "Suppress progress indicator")
	rate        = flag.Int("ratelimit", 0, "Rate limit upload in KiB/s. No limit by default")
)

func main() {
	flag.Parse()

	if *filename == "" {
		fmt.Printf("You must provide a filename of a video file to upload\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	var reader io.Reader
	var filesize int64

	if strings.HasPrefix(*filename, "http") {
		resp, err := http.Head(*filename)
		if err != nil {
			log.Fatalf("Error opening %v: %v", *filename, err)
		}
		lenStr := resp.Header.Get("content-length")
		if lenStr != "" {
			filesize, err = strconv.ParseInt(lenStr, 10, 64)
			if err != nil {
				log.Fatal(err)
			}
		}

		resp, err = http.Get(*filename)
		if err != nil {
			log.Fatalf("Error opening %v: %v", *filename, err)
		}
		reader = resp.Body
		filesize = resp.ContentLength
		defer resp.Body.Close()
	} else {
		file, err := os.Open(*filename)
		if err != nil {
			log.Fatalf("Error opening %v: %v", *filename, err)
		}
		fileInfo, err := file.Stat()
		if err != nil {
			log.Fatalf("Error stating file %v: %v", *filename, err)
		}
		filesize = fileInfo.Size()
		reader = file
		defer file.Close()
	}

	ctx := context.Background()
	transport := &limitTransport{rt: http.DefaultTransport, filesize: filesize}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
		Transport: transport,
	})

	if !*quiet {
		ticker := time.NewTicker(time.Second).C
		quitChan := make(chan bool)
		defer func() {
			quitChan <- true
		}()
		go func() {
			for {
				select {
				case <-ticker:
					if transport.reader != nil {
						s := transport.reader.Monitor.Status()
						fmt.Printf("\rProgress: %.2f KiB/s, %d / %d (%s) ETA %s", float32(s.CurRate)/1000, s.Bytes, filesize, s.Progress, s.TimeRem)
					}
				case <-quitChan:
					return
				}
			}
		}()
	}
	client, err := buildOAuthHTTPClient(ctx, youtube.YoutubeUploadScope)
	if err != nil {
		log.Fatalf("Error building OAuth client: %v", err)
	}

	service, err := youtube.New(client)
	if err != nil {
		log.Fatalf("Error creating YouTube client: %v", err)
	}

	upload := &youtube.Video{
		Snippet: &youtube.VideoSnippet{
			Title:       *title,
			Description: *description,
			CategoryId:  *category,
		},
		Status: &youtube.VideoStatus{PrivacyStatus: *privacy},
	}

	// The API returns a 400 Bad Request response if tags is an empty string.
	if strings.Trim(*keywords, "") != "" {
		upload.Snippet.Tags = strings.Split(*keywords, ",")
	}

	call := service.Videos.Insert("snippet,status", upload)

	var option googleapi.MediaOption
	var video *youtube.Video

	// our RoundTrip gets bypassed if the filesize < DefaultUploadChunkSize
	if googleapi.DefaultUploadChunkSize > filesize {
		option = googleapi.ChunkSize(int(filesize / 2))
	} else {
		option = googleapi.ChunkSize(googleapi.DefaultUploadChunkSize)
	}

	fmt.Printf("Uploading file '%s'...\n", *filename)

	video, err = call.Media(reader, option).Do()
	if err != nil {
		if video != nil {
			log.Fatalf("Error making YouTube API call: %v, %v", err, video.HTTPStatusCode)
		} else {
			log.Fatalf("Error making YouTube API call: %v", err)
		}
	}
	fmt.Printf("\nUpload successful! Video ID: %v\n", video.Id)
}

type limitTransport struct {
	rt       http.RoundTripper
	reader   *flowrate.Reader
	filesize int64
}

func (t *limitTransport) RoundTrip(r *http.Request) (res *http.Response, err error) {

	// FIXME need a better way to detect which roundtrip is the media upload
	if r.ContentLength > 1000 {
		var monitor *flowrate.Monitor

		if t.reader != nil {
			monitor = t.reader.Monitor
		}
		t.reader = flowrate.NewReader(r.Body, int64(*rate*1000))

		if monitor != nil {
			// carry over stats to new limiter
			t.reader.Monitor = monitor
		} else {
			t.reader.Monitor.SetTransferSize(t.filesize)
		}
		r.Body = ioutil.NopCloser(t.reader)
	}

	return t.rt.RoundTrip(r)
}
