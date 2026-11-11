package redact

import (
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const (
	redactedText            = "REDACTED"
	minSecretPathSegmentLen = 16
)

func URL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid URL>"
	}

	redacted := false
	if parsedURL.User != nil {
		parsedURL.User = url.User(redactedText)
		redacted = true
	}
	if parsedURL.RawQuery != "" {
		keys := slices.Sorted(maps.Keys(parsedURL.Query()))
		for i, key := range keys {
			keys[i] = key + "=" + redactedText
		}
		parsedURL.RawQuery = strings.Join(keys, "&")
		redacted = true
	}
	segments := strings.Split(parsedURL.Path, "/")
	for i, segment := range segments {
		if len(segment) >= minSecretPathSegmentLen {
			segments[i] = redactedText
			redacted = true
		}
	}
	if !redacted {
		return rawURL
	}
	parsedURL.Path = strings.Join(segments, "/")
	parsedURL.RawPath = ""
	parsedURL.Fragment = ""

	return parsedURL.String()
}

var urlInText = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'\\]+`)

func URLsIn(text string) string {
	return urlInText.ReplaceAllStringFunc(text, func(match string) string {
		trimmed := strings.TrimRight(match, ":,.;)]")

		return URL(trimmed) + match[len(trimmed):]
	})
}

type urlsError struct {
	err error
}

func (e urlsError) Error() string { return URLsIn(e.err.Error()) }

func (e urlsError) Unwrap() error { return e.err }

func ErrorURLs(err error) error {
	if err == nil {
		return nil
	}

	return urlsError{err: err}
}
