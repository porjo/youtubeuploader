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
	"fmt"

	"google.golang.org/api/youtube/v3"
)

type Playlistx struct {
	Id            string
	Title         string
	PrivacyStatus string
}

type VideoMeta struct {
	// snippet
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	CategoryId  string   `json:"categoryId,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	// status
	PrivacyStatus          string `json:"privacyStatus,omitempty"`
	Embeddable             bool   `json:"embeddable,omitempty"`
	License                string `json:"license,omitempty"`
	PublicStatsViewable    bool   `json:"publicStatsViewable,omitempty"`
	PublishAt              Date   `json:"publishAt,omitempty"`
	MadeForKids            bool   `json:"madeForKids,omitempty"`
	ContainsSyntheticMedia bool   `json:"containsSyntheticMedia,omitempty"`

	// recording details
	RecordingDate Date `json:"recordingDate,omitempty"`

	PlaylistIDs    []string `json:"playlistIds,omitempty"`
	PlaylistTitles []string `json:"playlistTitles,omitempty"`

	// BCP-47 language code e.g. 'en','es'
	Language string `json:"language,omitempty"`

	Localizations map[string]youtube.VideoLocalization `json:"localizations,omitempty"`
}

func playlistList(service *youtube.Service, pageToken string) (*youtube.PlaylistListResponse, error) {
	call := service.Playlists.List([]string{"snippet", "contentDetails"})
	call = call.Mine(true)

	if pageToken != "" {
		call = call.PageToken(pageToken)
	}

	response, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("error retrieving playlists: %w", err)
	}

	return response, nil
}

func (plx *Playlistx) AddVideoToPlaylist(service *youtube.Service, videoID string) error {
	var playlist *youtube.Playlist
	var err error

	nextPageToken := ""
	for {
		// retrieve the next set of playlists
		playlistResponse, err := playlistList(service, nextPageToken)
		if err != nil {
			return err
		}

		for _, pl := range playlistResponse.Items {
			if pl.Id == plx.Id || pl.Snippet.Title == plx.Title {
				playlist = pl
				break
			}
		}

		// retrieve the next page of results or exit the loop if done
		nextPageToken = playlistResponse.NextPageToken
		if nextPageToken == "" {
			break
		}
	}

	// create playlist if it doesn't exist
	if playlist == nil {
		if plx.Id != "" {
			return fmt.Errorf("playlist ID %q doesn't exist", plx.Id)
		}
		playlist = &youtube.Playlist{}
		playlist.Snippet = &youtube.PlaylistSnippet{Title: plx.Title}
		playlist.Status = &youtube.PlaylistStatus{PrivacyStatus: plx.PrivacyStatus}
		insertCall := service.Playlists.Insert([]string{"snippet", "status"}, playlist)
		// API doesn't return playlist ID here!?
		playlist, err = insertCall.Do()
		if err != nil {
			return fmt.Errorf("error creating playlist with title %q: %w", plx.Title, err)
		}
	}

	playlistItem := &youtube.PlaylistItem{}
	playlistItem.Snippet = &youtube.PlaylistItemSnippet{PlaylistId: playlist.Id, Title: playlist.Snippet.Title}
	playlistItem.Snippet.ResourceId = &youtube.ResourceId{
		VideoId: videoID,
		Kind:    "youtube#video",
	}

	insertCall := service.PlaylistItems.Insert([]string{"snippet"}, playlistItem)
	_, err = insertCall.Do()
	if err != nil {
		return err
	}

	fmt.Printf("Video added to playlist %q (%s)\n", playlist.Snippet.Title, playlist.Id)

	return nil
}
