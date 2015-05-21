# Youtube Uploader

## Youtube API

Talking to the Youtube API requires oauth2 authentication. As such, you must:

- Create an account on the Google Developers Console
- Register a new app there
- Enable the Youtube API (APIs & Auth -> APIs)
- Create Client ID (APIs & Auth -> Credentials), select 'Web application'
- Take note of the `Client ID` and `Client secret` values

## Usage

First create `client_secret.json` file that looks like this:

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

Run the utility, pointing it to a local video file:

```
./youtubeuploader -filename test.mp4
```
