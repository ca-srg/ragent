package slacksearch

import (
	"math"
	"strconv"
	"time"
)

// FormatSlackTimestamp converts a Slack timestamp to RFC3339 format.
func FormatSlackTimestamp(ts string) string {
	if ts == "" {
		return "-"
	}
	seconds, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return ts
	}
	secs := int64(seconds)
	nsecs := int64((seconds - math.Floor(seconds)) * 1e9)
	return time.Unix(secs, nsecs).Format(time.RFC3339)
}

// FormatSlackUser returns a displayable Slack user identifier.
func FormatSlackUser(userID, username string) string {
	if username != "" {
		return username
	}
	if userID != "" {
		return userID
	}
	return "unknown"
}

// FormatSlackChannel returns a displayable Slack channel identifier.
func FormatSlackChannel(id string) string {
	if id == "" {
		return "-"
	}
	return id
}
