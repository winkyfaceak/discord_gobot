package weather

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// FetchWttr uses curl to fetch weather from wttr.in
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

	requestURL := buildWttrURL(location, units, view)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		"curl",
		"-sS",
		"--fail",
		"--max-time",
		"8",
		requestURL,
	)

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", errors.New("weather request timed out")
	}

	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}

		return "", fmt.Errorf("curl wttr.in failed: %s", msg)
	}

	result := strings.TrimSpace(string(output))
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
