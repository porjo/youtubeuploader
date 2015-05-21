# Youtube Uploader

## Youtube API

Talking to the Youtube API requires oauth2 authentication. As such, you must:

1. Create an account on the Google Developers Console
1. Register a new app there
1. Enable the Youtube API (APIs & Auth -> APIs)
1. Create Client ID (APIs & Auth -> Credentials), select 'Web application'
1. Take note of the `Client ID` and `Client secret` values

## Usage

### Build

- Install Go e.g. `yum install golang`
- `go get github.com/porjo/youtubeuploader`

### Setup

The utility looks for `client_secret.json` in the local directory. Create it first using the details from above:

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

### Run

```
./youtubeuploader -filename test.mp4
```
