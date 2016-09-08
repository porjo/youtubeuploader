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
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mxk/go-flowrate/flowrate"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/youtube/v3"
)

var (
	filename     = flag.String("filename", "", "Filename to upload. Can be a URL")
	title        = flag.String("title", "Video Title", "Video title")
	description  = flag.String("description", "uploaded by youtubeuploader", "Video description")
	category     = flag.String("category", "", "Video category")
	keywords     = flag.String("keywords", "", "Comma separated list of video keywords")
	privacy      = flag.String("privacy", "private", "Video privacy status")
	showProgress = flag.Bool("progress", true, "Show progress indicator")
	rate         = flag.Int("ratelimit", 0, "Rate limit upload in KB/s. No limit by default")
)

type customReader struct {
	Reader io.Reader

	bytes     int64
	lapTime   time.Time
	startTime time.Time
	filesize  int64
}

func main() {
	flag.Parse()

	if *filename == "" {
		log.Fatalf("You must provide a filename of a video file to upload")
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

	var option googleapi.MediaOption
	if filesize < (1024 * 1024 * 10) {
		// on small uploads (<10MB), set minimum chunk size so we can see progress
		option = googleapi.ChunkSize(1)
	} else {
		// on larger uploads, use the default chunk size for best performance
		option = googleapi.ChunkSize(googleapi.DefaultUploadChunkSize)
	}

	transport := limitTransport{}
	transport.filesize = filesize
	client, err := buildOAuthHTTPClient(youtube.YoutubeUploadScope)
	if err != nil {
		log.Fatalf("Error building OAuth client: %v", err)
	}
	client.Transport = transport

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

	var video *youtube.Video
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
	http.RoundTripper
	filesize int64
}

func (t limitTransport) RoundTrip(r *http.Request) (res *http.Response, err error) {
	body := flowrate.NewReader(r.Body, int64(*rate))
	body.Monitor.SetTransferSize(t.filesize)
	r.Body = body
	return t.RoundTripper.RoundTrip(r)
}
