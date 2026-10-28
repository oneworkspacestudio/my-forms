package forms

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type Storage interface {
	Call(context.Context, string, string, []byte, string) ([]byte, int, error)
}
type MyDriveClient struct {
	URL, Token string
	Client     *http.Client
}

func NewDrive(raw, token string) (*MyDriveClient, error) {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() || token == "" {
		return nil, errors.New("Configure the managed loopback My Drive adapter and capability")
	}
	return &MyDriveClient{raw, token, &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (d *MyDriveClient) Call(ctx context.Context, op, path string, data []byte, subject string) ([]byte, int, error) {
	b, _ := json.Marshal(map[string]any{"operation": op, "path": path, "data": data, "subject": subject})
	req, e := http.NewRequestWithContext(ctx, "POST", d.URL, bytes.NewReader(b))
	if e != nil {
		return nil, 0, e
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := d.Client.Do(req)
	if e != nil {
		return nil, 503, errors.New("My Drive is unavailable; nothing was acknowledged as saved")
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 24<<20))
	return body, resp.StatusCode, e
}

type envelope struct {
	Schema  int             `json:"schema"`
	SHA     string          `json:"sha256"`
	Payload json.RawMessage `json:"payload"`
}

func pack(v any) []byte {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	out, _ := json.Marshal(envelope{1, hex.EncodeToString(sum[:]), b})
	return out
}
func unpack(b []byte, v any) error {
	var x envelope
	if json.Unmarshal(b, &x) != nil || x.Schema != 1 {
		return errors.New("Invalid Drive record schema")
	}
	sum := sha256.Sum256(x.Payload)
	if x.SHA != hex.EncodeToString(sum[:]) {
		return errors.New("Drive record integrity check failed")
	}
	return json.Unmarshal(x.Payload, v)
}

type Store struct {
	mu        sync.RWMutex
	Drive     Storage
	Forms     map[string]Form
	Responses map[string]map[string]Response
	Ready     bool
	LastSync  string
	Failure   string
}

func NewStore(d Storage) *Store {
	return &Store{Drive: d, Forms: map[string]Form{}, Responses: map[string]map[string]Response{}}
}
func (s *Store) write(ctx context.Context, path string, v any) error {
	b := pack(v)
	_, status, e := s.Drive.Call(ctx, "write", path, b, "")
	// Read-back resolves uncertain acknowledgements and verifies byte integrity.
	got, readStatus, re := s.Drive.Call(ctx, "read", path, nil, "")
	if re == nil && readStatus == 200 && bytes.Equal(got, b) {
		return nil
	}
	if status == 409 {
		return errors.New("Drive already has a different revision; reload before saving")
	}
	if e != nil {
		return e
	}
	return errors.New("My Drive did not verify this save; retry with the same submission ID")
}
func (s *Store) mkdir(ctx context.Context, path string) error {
	_, status, e := s.Drive.Call(ctx, "mkdir", path, nil, "")
	if e != nil {
		return e
	}
	if status != 200 && status != 201 && status != 409 {
		return errors.New("My Drive folder creation failed")
	}
	return nil
}
func (s *Store) list(ctx context.Context, path string) (files, folders []string, err error) {
	b, status, e := s.Drive.Call(ctx, "list", path, nil, "")
	if e != nil {
		return nil, nil, e
	}
	if status != 200 {
		return nil, nil, fmt.Errorf("Drive directory unavailable (%d)", status)
	}
	var v struct {
		Files   []string `json:"files"`
		Folders []string `json:"folders"`
	}
	if json.Unmarshal(b, &v) != nil {
		return nil, nil, errors.New("Invalid Drive directory response")
	}
	for _, n := range append(append([]string{}, v.Files...), v.Folders...) {
		if strings.ContainsAny(n, "/\\") || n == ".." {
			return nil, nil, errors.New("Invalid Drive object name")
		}
	}
	sort.Strings(v.Files)
	return v.Files, v.Folders, nil
}
func (s *Store) read(ctx context.Context, path string, v any) error {
	b, status, e := s.Drive.Call(ctx, "read", path, nil, "")
	if e != nil {
		return e
	}
	if status != 200 {
		return errors.New("Drive record unavailable")
	}
	return unpack(b, v)
}
func prefix(f Form) string { return "/" + f.Owner + "/" + f.ID }
func (s *Store) Reindex(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Ready = false
	forms := map[string]Form{}
	responses := map[string]map[string]Response{}
	_, owners, e := s.list(ctx, "/")
	if e != nil {
		s.Failure = e.Error()
		return e
	}
	for _, owner := range owners {
		if !ownerPattern.MatchString(owner) {
			continue
		}
		_, ids, e := s.list(ctx, "/"+owner)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if !idPattern.MatchString(id) {
				continue
			}
			base := "/" + owner + "/" + id
			events, _, e := s.list(ctx, base+"/events")
			if e != nil {
				return e
			}
			var f Form
			for _, name := range events {
				if !strings.HasSuffix(name, ".json") {
					continue
				}
				var candidate Form
				if e = s.read(ctx, base+"/events/"+name, &candidate); e != nil {
					return e
				}
				if candidate.ID != id || candidate.Owner != owner || candidate.Schema != Schema || candidate.Revision != f.Revision+1 {
					return errors.New("Form journal is inconsistent")
				}
				f = candidate
			}
			if f.ID == "" {
				continue
			}
			if _, exists := forms[id]; exists {
				return errors.New("Duplicate form identity in Drive")
			}
			forms[id] = f
			responses[id] = map[string]Response{}
			records, _, e := s.list(ctx, base+"/responses")
			if e != nil {
				return e
			}
			for _, name := range records {
				if !strings.HasSuffix(name, ".json") {
					continue
				}
				var r Response
				if e = s.read(ctx, base+"/responses/"+name, &r); e != nil {
					return e
				}
				if r.Form != id || r.Schema != Schema || !idPattern.MatchString(r.ID) {
					return errors.New("Response journal is inconsistent")
				}
				previous := responses[id][r.ID]
				if r.Version != previous.Version+1 {
					return errors.New("Response revision sequence is inconsistent")
				}
				if r.Version > previous.Version {
					responses[id][r.ID] = r
				}
			}
		}
	}
	s.Forms = forms
	s.Responses = responses
	s.Ready = true
	s.Failure = ""
	s.LastSync = time.Now().UTC().Format(time.RFC3339)
	return nil
}
func (s *Store) saveForm(ctx context.Context, f Form) error {
	if e := s.write(ctx, fmt.Sprintf("%s/events/%09d.json", prefix(f), f.Revision), f); e != nil {
		return e
	}
	s.Forms[f.ID] = f
	return nil
}
func (s *Store) saveResponse(ctx context.Context, f Form, r Response) error {
	if e := s.write(ctx, fmt.Sprintf("%s/responses/%s-%09d.json", prefix(f), r.ID, r.Version), r); e != nil {
		return e
	}
	if s.Responses[f.ID] == nil {
		s.Responses[f.ID] = map[string]Response{}
	}
	s.Responses[f.ID][r.ID] = r
	return nil
}
func (s *Store) count(id string) int {
	n := 0
	for _, r := range s.Responses[id] {
		if !r.Deleted {
			n++
		}
	}
	return n
}
