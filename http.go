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
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/porjo/go-flowrate/flowrate"
	"google.golang.org/api/youtube/v3"
)

type limitTransport struct {
	rt       http.RoundTripper
	reader   *flowrate.Reader
	filesize int64
}

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

	// single playistID retained for backwards compatibility
	PlaylistID  string   `json:"playlistId, omitempty"`
	PlaylistIDs []string `json:"playlistIds, omitempty"`

	// BCP-47 language code e.g. 'en','es'
	Language string `json:"language, omitempty"`
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
