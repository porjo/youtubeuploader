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
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/youtube/v3"
)

type chanChan chan chan struct{}

var (
	filename          = flag.String("filename", "", "Filename to upload. Can be a URL. Read from stdin with '-'")
	thumbnail         = flag.String("thumbnail", "", "Thumbnail to upload. Can be a URL")
	caption           = flag.String("caption", "", "Caption to upload. Can be a URL")
	title             = flag.String("title", "Video Title", "Video title")
	description       = flag.String("description", "uploaded by youtubeuploader", "Video description")
	language          = flag.String("language", "en", "Video language")
	categoryId        = flag.String("categoryId", "", "Video category Id")
	tags              = flag.String("tags", "", "Comma separated list of video tags")
	privacy           = flag.String("privacy", "private", "Video privacy status")
	quiet             = flag.Bool("quiet", false, "Suppress progress indicator")
	rate              = flag.Int("ratelimit", 0, "Rate limit upload in Kbps. No limit by default")
	metaJSON          = flag.String("metaJSON", "", "JSON file containing title,description,tags etc (optional)")
	limitBetween      = flag.String("limitBetween", "", "Only rate limit between these times e.g. 10:00-14:00 (local time zone)")
	headlessAuth      = flag.Bool("headlessAuth", false, "set this if no browser available for the oauth authorisation step")
	oAuthPort         = flag.Int("oAuthPort", 8080, "TCP port to listen on when requesting an oAuth token")
	showAppVersion    = flag.Bool("v", false, "show version")
	chunksize         = flag.Int("chunksize", googleapi.DefaultUploadChunkSize, "size (in bytes) of each upload chunk. A zero value will cause all data to be uploaded in a single request")
	notifySubscribers = flag.Bool("notify", true, "notify channel subscribers of new video")

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
		fmt.Printf("You must provide a filename of a video file to upload\n")
		flag.PrintDefaults()
		os.Exit(1)
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

	var quitChan chanChan
	if !*quiet {
		quitChan = make(chanChan)
		go func() {
			Progress(quitChan, transport, filesize)
		}()
	}
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

	call := service.Videos.Insert("snippet,status,recordingDetails", upload)
	video, err = call.NotifySubscribers(*notifySubscribers).Media(reader, option).Do()

	if quitChan != nil {
		quit := make(chan struct{})
		quitChan <- quit
		<-quit
	}

	if err != nil {
		if video != nil {
			log.Fatalf("Error making YouTube API call: %v, %v", err, video.HTTPStatusCode)
		} else {
			log.Fatalf("Error making YouTube API call: %v", err)
		}
	}
	fmt.Printf("Upload successful! Video ID: %v\n", video.Id)

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
		captionInsert := service.Captions.Insert("snippet", captionObj).Sync(true)
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
	// PlaylistID is deprecated in favour of PlaylistIDs
	if videoMeta.PlaylistID != "" {
		plx.Id = videoMeta.PlaylistID
		err = plx.AddVideoToPlaylist(service, video.Id)
		if err != nil {
			log.Fatalf("Error adding video to playlist: %s", err)
		}
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
