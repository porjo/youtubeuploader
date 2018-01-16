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
	"encoding/json"
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
	filename     = flag.String("filename", "", "Filename to upload. Can be a URL")
	thumbnail    = flag.String("thumbnail", "", "Thumbnail to upload. Can be a URL")
	title        = flag.String("title", "Video Title", "Video title")
	description  = flag.String("description", "uploaded by youtubeuploader", "Video description")
	categoryId   = flag.String("categoryId", "", "Video category Id")
	tags         = flag.String("tags", "", "Comma separated list of video tags")
	privacy      = flag.String("privacy", "private", "Video privacy status")
	quiet        = flag.Bool("quiet", false, "Suppress progress indicator")
	rate         = flag.Int("ratelimit", 0, "Rate limit upload in kbps. No limit by default")
	metaJSON     = flag.String("metaJSON", "", "JSON file containing title,description,tags etc (optional)")
	headlessAuth = flag.Bool("headlessAuth", false, "set this if host does not have browser available for oauth authorisation step")
)

type Video struct {
	// snippet
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	CategoryId  string   `json:"categoryId,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	// status
	PrivacyStatus string `json:"privacyStatus,omitempty"`

	// recording details
	Location            *youtube.GeoPoint `json:"location,omitempty"`
	LocationDescription string            `json:"locationDescription, omitempty"`
	RecordingDate       Date              `json:"recordingDate, omitempty"`
}

const inputDateLayout = "2006-01-02"
const outputDateLayout = "2006-01-02T15:04:05.000Z" //ISO 8601 (YYYY-MM-DDThh:mm:ss.sssZ)

type Date struct {
	time.Time
}

func main() {
	flag.Parse()

	if *filename == "" {
		fmt.Printf("You must provide a filename of a video file to upload\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	reader, filesize := Open(*filename)
	defer reader.Close()

	var thumbReader io.ReadCloser
	if *thumbnail != "" {
		thumbReader, _ = Open(*thumbnail)
		defer thumbReader.Close()
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
						curRate := float32(s.CurRate)
						if curRate >= 125000 {
							fmt.Printf("\rProgress: %8.2f Mbps, %d / %d (%s) ETA %8s", curRate/125000, s.Bytes, filesize, s.Progress, s.TimeRem)
						} else {
							fmt.Printf("\rProgress: %8.2f kbps, %d / %d (%s) ETA %8s", curRate/125, s.Bytes, filesize, s.Progress, s.TimeRem)
						}
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
		Snippet:          &youtube.VideoSnippet{},
		RecordingDetails: &youtube.VideoRecordingDetails{},
		Status:           &youtube.VideoStatus{},
	}

	// attempt to load from meta JSON, otherwise use values specified from command line flags
	if *metaJSON != "" {
		video := Video{}
		file, e := ioutil.ReadFile(*metaJSON)
		if e != nil {
			fmt.Printf("Could not read metaJSON file '%s': %s\n", *metaJSON, e)
			fmt.Println("Will use command line flags instead")
			goto errJump
		}

		e = json.Unmarshal(file, &video)
		if e != nil {
			fmt.Printf("Could not read metaJSON file '%s': %s\n", *metaJSON, e)
			fmt.Println("Will use command line flags instead")
			goto errJump
		}

		upload.Snippet.Tags = video.Tags
		upload.Snippet.Title = video.Title
		upload.Snippet.Description = video.Description
		upload.Snippet.CategoryId = video.CategoryId
		if video.PrivacyStatus != "" {
			upload.Status.PrivacyStatus = video.PrivacyStatus
		}
		if video.Location != nil {
			upload.RecordingDetails.Location = video.Location
		}
		if video.LocationDescription != "" {
			upload.RecordingDetails.LocationDescription = video.LocationDescription
		}
		if !video.RecordingDate.IsZero() {
			upload.RecordingDetails.RecordingDate = video.RecordingDate.Format(outputDateLayout)
		}

	errJump:
	}

	if upload.Status.PrivacyStatus == "" {
		upload.Status = &youtube.VideoStatus{PrivacyStatus: *privacy}
	}
	if upload.Snippet.Tags == nil && strings.Trim(*tags, "") != "" {
		upload.Snippet.Tags = strings.Split(*tags, ",")
	}
	if upload.Snippet.Title == "" {
		upload.Snippet.Title = *title
	}
	if upload.Snippet.Description == "" {
		upload.Snippet.Description = *description
	}
	if upload.Snippet.CategoryId == "" && *categoryId != "" {
		upload.Snippet.CategoryId = *categoryId
	}

	call := service.Videos.Insert("snippet,status,recordingDetails", upload)

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

	if thumbReader != nil {
		log.Printf("Uploading thumbnail '%s'...\n", *thumbnail)
		_, err = service.Thumbnails.Set(video.Id).Media(thumbReader).Do()
		if err != nil {
			log.Fatalf("Error making YouTube API call: %v", err)
		}
		fmt.Printf("Thumbnail uploaded!\n")
	}
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

		// kbit/s to B/s = 1000/8 = 125
		t.reader = flowrate.NewReader(r.Body, int64(*rate*125))

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

func (d *Date) UnmarshalJSON(b []byte) (err error) {
	s := string(b)
	s = s[1 : len(s)-1]
	d.Time, err = time.Parse(inputDateLayout, s)
	return
}

func Open(filename string) (reader io.ReadCloser, filesize int64) {
	if strings.HasPrefix(filename, "http") {
		resp, err := http.Head(filename)
		if err != nil {
			log.Fatalf("Error opening %v: %v", filename, err)
		}
		lenStr := resp.Header.Get("content-length")
		if lenStr != "" {
			filesize, err = strconv.ParseInt(lenStr, 10, 64)
			if err != nil {
				log.Fatal(err)
			}
		}

		resp, err = http.Get(filename)
		if err != nil {
			log.Fatalf("Error opening %v: %v", filename, err)
		}
		if resp.ContentLength != 0 {
			filesize = resp.ContentLength
		}
		reader = resp.Body
		return
	}

	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Error opening %v: %v", filename, err)
	}

	fileInfo, err := file.Stat()
	if err != nil {
		log.Fatalf("Error stating file %v: %v", filename, err)
	}

	return file, fileInfo.Size()
}
