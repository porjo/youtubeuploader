#!/bin/bash

#(env GOOS=linux GOARCH=arm GOARM=5 go build -o youtubeuploader_linux_armv5; gzip youtubeuploader_linux_armv5) &
#(env GOOS=linux GOARCH=arm GOARM=6 go build -o youtubeuploader_linux_armv6; gzip youtubeuploader_linux_armv6) &
(env GOOS=linux GOARCH=arm GOARM=7 go build -o youtubeuploader_linux_armv7; gzip youtubeuploader_linux_armv7) &
(env GOOS=linux GOARCH=arm64 go build -o youtubeuploader_linux_arm64; gzip youtubeuploader_linux_arm64) &
(env GOOS=linux GOARCH=amd64 go build -o youtubeuploader_linux_amd64; gzip youtubeuploader_linux_amd64) &
(env GOOS=windows GOARCH=amd64 go build -o youtubeuploader_windows_amd64.exe; gzip youtubeuploader_windows_amd64.exe) &
(env GOOS=darwin GOARCH=amd64 go build -o youtubeuploader_mac_amd64.exe; gzip youtubeuploader_mac_amd64.exe) &

wait
