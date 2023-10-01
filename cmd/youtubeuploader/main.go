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
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	yt "github.com/porjo/youtubeuploader"
	"github.com/porjo/youtubeuploader/internal/limiter"
	"github.com/porjo/youtubeuploader/internal/utils"
	"google.golang.org/api/googleapi"
)

const inputTimeLayout = "15:04"

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
	rateLimit         = flag.Int("ratelimit", 0, "rate limit upload in Kbps. No limit by default")
	metaJSON          = flag.String("metaJSON", "", "JSON file containing title,description,tags etc (optional)")
	metaJSONout       = flag.String("metaJSONout", "", "filename to write uploaded video metadata into (optional)")
	limitBetween      = flag.String("limitBetween", "", "only rate limit between these times e.g. 10:00-14:00 (local time zone)")
	oAuthPort         = flag.Int("oAuthPort", 8080, "TCP port to listen on when requesting an oAuth token")
	showAppVersion    = flag.Bool("version", false, "show version")
	chunksize         = flag.Int("chunksize", googleapi.DefaultUploadChunkSize, "size (in bytes) of each upload chunk. A zero value will cause all data to be uploaded in a single request")
	notifySubscribers = flag.Bool("notify", true, "notify channel subscribers of new video. Specify '-notify=false' to disable.")
	debug             = flag.Bool("debug", false, "turn on verbose log output")
	sendFileName      = flag.Bool("sendFilename", true, "send original file name to YouTube")

	// this is set by compile-time to match git tag
	appVersion string = "unknown"
)

func main() {

	var err error

	flag.Parse()
	config := yt.Config{
		Filename:          *filename,
		Thumbnail:         *thumbnail,
		Caption:           *caption,
		Title:             *title,
		Description:       *description,
		Language:          *language,
		CategoryId:        *categoryId,
		Tags:              *tags,
		Privacy:           *privacy,
		Quiet:             *quiet,
		RateLimit:         *rateLimit,
		MetaJSON:          *metaJSON,
		MetaJSONOut:       *metaJSONout,
		LimitBetween:      *limitBetween,
		OAuthPort:         *oAuthPort,
		ShowAppVersion:    *showAppVersion,
		Chunksize:         *chunksize,
		NotifySubscribers: *notifySubscribers,
		SendFileName:      *sendFileName,
	}

	config.Logger = utils.NewLogger(*debug)

	config.Logger.Debugf("Youtubeuploader version: %s\n", appVersion)

	if config.ShowAppVersion {
		fmt.Printf("Youtubeuploader version: %s\n", appVersion)
		os.Exit(0)
	}

	if config.Filename == "" {
		fmt.Printf("\nYou must provide a filename of a video file to upload\n")
		fmt.Printf("\nUsage:\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if config.Title == "" {
		config.Title = strings.ReplaceAll(filepath.Base(config.Filename), filepath.Ext(config.Filename), "")
	}

	var limitRange limiter.LimitRange
	if config.LimitBetween != "" {
		limitRange, err = limiter.ParseLimitBetween(config.LimitBetween, inputTimeLayout)
		if err != nil {
			fmt.Printf("Invalid value for -limitBetween: %v", err)
			os.Exit(1)
		}
	}

	videoReader, filesize, err := yt.Open(config.Filename, yt.VIDEO)
	if err != nil {
		log.Fatal(err)
	}
	defer videoReader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	transport, err := limiter.NewLimitTransport(config.Logger, http.DefaultTransport, limitRange, filesize, config.RateLimit)
	if err != nil {
		log.Fatal(err)
	}

	err = yt.Run(ctx, transport, config, videoReader)
	if err != nil {
		log.Fatal(err)
	}

}
