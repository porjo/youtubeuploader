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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/browser"
	"golang.org/x/oauth2"
)

const (
	missingClientSecretsMessage = `
Please configure OAuth 2.0

To make this sample run, you need to populate the client_secrets.json file
found at:

   %v

with information from the {{ Google Cloud Console }}
{{ https://cloud.google.com/console }}

For more information about the client_secrets.json file format, please visit:
https://developers.google.com/api-client-library/python/guide/aaa_client_secrets`

	callbackTimeout = 120 * time.Second
)

var (
	clientSecretsFile = flag.String("secrets", "client_secrets.json", "Client Secrets configuration")
	cache             = flag.String("cache", "request.token", "token cache file")
)

// CallbackStatus is returned from the oauth2 callback
type CallbackStatus struct {
	code  string
	state string
}

// Cache specifies the methods that implement a Token cache.
type Cache interface {
	Token() (*oauth2.Token, error)
	PutToken(*oauth2.Token) error
}

// CacheFile implements Cache. Its value is the name of the file in which
// the Token is stored in JSON format.
type CacheFile string

// oAuthClientConfig is a data structure definition for the client_secrets.json file.
// The code unmarshals the JSON configuration file into this structure.
type oAuthClientConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
}

// oAuthRootConfig is a root-level configuration object.
type oAuthRootConfig struct {
	Installed oAuthClientConfig `json:"installed"`
	Web       oAuthClientConfig `json:"web"`
}

// readConfig reads the configuration from clientSecretsFile.
// It returns an oauth configuration object for use with the Google API client.
func readConfig(scopes []string) (*oauth2.Config, error) {

	// Read the secrets file
	data, err := os.ReadFile(*clientSecretsFile)
	if err != nil {
		// fallback to reading from OS specific default config dir
		if errors.Is(err, fs.ErrNotExist) {
			confDir, err := os.UserConfigDir()
			if err != nil {
				return nil, err
			}
			fullPath := filepath.Join(confDir, "youtubeuploader", "client_secrets.json")
			// TODO debug log
			//logger.Debugf("Reading client secrets from %q\n", fullPath)
			data, err = os.ReadFile(fullPath)
			if err != nil {
				return nil, fmt.Errorf(missingClientSecretsMessage, fullPath)
			}
		} else {
			pwd, _ := os.Getwd()
			fullPath := filepath.Join(pwd, *clientSecretsFile)
			return nil, fmt.Errorf(missingClientSecretsMessage, fullPath)
		}
	}

	cfg1 := new(oAuthRootConfig)
	err = json.Unmarshal(data, &cfg1)
	if err != nil {
		return nil, err
	}

	var oCfg *oauth2.Config

	var cfg2 oAuthClientConfig
	if cfg1.Web.ClientID != "" {
		cfg2 = cfg1.Web
	} else if cfg1.Installed.ClientID != "" {
		cfg2 = cfg1.Installed
	} else {
		return nil, errors.New("client secrets file format not recognised")
	}

	redirURL := ""
	if len(cfg2.RedirectURIs) > 0 {
		redirURL = cfg2.RedirectURIs[0]
	} else {
		fmt.Printf("Redirect URL could not be found. Using default: http://localhost:8080/oauth2callback\n")
		redirURL = "http://localhost:8080/oauth2callback"
	}

	oCfg = &oauth2.Config{
		ClientID:     cfg2.ClientID,
		ClientSecret: cfg2.ClientSecret,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg2.AuthURI,
			TokenURL: cfg2.TokenURI,
		},
		RedirectURL: redirURL,
	}
	return oCfg, nil
}

// startCallbackWebServer starts a web server that listens on http://localhost:8080.
// The webserver waits for an oauth code in the three-legged auth flow.
func startCallbackWebServer(ctx context.Context, oAuthPort int) (callbackCh chan CallbackStatus, err error) {
	ctx2, _ := context.WithTimeout(ctx, callbackTimeout)

	quitChan := make(chan struct{})
	defer close(quitChan)

	var srv http.Server

	srv.Addr = fmt.Sprintf(":%d", oAuthPort)

	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.FormValue("code")
		state := r.FormValue("state")
		if code != "" && state != "" {
			cbs := CallbackStatus{}
			cbs.state = r.FormValue("state")
			cbs.code = r.FormValue("code")
			callbackCh <- cbs // send code to OAuth flow
			fmt.Fprintf(w, "Received code: %v\r\nYou can now safely close this browser window.", cbs.code)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			err := srv.Shutdown(ctx)
			if err != nil {
				log.Printf("Callback server shutdown error: %s\n", err)
			}
		}
	})

	callbackCh = make(chan CallbackStatus)

	// shutdown server on context timeout
	go func() {
		select {
		case <-ctx2.Done():
			log.Printf("Timed out waiting for request to callback server: http://localhost:%d\n", oAuthPort)
			err := srv.Shutdown(ctx)
			if err != nil {
				log.Printf("Callback server shutdown error: %s\n", err)
			}
		case <-quitChan:
			return
		}
	}()

	go func() {
		defer close(callbackCh)
		//if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("Callback server error: %s\n", err)
		}
	}()

	return callbackCh, nil
}

// BuildOAuthHTTPClient takes the user through the three-legged OAuth flow.
// It opens a browser in the native OS or outputs a URL, then blocks until
// the redirect completes to the /oauth2callback URI.
// It returns an instance of an HTTP client that can be passed to the
// constructor of the YouTube client.
func BuildOAuthHTTPClient(ctx context.Context, scopes []string, oAuthPort int) (*http.Client, error) {
	config, err := readConfig(scopes)
	if err != nil {
		msg := fmt.Sprintf("Cannot read configuration file: %v", err)
		return nil, errors.New(msg)
	}

	// Check if supplied token cache file exists
	// fallback to reading from OS specific default config dir
	_, err = os.Stat(*cache)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		confDir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		cachePath := filepath.Join(confDir, "youtubeuploader", "request.token")
		_, err = os.Stat(cachePath)
		if err == nil {
			// TODO debug log
			//logger.Debugf("Reading token from cache file %q\n", cachePath)
			*cache = cachePath
		}
	}

	// Try to read the token from the cache file.
	// If an error occurs, do the three-legged OAuth flow because
	// the token is invalid or doesn't exist.
	tokenCache := CacheFile(*cache)
	token, err := tokenCache.Token()
	if err == nil {
		return config.Client(ctx, token), nil
	}

	// You must always provide a non-zero string and validate that it matches
	// the state query parameter on your redirect callback
	randState := fmt.Sprintf("st%d", time.Now().UnixNano())

	// Start web server.
	// This is how this program receives the authorization code
	// when the browser redirects.
	callbackCh, err := startCallbackWebServer(ctx, oAuthPort)
	if err != nil {
		return nil, err
	}

	url := config.AuthCodeURL(randState, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	var cbs CallbackStatus

	err = browser.OpenURL(url)
	if err != nil {
		fmt.Printf("Error opening URL: %s\n\n", err)
		fmt.Printf("Visit the URL below to get a code. This program will pause until the site is visited.\n\n%s\n", url)
	} else {
		fmt.Println("Your browser has been opened to an authorization URL.",
			" This program will resume once authorization has been provided.")
	}

	// Wait for the web server to get the code.
	cbs = <-callbackCh

	if cbs.state != randState {
		return nil, fmt.Errorf("expecting state %q, received state %q", randState, cbs.state)
	}

	token, err = config.Exchange(context.TODO(), cbs.code)
	if err != nil {
		return nil, err
	}
	err = tokenCache.PutToken(token)
	if err != nil {
		return nil, err
	}

	return config.Client(ctx, token), nil
}

// Token retreives the token from the token cache
func (f CacheFile) Token() (*oauth2.Token, error) {
	file, err := os.Open(string(f))
	if err != nil {
		return nil, fmt.Errorf("CacheFile.Token: %s", err.Error())
	}
	defer file.Close()
	tok := &oauth2.Token{}
	if err := json.NewDecoder(file).Decode(tok); err != nil {
		return nil, fmt.Errorf("CacheFile.Token: %s", err.Error())
	}
	return tok, nil
}

// PutToken stores the token in the token cache
func (f CacheFile) PutToken(tok *oauth2.Token) error {
	file, err := os.OpenFile(string(f), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("CacheFile.PutToken: %s", err.Error())
	}
	if err := json.NewEncoder(file).Encode(tok); err != nil {
		file.Close()
		return fmt.Errorf("CacheFile.PutToken: %s", err.Error())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("CacheFile.PutToken: %s", err.Error())
	}
	return nil
}
