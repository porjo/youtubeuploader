# Youtube Uploader

Scripted uploads to youtube.

- upload video files from local disk or from the web.
- ratelimit upload bandwidth

## Youtube API

Talking to the Youtube API requires oauth2 authentication. As such, you must:

1. Create an account on the [Google Developers Console](https://console.developers.google.com)
1. Register a new app there
1. Enable the Youtube API (APIs & Auth -> APIs)
1. Create Client ID (APIs & Auth -> Credentials), select 'Web application'
1. Add an 'Authorized redirect URI' of 'http://localhost:8080/oauth2callback'
1. Take note of the `Client ID` and `Client secret` values

## Usage

### Build

- Install Go e.g. `yum install golang`
- `go get github.com/porjo/youtubeuploader`

### Setup

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

### Usage

At a minimum, just specify a filename:

```
./youtubeuploader -filename blob.mp4
```

If it is the first time you've run the utility, a browser window should popup and prompt you to provide Youtube credentials. A token will be created and stored in `request.token` file in the local directory for subsequent use. To run the utility on a headless-server, generate the token file locally first, then simply copy the token file along with `youtubeuploader` and `client_secrets.json` to the remote host.

Full list of options:
```
$ ./youtubeuploader --help
Usage of ./youtubeuploader:
  -cache="request.token": Token cache file
  -category="22": Video category
  -description="Test Description": Video description
  -filename="": Filename to upload. Can be a URL
  -keywords="": Comma separated list of video keywords
  -privacy="unlisted": Video privacy status
  -progress=true: Show progress indicator
  -secrets="client_secrets.json": Client Secrets configuration
  -title="Test Title": Video title
```
*NOTE:* When specifying a URL as the filename, the data will be streamed through the localhost (download from remote host, then upload to Youtube)

### Credit

Based on [Go Youtube API Sample code](https://github.com/youtube/api-samples/tree/master/go)
