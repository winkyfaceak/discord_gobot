// Package navidrome is a small client for Navidrome's Subsonic API: search,
// song/album lookup, cover art, and songs streamed as Opus for Discord voice.
package navidrome

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one Navidrome server as one user.
type Client struct {
	baseURL  string
	user     string
	password string
	api      *http.Client
	stream   *http.Client
}

// Song is a playable track.
type Song struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	CoverArt string `json:"coverArt"`
	Duration int    `json:"duration"` // seconds
}

// Album is a search result that expands to its songs.
type Album struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Artist string `json:"artist"`
}

func New(baseURL, user, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		user:     user,
		password: password,
		api:      &http.Client{Timeout: 10 * time.Second},
		// No overall timeout: a song streams for minutes
		stream: &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 15 * time.Second}},
	}
}

// Search finds songs and albums matching query.
func (c *Client) Search(ctx context.Context, query string, songs, albums int) ([]Song, []Album, error) {
	var r response
	err := c.get(ctx, "search3", url.Values{
		"query":       {query},
		"songCount":   {strconv.Itoa(songs)},
		"albumCount":  {strconv.Itoa(albums)},
		"artistCount": {"0"},
	}, &r)
	return r.SearchResult3.Song, r.SearchResult3.Album, err
}

// Song looks up one song by ID.
func (c *Client) Song(ctx context.Context, id string) (Song, error) {
	var r response
	err := c.get(ctx, "getSong", url.Values{"id": {id}}, &r)
	return r.Song, err
}

// AlbumSongs returns an album's songs in track order.
func (c *Client) AlbumSongs(ctx context.Context, id string) ([]Song, error) {
	var r response
	err := c.get(ctx, "getAlbum", url.Values{"id": {id}}, &r)
	return r.Album.Song, err
}

// CoverArt returns the image for a song or album's coverArt ID.
func (c *Client) CoverArt(ctx context.Context, id string, size int) ([]byte, error) {
	body, err := c.binary(ctx, c.api, "getCoverArt", url.Values{"id": {id}, "size": {strconv.Itoa(size)}})
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, 5<<20))
}

// Stream starts a song transcoded by Navidrome to Ogg Opus.
func (c *Client) Stream(ctx context.Context, id string) (*OpusStream, error) {
	body, err := c.binary(ctx, c.stream, "stream", url.Values{"id": {id}, "format": {"opus"}, "maxBitRate": {"128"}})
	if err != nil {
		return nil, err
	}
	return &OpusStream{body: body, r: bufio.NewReader(body)}, nil
}

type response struct {
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
	SearchResult3 struct {
		Song  []Song  `json:"song"`
		Album []Album `json:"album"`
	} `json:"searchResult3"`
	Album struct {
		Song []Song `json:"song"`
	} `json:"album"`
	Song Song `json:"song"`
}

func (c *Client) get(ctx context.Context, endpoint string, params url.Values, out *response) error {
	resp, err := c.do(ctx, c.api, endpoint, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp.Body, out)
}

// binary fetches a non-JSON endpoint (audio, images). Subsonic reports errors
// as a JSON body with HTTP 200, so a JSON reply here is an error.
func (c *Client) binary(ctx context.Context, client *http.Client, endpoint string, params url.Values) (io.ReadCloser, error) {
	resp, err := c.do(ctx, client, endpoint, params)
	if err != nil {
		return nil, err
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		defer resp.Body.Close()
		var r response
		if err := decode(resp.Body, &r); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("navidrome %s returned no data", endpoint)
	}
	return resp.Body, nil
}

// do sends an authenticated request.
func (c *Client) do(ctx context.Context, client *http.Client, endpoint string, params url.Values) (*http.Response, error) {
	salt := make([]byte, 8)
	_, _ = rand.Read(salt)
	saltHex := hex.EncodeToString(salt)
	token := md5.Sum([]byte(c.password + saltHex))
	params.Set("u", c.user)
	params.Set("t", hex.EncodeToString(token[:]))
	params.Set("s", saltHex)
	params.Set("v", "1.16.1")
	params.Set("c", "discord_gobot")
	params.Set("f", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/rest/"+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("navidrome %s: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("navidrome %s returned %s", endpoint, resp.Status)
	}
	return resp, nil
}

func decode(body io.Reader, out *response) error {
	var envelope struct {
		Response response `json:"subsonic-response"`
	}
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode navidrome response: %w", err)
	}
	if envelope.Response.Status != "ok" {
		return fmt.Errorf("navidrome: %s", envelope.Response.Error.Message)
	}
	*out = envelope.Response
	return nil
}

// OpusStream yields the Opus packets of an Ogg Opus stream, one per Next call.
type OpusStream struct {
	body    io.ReadCloser
	r       *bufio.Reader
	packets [][]byte
	partial []byte
	headers int
}

// Next returns the next audio packet, or io.EOF at the end of the song.
func (o *OpusStream) Next() ([]byte, error) {
	for len(o.packets) == 0 {
		if err := o.readPage(); err != nil {
			return nil, err
		}
	}
	packet := o.packets[0]
	o.packets = o.packets[1:]
	return packet, nil
}

func (o *OpusStream) Close() error { return o.body.Close() }

// readPage reads one Ogg page. Packets are split into segments of up to 255
// bytes; a segment shorter than 255 ends a packet, which may span pages.
func (o *OpusStream) readPage() error {
	var header [27]byte
	if _, err := io.ReadFull(o.r, header[:]); err != nil {
		return err
	}
	if string(header[:4]) != "OggS" {
		return errors.New("navidrome stream is not Ogg Opus")
	}
	segments := make([]byte, header[26])
	if _, err := io.ReadFull(o.r, segments); err != nil {
		return err
	}
	size := 0
	for _, n := range segments {
		size += int(n)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(o.r, data); err != nil {
		return err
	}

	for _, n := range segments {
		o.partial = append(o.partial, data[:n]...)
		data = data[n:]
		if n == 255 {
			continue
		}
		packet := o.partial
		o.partial = nil
		// The first two packets are the OpusHead and OpusTags headers
		if o.headers < 2 {
			o.headers++
		} else if len(packet) > 0 {
			o.packets = append(o.packets, packet)
		}
	}
	return nil
}
