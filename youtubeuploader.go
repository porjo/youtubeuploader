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

type VideoMeta struct {
	// snippet
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	CategoryId  string   `json:"categoryId,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	// status
	PrivacyStatus       string `json:"privacyStatus,omitempty"`
	Embeddable          bool   `json:"embeddable,omitempty"`
	License             string `json:"license,omitempty"`
	PublicStatsViewable bool   `json:"publicStatsViewable,omitempty"`
	PublishAt           string `json:"publishAt,omitempty"`

	// recording details
	Location            *youtube.GeoPoint `json:"location,omitempty"`
	LocationDescription string            `json:"locationDescription, omitempty"`
	RecordingDate       Date              `json:"recordingDate, omitempty"`

	PlaylistID string `json:"playlistId, omitempty"`
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

	var quitChan chan chan struct{}

	if !*quiet {
		ticker := time.Tick(time.Second)
		quitChan = make(chan chan struct{})
		go func() {
			var erase int
			for {
				select {
				case <-ticker:
					if transport.reader != nil {
						s := transport.reader.Monitor.Status()
						curRate := float32(s.CurRate)
						var status string
						if curRate >= 125000 {
							status = fmt.Sprintf("Progress: %8.2f Mbps, %d / %d (%s) ETA %8s", curRate/125000, s.Bytes, filesize, s.Progress, s.TimeRem)
						} else {
							status = fmt.Sprintf("Progress: %8.2f kbps, %d / %d (%s) ETA %8s", curRate/125, s.Bytes, filesize, s.Progress, s.TimeRem)
						}
						fmt.Printf("\r%s\r%s", strings.Repeat(" ", erase), status)
						erase = len(status)
					}
				case ch := <-quitChan:
					close(ch)
					return
				}
			}
		}()
	}
	client, err := buildOAuthHTTPClient(ctx, []string{youtube.YoutubeUploadScope, youtube.YoutubeScope})
	if err != nil {
		log.Fatalf("Error building OAuth client: %v", err)
	}

	upload := &youtube.Video{
		Snippet:          &youtube.VideoSnippet{},
		RecordingDetails: &youtube.VideoRecordingDetails{},
		Status:           &youtube.VideoStatus{},
	}

	videoMeta := LoadVideoMeta(*metaJSON, upload)

	service, err := youtube.New(client)
	if err != nil {
		log.Fatalf("Error creating Youtube client: %s", err)
	}

	if upload.Status.PrivacyStatus == "" {
		upload.Status.PrivacyStatus = *privacy
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

	fmt.Printf("Uploading file '%s'...\n", *filename)

	var option googleapi.MediaOption
	var video *youtube.Video

	// our RoundTrip gets bypassed if the filesize < DefaultUploadChunkSize
	if googleapi.DefaultUploadChunkSize > filesize {
		option = googleapi.ChunkSize(int(filesize / 2))
	} else {
		option = googleapi.ChunkSize(googleapi.DefaultUploadChunkSize)
	}

	call := service.Videos.Insert("snippet,status,recordingDetails", upload)
	video, err = call.Media(reader, option).Do()

	quit := make(chan struct{})
	quitChan <- quit
	<-quit

	if err != nil {
		if video != nil {
			log.Fatalf("Error making YouTube API call: %v, %v", err, video.HTTPStatusCode)
		} else {
			log.Fatalf("Error making YouTube API call: %v", err)
		}
	}
	fmt.Printf("\nUpload successful! Video ID: %v\n", video.Id)

	if videoMeta.PlaylistID != "" {
		err = AddVideoToPlaylist(service, videoMeta.PlaylistID, video.Id)
		if err != nil {
			log.Fatalf("Error adding video to playlist: %s", err)
		}
	}
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

func AddVideoToPlaylist(service *youtube.Service, playlistID, videoID string) (err error) {
	listCall := service.Playlists.List("snippet,contentDetails")
	listCall = listCall.Mine(true)
	response, err := listCall.Do()
	if err != nil {
		return fmt.Errorf("error retrieving playlists: %s", err)
	}

	var playlist *youtube.Playlist
	for _, pl := range response.Items {
		if pl.Id == playlistID {
			playlist = pl
			break
		}
	}

	// TODO: handle creation of playlist
	if playlist == nil {
		return fmt.Errorf("playlist ID '%s' doesn't exist", playlistID)
	}

	playlistItem := &youtube.PlaylistItem{}
	playlistItem.Snippet = &youtube.PlaylistItemSnippet{PlaylistId: playlist.Id}
	playlistItem.Snippet.ResourceId = &youtube.ResourceId{
		VideoId: videoID,
		Kind:    "youtube#video",
	}

	insertCall := service.PlaylistItems.Insert("snippet", playlistItem)
	_, err = insertCall.Do()
	if err != nil {
		return fmt.Errorf("error inserting video into playlist: %s", err)
	}

	fmt.Printf("Video added to playlist '%s' (%s)\n", playlist.Snippet.Title, playlist.Id)

	return nil
}

func LoadVideoMeta(filename string, video *youtube.Video) (videoMeta VideoMeta) {
	// attempt to load from meta JSON, otherwise use values specified from command line flags
	if filename != "" {
		file, e := ioutil.ReadFile(filename)
		if e != nil {
			fmt.Printf("Could not read filename file '%s': %s\n", filename, e)
			fmt.Println("Will use command line flags instead")
			goto errJump
		}

		e = json.Unmarshal(file, &videoMeta)
		if e != nil {
			fmt.Printf("Could not read filename file '%s': %s\n", filename, e)
			fmt.Println("Will use command line flags instead")
			goto errJump
		}

		video.Snippet.Tags = videoMeta.Tags
		video.Snippet.Title = videoMeta.Title
		video.Snippet.Description = videoMeta.Description
		video.Snippet.CategoryId = videoMeta.CategoryId
		if videoMeta.PrivacyStatus != "" {
			video.Status.PrivacyStatus = videoMeta.PrivacyStatus
		}
		if videoMeta.Location != nil {
			video.RecordingDetails.Location = videoMeta.Location
		}
		if videoMeta.LocationDescription != "" {
			video.RecordingDetails.LocationDescription = videoMeta.LocationDescription
		}
		if !videoMeta.RecordingDate.IsZero() {
			video.RecordingDetails.RecordingDate = videoMeta.RecordingDate.Format(outputDateLayout)
		}
	}
errJump:

	if video.Status.PrivacyStatus == "" {
		video.Status = &youtube.VideoStatus{PrivacyStatus: *privacy}
	}
	if video.Snippet.Tags == nil && strings.Trim(*tags, "") != "" {
		video.Snippet.Tags = strings.Split(*tags, ",")
	}
	if video.Snippet.Title == "" {
		video.Snippet.Title = *title
	}
	if video.Snippet.Description == "" {
		video.Snippet.Description = *description
	}
	if video.Snippet.CategoryId == "" && *categoryId != "" {
		video.Snippet.CategoryId = *categoryId
	}

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
