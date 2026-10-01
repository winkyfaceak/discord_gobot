package weather

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// FetchWttr fetches plain-text weather from wttr.in
//
// location examples:
//   - Dublin
//   - New York
//   - Tokyo
//   - muc
//
// units:
//   - m = metric, Celsius and km/h
//   - u = US, Fahrenheit and mph
//   - M = metric, Celsius and m/s wind
//
// view:
//   - compact  = one-line weather
//   - current  = current weather only
//   - today    = current weather + today's forecast
//   - two_days = current weather + today + tomorrow
//   - full     = full wttr.in forecast
func FetchWttr(location string, units string, view string) (string, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return "", errors.New("location is required")
	}

	req, err := http.NewRequest(http.MethodGet, buildWttrURL(location, units, view), nil)
	if err != nil {
		return "", err
	}
	// wttr.in serves HTML to browsers and plain text to terminal clients
	req.Header.Set("User-Agent", "curl/8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("wttr.in request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read wttr.in response: %w", err)
	}

	result := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wttr.in returned %s", resp.Status)
	}
	if result == "" {
		return "", errors.New("wttr.in returned an empty response")
	}

	return result, nil
}

func buildWttrURL(location string, units string, view string) string {
	escapedLocation := url.PathEscape(location)

	// wttr.in examples use + for spaces in location names.
	escapedLocation = strings.ReplaceAll(escapedLocation, "%20", "+")

	unitFlag := normalizeUnits(units)

	if view == "compact" {
		return fmt.Sprintf(
			"https://wttr.in/%s?format=4&%s",
			escapedLocation,
			unitFlag,
		)
	}

	viewFlag := normalizeView(view)

	// A = force terminal/ANSI-style weather output
	// F = hide the "Follow" line
	// T = turn terminal color sequences off
	//
	// Example:
	//   https://wttr.in/Dublin?m2AFT
	return fmt.Sprintf(
		"https://wttr.in/%s?%s%sAFT",
		escapedLocation,
		unitFlag,
		viewFlag,
	)
}

func normalizeUnits(units string) string {
	switch units {
	case "u":
		return "u"
	case "M":
		return "M"
	default:
		return "m"
	}
}

func normalizeView(view string) string {
	switch view {
	case "current":
		return "0"
	case "today":
		return "1"
	case "two_days":
		return "2"
	case "full":
		return ""
	default:
		return "1"
	}
}
