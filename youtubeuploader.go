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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/youtube/v3"
)

type chanChan chan chan struct{}

var (
	filename          = flag.String("filename", "", "video filename. Can be a URL. Read from stdin with '-'")
	thumbnail         = flag.String("thumbnail", "", "thumbnail filename. Can be a URL")
	caption           = flag.String("caption", "", "caption filename. Can be a URL")
	title             = flag.String("title", "", "video title")
	description       = flag.String("description", "uploaded by youtubeuploader", "video description")
	language          = flag.String("language", "en", "video language")
	categoryId        = flag.String("categoryId", "", "video category Id")
	tags              = flag.String("tags", "", "comma separated list of video tags")
	privacy           = flag.String("privacy", "private", "video privacy status")
	quiet             = flag.Bool("quiet", false, "suppress progress indicator")
	rate              = flag.Int("ratelimit", 0, "rate limit upload in Kbps. No limit by default")
	metaJSON          = flag.String("metaJSON", "", "JSON file containing title,description,tags etc (optional)")
	metaJSONout       = flag.String("metaJSONout", "", "filename to write uploaded video metadata into (optional)")
	limitBetween      = flag.String("limitBetween", "", "only rate limit between these times e.g. 10:00-14:00 (local time zone)")
	oAuthPort         = flag.Int("oAuthPort", 8080, "TCP port to listen on when requesting an oAuth token")
	showAppVersion    = flag.Bool("version", false, "show version")
	chunksize         = flag.Int("chunksize", googleapi.DefaultUploadChunkSize, "size (in bytes) of each upload chunk. A zero value will cause all data to be uploaded in a single request")
	notifySubscribers = flag.Bool("notify", true, "notify channel subscribers of new video. Specify '-notify=false' to disable.")
	debug             = flag.Bool("debug", false, "turn on verbose log output")

	// this is set by compile-time to match git tag
	appVersion string = "unknown"
)

func main() {
	flag.Parse()

	if *showAppVersion {
		fmt.Printf("Youtubeuploader version: %s\n", appVersion)
		os.Exit(0)
	}

	if *filename == "" {
		fmt.Printf("\nYou must provide a filename of a video file to upload\n")
		fmt.Printf("\nUsage:\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *title == "" {
		*title = strings.ReplaceAll(filepath.Base(*filename), filepath.Ext(*filename), "")
	}

	var reader io.ReadCloser
	var filesize int64
	var err error

	var limitRange limitRange
	if *limitBetween != "" {
		limitRange, err = parseLimitBetween(*limitBetween)
		if err != nil {
			fmt.Printf("Invalid value for -limitBetween: %v", err)
			os.Exit(1)
		}
	}

	reader, filesize, err = Open(*filename)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	var thumbReader io.ReadCloser
	if *thumbnail != "" {
		thumbReader, _, err = Open(*thumbnail)
		if err != nil {
			log.Fatal(err)
		}
		defer thumbReader.Close()
	}

	var captionReader io.ReadCloser
	if *caption != "" {
		captionReader, _, err = Open(*caption)
		if err != nil {
			log.Fatal(err)
		}
		defer captionReader.Close()
	}

	ctx := context.Background()
	transport := &limitTransport{rt: http.DefaultTransport, lr: limitRange, filesize: filesize}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
		Transport: transport,
	})

	p := &Progress{Quiet: *quiet, Transport: transport, Filesize: filesize}
	signalChan := make(chan os.Signal, 1)
	SetSignalNotify(signalChan)
	quitChan := make(chanChan)
	go p.Progress(quitChan, signalChan)

	client, err := buildOAuthHTTPClient(ctx, []string{youtube.YoutubeUploadScope, youtube.YoutubepartnerScope, youtube.YoutubeScope})
	if err != nil {
		log.Fatalf("Error building OAuth client: %v", err)
	}

	upload := &youtube.Video{
		Snippet:          &youtube.VideoSnippet{},
		RecordingDetails: &youtube.VideoRecordingDetails{},
		Status:           &youtube.VideoStatus{},
	}

	videoMeta, err := LoadVideoMeta(*metaJSON, upload)
	if err != nil {
		log.Fatalf("Error loading video meta data: %s", err)
	}

	service, err := youtube.New(client)
	if err != nil {
		log.Fatalf("Error creating Youtube client: %s", err)
	}

	if *filename == "-" {
		fmt.Printf("Uploading file from pipe\n")
	} else {
		fmt.Printf("Uploading file '%s'\n", *filename)
	}

	var option googleapi.MediaOption
	var video *youtube.Video

	option = googleapi.ChunkSize(*chunksize)

	call := service.Videos.Insert([]string{"snippet", "status", "recordingDetails"}, upload)
	video, err = call.NotifySubscribers(*notifySubscribers).Media(reader, option).Do()

	quit := make(chan struct{})
	quitChan <- quit
	// wait here until quit gets closed
	<-quit

	if err != nil {
		if video != nil {
			log.Fatalf("Error making YouTube API call: %v, %v", err, video.HTTPStatusCode)
		} else {
			log.Fatalf("Error making YouTube API call: %v", err)
		}
	}
	fmt.Printf("Upload successful! Video ID: %v\n", video.Id)

	if *metaJSONout != "" {
		JSONOut, _ := json.Marshal(video)
		err = os.WriteFile(*metaJSONout, JSONOut, 0666)
		if err != nil {
			log.Fatalf("Error writing to video metadata file '%s': %s\n", *metaJSONout, err)
		}
		fmt.Printf("Wrote video metadata to file '%s'\n", *metaJSONout)
	}

	if thumbReader != nil {
		log.Printf("Uploading thumbnail '%s'...\n", *thumbnail)
		_, err = service.Thumbnails.Set(video.Id).Media(thumbReader).Do()
		if err != nil {
			log.Fatalf("Error making YouTube API call: %v", err)
		}
		fmt.Printf("Thumbnail uploaded!\n")
	}

	// Insert caption
	if captionReader != nil {
		captionObj := &youtube.Caption{
			Snippet: &youtube.CaptionSnippet{},
		}
		captionObj.Snippet.VideoId = video.Id
		captionObj.Snippet.Language = *language
		captionObj.Snippet.Name = *language
		captionInsert := service.Captions.Insert([]string{"snippet"}, captionObj).Sync(true)
		captionRes, err := captionInsert.Media(captionReader).Do()
		if err != nil {
			if captionRes != nil {
				log.Fatalf("Error inserting caption: %v, %v", err, captionRes.HTTPStatusCode)
			} else {
				log.Fatalf("Error inserting caption: %v", err)
			}
		}
		fmt.Printf("Caption uploaded!\n")
	}

	plx := &Playlistx{}
	if upload.Status.PrivacyStatus != "" {
		plx.PrivacyStatus = upload.Status.PrivacyStatus
	}

	if len(videoMeta.PlaylistIDs) > 0 {
		plx.Title = ""
		for _, pid := range videoMeta.PlaylistIDs {
			plx.Id = pid
			err = plx.AddVideoToPlaylist(service, video.Id)
			if err != nil {
				log.Fatalf("Error adding video to playlist: %s", err)
			}
		}
	}

	if len(videoMeta.PlaylistTitles) > 0 {
		plx.Id = ""
		for _, title := range videoMeta.PlaylistTitles {
			plx.Title = title
			err = plx.AddVideoToPlaylist(service, video.Id)
			if err != nil {
				log.Fatalf("Error adding video to playlist: %s", err)
			}
		}
	}
}
