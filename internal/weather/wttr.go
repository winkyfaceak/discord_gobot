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

// FetchWttr uses the local curl command to fetch weather from wttr.in.
//
// We use exec.CommandContext instead of "sh -c" so user input is passed as a
// normal argument, not as shell code. That is safer for Discord bot input.
func FetchWttr(location string, units string) (string, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return "", errors.New("location is required")
	}

	units = normalizeUnits(units)

	requestURL := buildWttrURL(location, units)

	// Give curl a hard timeout
	//
	// The Discord command handler will defer the interaction first, so this
	// can safely take a few seconds without causing "application did not respond"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		"curl",
		"-sS",
		"--fail",
		"--max-time",
		"6",
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

// buildWttrURL builds a wttr.in URL for a location
//
// Example:
//
//	https://wttr.in/Dublin?format=4&m
//
// format=4 gives a compact one-line weather report that fits well in Discord.
func buildWttrURL(location string, units string) string {
	escapedLocation := url.PathEscape(location)

	// wttr.in examples commonly use + for spaces in location names
	escapedLocation = strings.ReplaceAll(escapedLocation, "%20", "+")

	return fmt.Sprintf(
		"https://wttr.in/%s?format=4&%s",
		escapedLocation,
		units,
	)
}

// normalizeUnits converts unknown unit values to metric
//
// Supported wttr.in unit options:
//
//	m = metric, Celsius and km/h
//	u = USCS, Fahrenheit and mph
//	M = metric, Celsius and m/s wind
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
