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

	"google.golang.org/api/youtube/v3"
)

var (
	filename     = flag.String("filename", "", "Filename to upload. Can be a URL")
	title        = flag.String("title", "Test Title", "Video title")
	description  = flag.String("description", "Test Description", "Video description")
	category     = flag.String("category", "22", "Video category")
	keywords     = flag.String("keywords", "", "Comma separated list of video keywords")
	privacy      = flag.String("privacy", "unlisted", "Video privacy status")
	showProgress = flag.Bool("progress", true, "Show progress indicator")
)

type customReader struct {
	ReadCloser io.ReadCloser

	bytes     int64
	lapTime   time.Time
	startTime time.Time
	fileSize  int64
}

func main() {
	flag.Parse()

	if *filename == "" {
		log.Fatalf("You must provide a filename of a video file to upload")
	}

	client, err := buildOAuthHTTPClient(youtube.YoutubeUploadScope)
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

	reader := &customReader{}

	if strings.HasPrefix(*filename, "http") {
		resp, err := http.Head(*filename)
		if err != nil {
			log.Fatalf("Error opening %v: %v", *filename, err)
		}
		lenStr := resp.Header.Get("content-length")
		if lenStr != "" {
			reader.fileSize, err = strconv.ParseInt(lenStr, 10, 64)
			if err != nil {
				log.Fatal(err)
			}
		}

		resp, err = http.Get(*filename)
		if err != nil {
			log.Fatalf("Error opening %v: %v", *filename, err)
		}
		reader.ReadCloser = resp.Body
	} else {
		file, err := os.Open(*filename)
		defer file.Close()
		if err != nil {
			log.Fatalf("Error opening %v: %v", *filename, err)
		}
		fileInfo, err := file.Stat()
		if err != nil {
			log.Fatalf("Error stating file %v: %v", *filename, err)
		}
		reader.fileSize = fileInfo.Size()
		reader.ReadCloser = file
	}

	response, err := call.Media(reader).Do()
	if err != nil {
		log.Fatalf("Error making YouTube API call: %v", err)
	}
	fmt.Printf("\nUpload successful! Video ID: %v\n", response.Id)
}

func (r *customReader) progress(Bps int64) {
	if r.fileSize > 0 {
		eta := time.Duration((r.fileSize-r.bytes)/Bps) * time.Second
		fmt.Printf("\rTransfer rate %.2f Mbps, %d / %d (%.2f%%) ETA %s", float32(Bps*8)/(1000*1000), r.bytes, r.fileSize, float32(r.bytes)/float32(r.fileSize)*100, eta)
	} else {
		fmt.Printf("\rTransfer rate %.2f Mbps, %d", float32(Bps*8)/(1000*1000), r.bytes)
	}
}

func (r *customReader) Read(p []byte) (n int, err error) {
	if r.startTime.IsZero() {
		r.startTime = time.Now()
	}
	if r.lapTime.IsZero() {
		r.lapTime = time.Now()
	}
	if len(p) == 0 {
		return 0, nil
	}
	n, err = r.ReadCloser.Read(p)
	r.bytes += int64(n)

	if time.Since(r.lapTime) >= time.Second || err == io.EOF {
		r.progress(r.bytes / int64(time.Since(r.startTime).Seconds()))
		r.lapTime = time.Now()
	}

	return n, err
}
