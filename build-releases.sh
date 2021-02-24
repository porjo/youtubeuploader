#!/bin/bash

> sha256-checksums

VER=$(git describe --tags)

(env GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-X main.appVersion=$VER" -o youtubeuploader_linux_armv7
tar -czf youtubeuploader_linux_armv7.tar.gz  youtubeuploader_linux_armv7
rm -f youtubeuploader_linux_armv7
sha256sum youtubeuploader_linux_armv7.tar.gz >> sha256-checksums
) &

(env GOOS=linux GOARCH=arm64 go build -ldflags "-X main.appVersion=$VER" -o youtubeuploader_linux_arm64
tar -czf youtubeuploader_linux_arm64.tar.gz youtubeuploader_linux_arm64
rm -f youtubeuploader_linux_arm64
sha256sum youtubeuploader_linux_arm64.tar.gz >> sha256-checksums
) &

(env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.appVersion=$VER" -o youtubeuploader_linux_amd64
tar -czf youtubeuploader_linux_amd64.tar.gz youtubeuploader_linux_amd64
rm -f youtubeuploader_linux_amd64
sha256sum youtubeuploader_linux_amd64.tar.gz >> sha256-checksums
) &

(env GOOS=windows GOARCH=amd64 go build -ldflags "-X main.appVersion=$VER" -o youtubeuploader_windows_amd64.exe
zip youtubeuploader_windows_amd64.zip youtubeuploader_windows_amd64.exe
rm -f youtubeuploader_windows_amd64.exe
sha256sum youtubeuploader_windows_amd64.zip >> sha256-checksums
) &

(env GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.appVersion=$VER" -o youtubeuploader_mac_amd64
zip youtubeuploader_mac_amd64.zip youtubeuploader_mac_amd64
rm -f youtubeuploader_mac_amd64
sha256sum youtubeuploader_mac_amd64.zip >> sha256-checksums
) &

wait
