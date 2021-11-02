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
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/youtube/v3"
)

const ytDateLayout = "2006-01-02T15:04:05.000Z" // ISO 8601 (YYYY-MM-DDThh:mm:ss.sssZ)
const inputDateLayout = "2006-01-02"
const inputDatetimeLayout = "2006-01-02T15:04:05-07:00"

type Date struct {
	time.Time
}

func LoadVideoMeta(filename string, video *youtube.Video) (*VideoMeta, error) {
	videoMeta := &VideoMeta{}
	// attempt to load from meta JSON, otherwise use values specified from command line flags
	if filename != "" {
		file, e := ioutil.ReadFile(filename)
		if e != nil {
			e2 := fmt.Errorf("Error reading file '%s': %s\n", filename, e)
			return nil, e2
		}

		e = json.Unmarshal(file, &videoMeta)
		if e != nil {
			e2 := fmt.Errorf("Error parsing file '%s': %s\n", filename, e)
			return nil, e2
		}

		video.Status = &youtube.VideoStatus{}
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
			video.Status.MadeForKids = true
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
		if !videoMeta.PublishAt.IsZero() {
			if video.Status.PrivacyStatus != "private" {
				fmt.Printf("publishAt can only be used when privacyStatus is 'private'. Ignoring publishAt...\n")
			} else {
				if videoMeta.PublishAt.Before(time.Now()) {
					fmt.Printf("publishAt (%s) was in the past!? Publishing now instead...\n", videoMeta.PublishAt)
					video.Status.PublishAt = time.Now().UTC().Format(ytDateLayout)
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
		video.Status.PrivacyStatus = *privacy
	}
	if video.Snippet.Tags == nil && strings.Trim(*tags, "") != "" {
		video.Snippet.Tags = strings.Split(*tags, ",")
	}
	if video.Snippet.Title == "" {
		video.Snippet.Title = *title
	}
	if video.Snippet.Description == "" {
		// expand newlines
		descriptionExpanded, err := strconv.Unquote(`"` + *description + `"`)
		if err != nil {
			video.Snippet.Description = *description
		} else {
			video.Snippet.Description = descriptionExpanded
		}
	}
	if video.Snippet.CategoryId == "" && *categoryId != "" {
		video.Snippet.CategoryId = *categoryId
	}
	if video.Snippet.DefaultLanguage == "" && *language != "" {
		video.Snippet.DefaultLanguage = *language
	}
	if video.Snippet.DefaultAudioLanguage == "" && *language != "" {
		video.Snippet.DefaultAudioLanguage = *language
	}

	return videoMeta, nil
}

func Open(filename string) (io.ReadCloser, int64, error) {
	var reader io.ReadCloser
	var filesize int64
	var err error
	if strings.HasPrefix(filename, "http") {
		var resp *http.Response
		resp, err = http.Head(filename)
		if err != nil {
			return reader, 0, fmt.Errorf("error opening %s: %s", filename, err)
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
			return reader, 0, fmt.Errorf("error opening %s: %s", filename, err)
		}
		if resp.ContentLength != 0 {
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
			return reader, 0, fmt.Errorf("error opening %s: %s", filename, err)
		}

		fileInfo, err = file.Stat()
		if err != nil {
			return reader, 0, fmt.Errorf("error stat'ing %s: %s", filename, err)
		}

		reader = file
		filesize = fileInfo.Size()
	}
	return reader, filesize, err
}

func (d *Date) UnmarshalJSON(b []byte) (err error) {
	s := string(b)
	s = s[1 : len(s)-1]
	// support ISO 8601 date only, and date + time
	if strings.ContainsAny(s, ":") {
		d.Time, err = time.Parse(inputDatetimeLayout, s)
	} else {
		d.Time, err = time.Parse(inputDateLayout, s)
	}
	return
}
