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
	"fmt"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

func SetThumbnail(ctx context.Context, config Config) error {
	thumbReader, _, err := Open(config.Thumbnail, IMAGE)
	if err != nil {
		return fmt.Errorf("error opening thumbnail: %w", err)
	}
	defer thumbReader.Close()

	client, err := BuildOAuthHTTPClient(
		ctx,
		[]string{youtube.YoutubeUploadScope, youtube.YoutubeScope},
		config.OAuthPort,
	)
	if err != nil {
		return fmt.Errorf("error building OAuth client: %w", err)
	}

	service, err := youtube.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("error creating YouTube client: %w", err)
	}

	fmt.Printf("Setting thumbnail %q on video %s...\n", config.Thumbnail, config.VideoID)
	_, err = service.Thumbnails.Set(config.VideoID).Media(thumbReader).Do()
	if err != nil {
		return fmt.Errorf("error setting thumbnail: %w", err)
	}

	fmt.Printf("Thumbnail set successfully on video %s\n", config.VideoID)
	return nil
}
