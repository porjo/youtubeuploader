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
	"log"
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

func LoadVideoMeta(filename string, video *youtube.Video) (videoMeta VideoMeta) {
	// attempt to load from meta JSON, otherwise use values specified from command line flags
	if filename != "" {
		file, e := ioutil.ReadFile(filename)
		if e != nil {
			fmt.Printf("Error reading file '%s': %s\n", filename, e)
			fmt.Println("Will use command line flags instead")
			goto errJump
		}

		e = json.Unmarshal(file, &videoMeta)
		if e != nil {
			fmt.Printf("Error parsing file '%s': %s\n", filename, e)
			fmt.Println("Will use command line flags instead")
			goto errJump
		}

		video.Status = &youtube.VideoStatus{}
		video.Snippet.Tags = videoMeta.Tags
		video.Snippet.Title = videoMeta.Title
		video.Snippet.Description = videoMeta.Description
		video.Snippet.CategoryId = videoMeta.CategoryId
		if videoMeta.Location != nil {
			video.RecordingDetails.Location = videoMeta.Location
		}
		if videoMeta.LocationDescription != "" {
			video.RecordingDetails.LocationDescription = videoMeta.LocationDescription
		}
		if !videoMeta.RecordingDate.IsZero() {
			video.RecordingDetails.RecordingDate = videoMeta.RecordingDate.UTC().Format(ytDateLayout)
		}

		// status
		if videoMeta.PrivacyStatus != "" {
			video.Status.PrivacyStatus = videoMeta.PrivacyStatus
		}
		if videoMeta.Embeddable {
			video.Status.Embeddable = videoMeta.Embeddable
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
errJump:

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
			log.Fatalf("Error opening %s: %s", filename, err)
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
			log.Fatalf("Error opening %s: %s", filename, err)
		}
		if resp.ContentLength != 0 {
			filesize = resp.ContentLength
		}
		reader = resp.Body
		return
	}

	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Error opening %s: %s", filename, err)
	}

	fileInfo, err := file.Stat()
	if err != nil {
		log.Fatalf("Error stat'ing file %s: %s", filename, err)
	}

	return file, fileInfo.Size()
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
