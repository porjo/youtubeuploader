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

package youtubeuploader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/porjo/youtubeuploader/internal/limiter"
	"github.com/porjo/youtubeuploader/internal/progress"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

func Run(ctx context.Context, transport *limiter.LimitTransport, config Config, videoReader io.ReadCloser) error {

	if config.Filename == "" {
		return fmt.Errorf("filename must be specified")
	}
	if transport == nil {
		return fmt.Errorf("transport cannot be nil")
	}
	if videoReader == nil {
		return fmt.Errorf("videoReader cannot be nil")
	}

	var thumbReader io.ReadCloser
	if config.Thumbnail != "" {
		r, _, err := Open(config.Thumbnail, IMAGE)
		if err != nil {
			return err
		}
		thumbReader = r
		defer thumbReader.Close()
	}

	var captionReader io.ReadCloser
	if config.Caption != "" {
		r, _, err := Open(config.Caption, CAPTION)
		if err != nil {
			return err
		}
		captionReader = r
		defer captionReader.Close()
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
		Transport: transport,
	})

	var progressInterval time.Duration
	if !config.Quiet {
		progressInterval = time.Second
	}

	prog, err := progress.NewProgress(transport, progressInterval)
	if err != nil {
		return err
	}

	signalChan := make(chan os.Signal, 1)
	SetSignalNotify(signalChan)
	go prog.Run(ctx, signalChan)

	client, err := BuildOAuthHTTPClient(
		ctx,
		[]string{youtube.YoutubeUploadScope, youtube.YoutubepartnerScope, youtube.YoutubeScope},
		config.OAuthPort,
	)
	if err != nil {
		return fmt.Errorf("error building OAuth client: %w", err)
	}

	videoMeta, uploadVideo, err := LoadVideoMeta(config)
	if err != nil {
		return fmt.Errorf("error loading video meta data: %w", err)
	}

	service, err := youtube.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("error creating Youtube client: %w", err)
	}

	if config.Filename == "-" {
		fmt.Printf("Uploading file from pipe\n")
	} else {
		fmt.Printf("Uploading file %q\n", config.Filename)
	}

	var option googleapi.MediaOption
	var resultVideo *youtube.Video

	option = googleapi.ChunkSize(config.Chunksize)

	call := service.Videos.Insert([]string{"snippet", "status", "localizations", "recordingDetails"}, uploadVideo)
	if config.SendFileName && config.Filename != "-" {
		filetitle := filepath.Base(config.Filename)
		slog.Debug("adding file name to request", "file", filetitle)
		call.Header().Set("Slug", filetitle)
	}
	resultVideo, err = call.NotifySubscribers(config.NotifySubscribers).Media(videoReader, option).Do()
	if err != nil {
		if resultVideo != nil {
			return fmt.Errorf("error making YouTube API call: %w, %v", err, resultVideo.HTTPStatusCode)
		} else {
			return fmt.Errorf("error making YouTube API call: %w", err)
		}
	}
	fmt.Printf("\nUpload successful! Video ID: %v\n", resultVideo.Id)

	if config.MetaJSONOut != "" {
		JSONOut, _ := json.Marshal(resultVideo)
		err = os.WriteFile(config.MetaJSONOut, JSONOut, 0666)
		if err != nil {
			return fmt.Errorf("error writing to video metadata file %q: %w", config.MetaJSONOut, err)
		}
		fmt.Printf("Wrote video metadata to file %q\n", config.MetaJSONOut)
	}

	if thumbReader != nil {
		fmt.Printf("Uploading thumbnail %q...\n", config.Thumbnail)
		_, err = service.Thumbnails.Set(resultVideo.Id).Media(thumbReader).Do()
		if err != nil {
			return fmt.Errorf("error making YouTube API call: %w", err)
		}
	}

	// Insert caption
	if captionReader != nil {
		fmt.Printf("Uploading caption %q...\n", config.Caption)
		captionObj := &youtube.Caption{
			Snippet: &youtube.CaptionSnippet{},
		}
		captionObj.Snippet.VideoId = resultVideo.Id
		captionObj.Snippet.Language = config.Language
		captionObj.Snippet.Name = config.Language
		captionInsert := service.Captions.Insert([]string{"snippet"}, captionObj).Sync(true)
		captionRes, err := captionInsert.Media(captionReader).Do()
		if err != nil {
			if captionRes != nil {
				return fmt.Errorf("error inserting caption: %w, %v", err, captionRes.HTTPStatusCode)
			} else {
				return fmt.Errorf("error inserting caption: %w", err)
			}
		}
	}

	plx := &Playlistx{}
	if uploadVideo.Status.PrivacyStatus != "" {
		plx.PrivacyStatus = uploadVideo.Status.PrivacyStatus
	}

	if len(videoMeta.PlaylistIDs) > 0 {
		plx.Title = ""
		for _, pid := range videoMeta.PlaylistIDs {
			plx.Id = pid
			err = plx.AddVideoToPlaylist(service, resultVideo.Id)
			if err != nil {
				return fmt.Errorf("error adding video to playlist: %w", err)
			}
		}
	}

	if len(videoMeta.PlaylistTitles) > 0 {
		plx.Id = ""
		for _, title := range videoMeta.PlaylistTitles {
			plx.Title = title
			err = plx.AddVideoToPlaylist(service, resultVideo.Id)
			if err != nil {
				return fmt.Errorf("error adding video to playlist: %w", err)
			}
		}
	}

	return nil
}
