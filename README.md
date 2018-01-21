# Youtube Uploader

Scripted uploads to youtube.

- upload video files from local disk or from the web.
- ratelimit upload bandwidth

## Download

Grab a [precompiled binary](https://github.com/porjo/youtubeuploader/releases) for Linux, Mac or Windows or build it yourself.

## Build

This project uses ['dep'](https://github.com/golang/dep) for [vendoring](https://blog.gopheracademy.com/advent-2015/vendor-folder/).

- Install Go e.g. `yum install golang` or `apt-get install golang`
- Define your Go Path e.g. `export GOPATH=$HOME/go`
- Fetch the project `go get github.com/porjo/youtubeuploader`
- run `dep ensure` in the project root
- run `go build`

## Setup

### Youtube API

Talking to the Youtube API requires oauth2 authentication. As such, you must:

1. Create an account on the [Google Developers Console](https://console.developers.google.com)
1. Register a new app there
1. Enable the Youtube API (APIs & Auth -> APIs)
1. Create Client ID (APIs & Auth -> Credentials), select 'Web application'
1. Add an 'Authorized redirect URI' of 'http://localhost:8080/oauth2callback'
1. Take note of the `Client ID` and `Client secret` values

The utility looks for `client_secrets.json` in the local directory. Create it first using the details from above:

```
{
  "installed": {
    "client_id": "xxxxxxxxxxxxxxxxxxx.apps.googleusercontent.com",
    "client_secret": "xxxxxxxxxxxxxxxxxxxxx",
    "redirect_uris": ["http://localhost:8080/oauth2callback"],
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://accounts.google.com/o/oauth2/token"
  }
}
```

Update `client_id` and `client_secret` to match your details

## Usage

At a minimum, just specify a filename:

```
./youtubeuploader -filename blob.mp4
```

If it is the first time you've run the utility, a browser window should popup and prompt you to provide Youtube credentials. A token will be created and stored in `request.token` file in the local directory for subsequent use. To run the utility on a headless-server, generate the token file locally first, then simply copy the token file along with `youtubeuploader` and `client_secrets.json` to the remote host.

Full list of options:
```
  -cache string
    	Token cache file (default "request.token")
  -categoryId string
    	Video category Id
  -description string
    	Video description (default "uploaded by youtubeuploader")
  -filename string
    	Filename to upload. Can be a URL
  -headlessAuth
    	set this if host does not have browser available for the oauth authorisation step
  -metaJSON string
    	JSON file containing title,description,tags etc (optional)
  -privacy string
    	Video privacy status (default "private")
  -quiet
    	Suppress progress indicator
  -ratelimit int
    	Rate limit upload in kbps. No limit by default
  -secrets string
    	Client Secrets configuration (default "client_secrets.json")
  -tags string
    	Comma separated list of video tags
  -thumbnail string
    	Thumbnail to upload. Can be a URL
  -title string
    	Video title (default "Video Title")
```
*NOTE:* When specifying a URL as the filename, the data will be streamed through the localhost (download from remote host, then upload to Youtube)


### Metadata

Video title, description etc can specified via the command line flags or via a JSON file using the `-metaJSON` flag. An example JSON file would be:

```json
{
  "title": "my test title",
  "description": "my test description",
  "tags": ["test tag1", "test tag2"],
  "privacyStatus": "private",
  "embeddable": true,
  "license": "creativeCommon",
  "publicStatsViewable": true,
  "publishAt": "2017-06-01T12:00:00.000+02:00",
  "categoryId": "10",
  "recordingdate": "2017-05-21",
  "location": {
    "latitude": 48.8584,
    "longitude": 2.2945
  },
  "locationDescription":  "Eiffel Tower"
}
```
All fields are optional. Command line flags will be used by default (where available). Use `\n` in the description to insert newlines.

## Alternative Oauth setup for headless clients

If you do not have access to a web browser on the host where `youtubeuploader` is installed, you may follow this oauth setup method instead:

1. Create an account on the [Google Developers Console](https://console.developers.google.com)
1. Register a new app there
1. Enable the Youtube API (APIs & Auth -> APIs)
1. Create Client ID (APIs & Auth -> Credentials), select Application Type 'Other'
1. Download the resulting credentials file, saving it as `client_secrets.json` in the `youtubeuploader` directory
1. Run `youtubeuploader` for the first time, passing the `-headlessAuth` parameter
1. Copy-and-paste the URL displayed and open that in a browser
1. Copy the resulting authorisation code and paste that into the `youtubeuploader` prompt: *"Enter authorisation code here:"*

(subsequent invocations of `youtubeuploader` do not require the `-headlessAuth` parameter)

## Credit

Based on [Go Youtube API Sample code](https://github.com/youtube/api-samples/tree/master/go)
