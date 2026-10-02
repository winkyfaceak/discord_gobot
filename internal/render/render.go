// Package render draws SVG documents as images with ImageMagick's magick
// command (librsvg for SVG; fonts come from fontconfig).
package render

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"strings"
)

// Magick runs magick with args, feeding it stdin, and returns its output.
func Magick(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "magick", args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ImageMagick: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// PNG draws an SVG document as a PNG.
func PNG(ctx context.Context, svg string) ([]byte, error) {
	return Magick(ctx, svg, "svg:-", "png:-")
}

// Escape makes text safe to put inside SVG markup.
func Escape(text string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(text))
	return b.String()
}
