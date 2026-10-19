package api

// Her desk (her computer's files on the site), as the folder sync (package desksync) reads and
// writes it: the change feed, a text file's body, the writes, and a blob's bytes from its URL.
// Writes say origin "sync": the human's folder, never the human's own hand on the website (a
// locked file refuses its deletes; her system files refuse its writes).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// OriginSync is how the folder's writes are labelled on the desk.
const OriginSync = "sync"

// DeskFile is one desk row, as the feed and the writes return it (never the body).
type DeskFile struct {
	UUID      string          `json:"uuid"`
	Path      string          `json:"path"`
	Kind      string          `json:"kind"` // text · blob · dir
	Mime      string          `json:"mime"`
	Size      int64           `json:"size"`
	SHA256    string          `json:"sha256"` // a by-reference blob's is its URL's
	URL       string          `json:"url,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	Origin    string          `json:"origin"`
	Locked    bool            `json:"locked"`
	Protected bool            `json:"protected"`
	Rev       int             `json:"rev"`
	Seq       int64           `json:"seq"`
	Deleted   bool            `json:"deleted"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ByReference — a blob kept as a URL elsewhere (its sha is the URL's, not the bytes').
func (f *DeskFile) ByReference() bool {
	if f.Kind != "blob" || f.URL == "" {
		return false
	}
	s := sha256.Sum256([]byte(f.URL))
	return hex.EncodeToString(s[:]) == f.SHA256
}

// DeskChanges is one page of the feed: rows changed after `since`, oldest first, tombstones
// included; loop while NextSince < Head.
type DeskChanges struct {
	Items     []DeskFile `json:"items"`
	NextSince int64      `json:"next_since"`
	Head      int64      `json:"head"`
}

func deskPath(companion, rest string) string {
	return "/companions/" + url.PathEscape(companion) + "/desktop" + rest
}

// DeskChanges reads the feed after `since` (limit ≤ 12000; 500 rows stay far under the read cap).
func (c *Client) DeskChanges(ctx context.Context, companion string, since int64, limit int) (*DeskChanges, error) {
	var out DeskChanges
	q := url.Values{"since": {fmt.Sprint(since)}, "limit": {fmt.Sprint(limit)}}
	if err := c.do(ctx, http.MethodGet, deskPath(companion, "/changes?"+q.Encode()), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeskText is a text file's row and body, read together (the body matches the row's sha).
func (c *Client) DeskText(ctx context.Context, companion, path string) (*DeskFile, string, error) {
	var out struct {
		File    DeskFile `json:"file"`
		Content string   `json:"content"`
	}
	// a 1 MiB text can take several MiB as JSON (escapes)
	if err := c.doLimit(ctx, http.MethodGet, deskPath(companion, "/file?"+url.Values{"path": {path}}.Encode()), nil, &out, 8<<20, 0); err != nil {
		return nil, "", err
	}
	return &out.File, out.Content, nil
}

// DeskPutIn is a write: Text or Bytes, the rev it replaces (0 = a new file), the meta to keep.
type DeskPutIn struct {
	Path  string
	Text  *string
	Bytes []byte
	Mime  string
	Meta  json.RawMessage
	IfRev int
}

// DeskPut writes one file; Unchanged = the desk already held these bytes (nothing was written).
func (c *Client) DeskPut(ctx context.Context, companion string, in DeskPutIn) (f *DeskFile, unchanged bool, err error) {
	body := map[string]any{"origin": OriginSync}
	switch {
	case in.Text != nil:
		body["content"] = *in.Text
	default:
		body["body_b64"] = base64.StdEncoding.EncodeToString(in.Bytes)
	}
	if in.Mime != "" {
		body["mime"] = in.Mime
	}
	if len(in.Meta) > 0 && string(in.Meta) != "null" {
		body["meta"] = in.Meta
	}
	if in.IfRev > 0 {
		body["if_rev"] = in.IfRev
	}
	var out struct {
		File      DeskFile `json:"file"`
		Unchanged bool     `json:"unchanged"`
	}
	// uploads get longer than the default, under the site's 90 s cut
	if err := c.doLimit(ctx, http.MethodPut, deskPath(companion, "/file?"+url.Values{"path": {in.Path}}.Encode()), body, &out, 4<<20, 85*time.Second); err != nil {
		return nil, false, err
	}
	return &out.File, out.Unchanged, nil
}

// DeskMkdir makes a folder (an existing one is fine).
func (c *Client) DeskMkdir(ctx context.Context, companion, path string) (*DeskFile, error) {
	var out struct {
		File DeskFile `json:"file"`
	}
	if err := c.do(ctx, http.MethodPost, deskPath(companion, "/mkdir"), map[string]any{"path": path, "origin": OriginSync}, &out); err != nil {
		return nil, err
	}
	return &out.File, nil
}

// DeskMove renames or moves a file or folder (ifRev 0 = no check).
func (c *Client) DeskMove(ctx context.Context, companion, from, to string, ifRev int) (*DeskFile, error) {
	var out struct {
		File DeskFile `json:"file"`
	}
	body := map[string]any{"from": from, "to": to, "origin": OriginSync}
	if ifRev > 0 {
		body["if_rev"] = ifRev
	}
	if err := c.do(ctx, http.MethodPost, deskPath(companion, "/mv"), body, &out); err != nil {
		return nil, err
	}
	return &out.File, nil
}

// DeskDelete deletes a file or a folder (and what's inside).
func (c *Client) DeskDelete(ctx context.Context, companion, path string) error {
	q := url.Values{"path": {path}, "origin": {OriginSync}}
	return c.do(ctx, http.MethodDelete, deskPath(companion, "/file?"+q.Encode()), nil, nil)
}

// DeskQuota is her desk's room: files and bytes used, and the caps.
type DeskQuota struct {
	Files        int64 `json:"files"`
	Bytes        int64 `json:"bytes"`
	MaxFiles     int64 `json:"max_files"`
	MaxBytes     int64 `json:"max_bytes"`
	MaxTextBytes int64 `json:"max_text_bytes"`
	MaxBlobBytes int64 `json:"max_blob_bytes"`
}

func (c *Client) DeskQuota(ctx context.Context, companion string) (*DeskQuota, error) {
	var out DeskQuota
	if err := c.do(ctx, http.MethodGet, deskPath(companion, "/quota"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ErrTooBig — a download larger than the caller allows.
var ErrTooBig = errors.New("api: download too large")

// Download fetches a blob's bytes from its URL into the file `into` (created or truncated), up to
// max bytes, and returns their sha256. The blob URL is public (the CDN, or the URL a by-reference
// blob points at): it never carries the key.
func Download(ctx context.Context, hc *http.Client, rawURL, ua string, max int64, into *os.File) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", ua)
	res, err := hc.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", 0, &Error{Status: res.StatusCode, Code: "download_failed"}
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(into, h), io.LimitReader(res.Body, max+1))
	if err != nil {
		return "", n, err
	}
	if n > max {
		return "", n, ErrTooBig
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// doLimit is do with a read cap of its own and, when timeout > 0, a longer deadline than the
// client's (an upload).
func (c *Client) doLimit(ctx context.Context, method, path string, in, out any, readCap int64, timeout time.Duration) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+Prefix+path, body)
	if err != nil {
		return err
	}
	req.Header = c.Header()
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if timeout > 0 && hc.Timeout < timeout {
		cp := *hc
		cp.Timeout = timeout
		hc = &cp
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, readCap))
	if err != nil {
		return err
	}
	return decode(res.StatusCode, raw, out)
}
