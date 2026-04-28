package youtubeuploader

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/api/googleapi"
)

const (
	YouTubeQuotaCostDocURL       = "https://developers.google.com/youtube/v3/determine_quota_cost"
	YouTubeQuotaAuditDocURL      = "https://developers.google.com/youtube/v3/guides/quota_and_compliance_audits"
	YouTubeQuotaExtensionFormURL = "https://support.google.com/youtube/contact/yt_api_form"
	YouTubeResumableUploadDocURL = "https://developers.google.com/youtube/v3/guides/using_resumable_upload_protocol"
)

var quotaErrorReasons = map[string]struct{}{
	"quotaExceeded":               {},
	"dailyLimitExceeded":          {},
	"dailyLimitExceeded402":       {},
	"quotaExceeded402":            {},
	"uploadLimitExceeded":         {},
	"userRequestsExceedRateLimit": {},
}

// UploadAlert is emitted by batch execution to support real-time operator notifications.
type UploadAlert struct {
	Time      time.Time
	TaskID    string
	AccountID string
	Severity  string
	Message   string
	Reasons   []string
}

func estimateUploadQuotaCost(config Config) int {
	cost := 100 // videos.insert
	if config.Thumbnail != "" {
		cost += 50 // thumbnails.set
	}
	if config.Caption != "" {
		cost += 400 // captions.insert
	}
	playlistOps := len(config.PlaylistIDs)
	cost += playlistOps * 50 // playlistItems.insert
	return cost
}

func extractYouTubeErrorReasons(err error) []string {
	if err == nil {
		return nil
	}
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return nil
	}
	if len(apiErr.Errors) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(apiErr.Errors))
	seen := map[string]struct{}{}
	for _, item := range apiErr.Errors {
		if item.Reason == "" {
			continue
		}
		if _, exists := seen[item.Reason]; exists {
			continue
		}
		seen[item.Reason] = struct{}{}
		reasons = append(reasons, item.Reason)
	}
	sort.Strings(reasons)
	return reasons
}

func isQuotaRelatedError(err error) bool {
	reasons := extractYouTubeErrorReasons(err)
	for _, reason := range reasons {
		if _, ok := quotaErrorReasons[reason]; ok {
			return true
		}
	}
	return false
}

func wrapUploadError(err error, operation string) error {
	if err == nil {
		return nil
	}
	reasons := extractYouTubeErrorReasons(err)
	if len(reasons) == 0 {
		return fmt.Errorf("%s failed: %w", operation, err)
	}
	if !isQuotaRelatedError(err) {
		return fmt.Errorf("%s failed: %w (reasons=%s)", operation, err, strings.Join(reasons, ","))
	}
	return fmt.Errorf(
		"%s failed: %w (quota reasons=%s). Check quota usage in Google Cloud Console, see quota cost doc: %s, use resumable upload guidance: %s, and request quota extension/audit when needed: %s and %s",
		operation,
		err,
		strings.Join(reasons, ","),
		YouTubeQuotaCostDocURL,
		YouTubeResumableUploadDocURL,
		YouTubeQuotaAuditDocURL,
		YouTubeQuotaExtensionFormURL,
	)
}
