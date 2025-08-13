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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/youtube/v3"
)

const (
	ytDateLayout        = "2006-01-02T15:04:05.000Z" // ISO 8601 (YYYY-MM-DDThh:mm:ss.sssZ)
	inputDateLayout     = "2006-01-02"
	inputDatetimeLayout = "2006-01-02T15:04:05-07:00"

	UNKNOWN MediaType = iota
	VIDEO
	IMAGE
	CAPTION
)

type Config struct {
	Filename          string
	Thumbnail         string
	Caption           string
	Title             string
	Description       string
	Language          string
	CategoryId        string
	Tags              string
	Privacy           string
	Quiet             bool
	RateLimit         int
	MetaJSON          string
	MetaJSONOut       string
	LimitBetween      string
	PlaylistIDs       []string
	OAuthPort         int
	ShowAppVersion    bool
	Chunksize         int
	NotifySubscribers bool
	SendFileName      bool
	RecordingDate     Date
}

type MediaType int

type Date struct {
	time.Time
}

func LoadVideoMeta(config Config, video *youtube.Video) (*VideoMeta, error) {
	videoMeta := &VideoMeta{}

	video.Snippet = &youtube.VideoSnippet{}
	video.RecordingDetails = &youtube.VideoRecordingDetails{}
	video.Status = &youtube.VideoStatus{}

	// Force send some boolean values.
	// Without this, defaults on the Youtube side are used which can have unexpected results.
	// See: https://github.com/porjo/youtubeuploader/issues/132
	video.Status.ForceSendFields = []string{"SelfDeclaredMadeForKids", "ContainsSyntheticMedia"}

	// attempt to load from meta JSON, otherwise use values specified from command line flags
	if config.MetaJSON != "" {
		file, e := os.ReadFile(config.MetaJSON)
		if e != nil {
			e2 := fmt.Errorf("error reading file %q: %w", config.MetaJSON, e)
			return nil, e2
		}

		e = json.Unmarshal(file, &videoMeta)
		if e != nil {
			e2 := fmt.Errorf("error parsing file %q: %w", config.MetaJSON, e)
			return nil, e2
		}

		video.Snippet.Tags = videoMeta.Tags
		video.Snippet.Title = videoMeta.Title
		video.Snippet.Description = videoMeta.Description
		video.Snippet.CategoryId = videoMeta.CategoryId
		// Location has been deprecated by Google
		// see: https://developers.google.com/youtube/v3/revision_history#release_notes_06_01_2017
		/*
			if videoMeta.Location != nil {
				video.RecordingDetails.Location = videoMeta.Location
			}
			if videoMeta.LocationDescription != "" {
				video.RecordingDetails.LocationDescription = videoMeta.LocationDescription
			}
		*/
		if !videoMeta.RecordingDate.IsZero() {
			video.RecordingDetails.RecordingDate = videoMeta.RecordingDate.UTC().Format(ytDateLayout)
		}

		// status
		if videoMeta.PrivacyStatus != "" {
			video.Status.PrivacyStatus = videoMeta.PrivacyStatus
		}
		if videoMeta.MadeForKids {
			video.Status.SelfDeclaredMadeForKids = true
		}
		if videoMeta.Embeddable {
			video.Status.Embeddable = true
		}
		if videoMeta.License != "" {
			video.Status.License = videoMeta.License
		}
		if videoMeta.PublicStatsViewable {
			video.Status.PublicStatsViewable = videoMeta.PublicStatsViewable
		}
		if videoMeta.ContainsSyntheticMedia {
			video.Status.ContainsSyntheticMedia = true
		}
		if !videoMeta.PublishAt.IsZero() {
			if video.Status.PrivacyStatus != "private" {
				fmt.Printf("publishAt can only be used when privacyStatus is 'private'. Ignoring publishAt...\n")
			} else {
				if videoMeta.PublishAt.Before(time.Now()) {
					fmt.Printf("publishAt (%s) was in the past!? Publishing now instead...\n", videoMeta.PublishAt)
				} else {
					video.Status.PublishAt = videoMeta.PublishAt.UTC().Format(ytDateLayout)
				}
			}
		}

		if videoMeta.Language != "" {
			video.Snippet.DefaultLanguage = videoMeta.Language
			video.Snippet.DefaultAudioLanguage = videoMeta.Language
		}
	}

	if video.Status.PrivacyStatus == "" {
		video.Status.PrivacyStatus = config.Privacy
	}
	if video.Snippet.Tags == nil && strings.Trim(config.Tags, "") != "" {
		video.Snippet.Tags = strings.Split(config.Tags, ",")
	}
	if video.Snippet.Title == "" {
		video.Snippet.Title = config.Title
	}
	if video.Snippet.Description == "" {
		// expand newlines
		descriptionExpanded, err := strconv.Unquote(`"` + config.Description + `"`)
		if err != nil {
			video.Snippet.Description = config.Description
		} else {
			video.Snippet.Description = descriptionExpanded
		}
	}
	if video.Snippet.CategoryId == "" && config.CategoryId != "" {
		video.Snippet.CategoryId = config.CategoryId
	}
	if video.Snippet.DefaultLanguage == "" && config.Language != "" {
		video.Snippet.DefaultLanguage = config.Language
	}
	if video.Snippet.DefaultAudioLanguage == "" && config.Language != "" {
		video.Snippet.DefaultAudioLanguage = config.Language
	}

	if video.RecordingDetails.RecordingDate == "" && !config.RecordingDate.IsZero() {
		video.RecordingDetails.RecordingDate = config.RecordingDate.UTC().Format(ytDateLayout)
	}

	// combine cli flag playistIDs and metaJSON playlistIDs. Remove any duplicates
	playlistIDs := slices.Concat(config.PlaylistIDs, videoMeta.PlaylistIDs)
	slices.Sort(playlistIDs)
	videoMeta.PlaylistIDs = slices.Compact(playlistIDs)

	return videoMeta, nil
}

func Open(filename string, mediaType MediaType) (io.ReadCloser, int64, error) {
	var reader io.ReadCloser
	var filesize int64
	var err error
	if strings.HasPrefix(filename, "http") {
		var resp *http.Response
		resp, err = http.Head(filename)
		if err != nil {
			return reader, 0, fmt.Errorf("error opening %q: %w", filename, err)
		}
		lenStr := resp.Header.Get("content-length")
		if lenStr != "" {
			filesize, err = strconv.ParseInt(lenStr, 10, 64)
			if err != nil {
				return reader, filesize, err
			}
		}

		resp, err = http.Get(filename)
		if err != nil {
			return reader, 0, fmt.Errorf("error opening %q: %w", filename, err)
		}
		// Go doc: When err is nil, resp always contains a non-nil resp.Body. Caller should close resp.Body when done reading from it.
		defer resp.Body.Close()
		if resp.ContentLength > 0 {
			filesize = resp.ContentLength
		}
		reader = resp.Body
	} else if filename == "-" {
		reader = os.Stdin
	} else {
		var file *os.File
		var fileInfo os.FileInfo
		file, err = os.Open(filename)
		if err != nil {
			return reader, 0, fmt.Errorf("error opening %q: %w", filename, err)
		}

		fileInfo, err = file.Stat()
		if err != nil {
			return reader, 0, fmt.Errorf("error stat'ing %q: %w", filename, err)
		}

		// check the file looks like the media type it is supposed to be
		buf := make([]byte, 512)
		_, err = file.Read(buf)
		if err != nil {
			return reader, 0, fmt.Errorf("error reading %q: %w", filename, err)
		}
		_, err = file.Seek(0, 0)
		if err != nil {
			return reader, 0, fmt.Errorf("error reading %q: %w", filename, err)
		}
		contentType := http.DetectContentType(buf)
		switch mediaType {
		case VIDEO:
			if !strings.HasPrefix(contentType, "video") && contentType != "application/octet-stream" {
				fmt.Printf("WARNING: input file %q doesn't appear to be a video. It has content type %q\n", filename, contentType)
			}
		case IMAGE:
			if !strings.HasPrefix(contentType, "image") && contentType != "application/octet-stream" {
				fmt.Printf("WARNING: input file %q doesn't appear to be an image. It has content type %q\n", filename, contentType)
			}
		}

		reader = file
		filesize = fileInfo.Size()

	}
	return reader, filesize, err
}

func (d *Date) UnmarshalJSON(b []byte) (err error) {
	s := string(b)
	s = s[1 : len(s)-1]
	err = d.parse(s)
	return
}

func (d *Date) Set(s string) (err error) {
	err = d.parse(s)
	return
}

func (d *Date) parse(s string) (err error) {
	// support ISO 8601 date only, and date + time
	if strings.ContainsAny(s, ":") {
		d.Time, err = time.Parse(inputDatetimeLayout, s)
	} else {
		d.Time, err = time.Parse(inputDateLayout, s)
	}
	return
}
