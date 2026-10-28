package forms

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const Schema = 1

var idPattern = regexp.MustCompile(`^[a-f0-9]{48}$`)
var ownerPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var Types = []string{"short", "paragraph", "choice", "checkboxes", "dropdown", "scale", "rating", "grid", "checkbox-grid", "date", "time", "file", "section", "heading", "image", "video"}

type Question struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Required    bool              `json:"required"`
	Options     []string          `json:"options"`
	Rows        []string          `json:"rows"`
	Other       bool              `json:"other"`
	Shuffle     bool              `json:"shuffle"`
	Min         int               `json:"min"`
	Max         int               `json:"max"`
	Validation  string            `json:"validation"`
	Pattern     string            `json:"pattern"`
	Branch      map[string]string `json:"branch"`
	Answer      json.RawMessage   `json:"answer,omitempty"`
	Points      int               `json:"points"`
	Feedback    string            `json:"feedback,omitempty"`
	Media       string            `json:"media"`
	FileTypes   []string          `json:"fileTypes"`
	FileSize    int64             `json:"fileSize"`
	FileCount   int               `json:"fileCount"`
}
type Settings struct {
	Restricted    bool     `json:"restricted"`
	Respondents   []string `json:"respondents"`
	CollectEmail  bool     `json:"collectEmail"`
	OneResponse   bool     `json:"oneResponse"`
	Deadline      string   `json:"deadline"`
	ResponseCap   int      `json:"responseCap"`
	Confirmation  string   `json:"confirmation"`
	Progress      bool     `json:"progress"`
	Quiz          bool     `json:"quiz"`
	ReleaseScores bool     `json:"releaseScores"`
	ShowAnswers   bool     `json:"showAnswers"`
	AllowEdit     bool     `json:"allowEdit"`
	DraftConsent  bool     `json:"draftConsent"`
	Accent        string   `json:"accent"`
	Background    string   `json:"background"`
	Font          string   `json:"font"`
	Header        string   `json:"header"`
}
type Definition struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Questions   []Question `json:"questions"`
	Settings    Settings   `json:"settings"`
}
type Grant struct {
	Subject   string `json:"subject"`
	Name      string `json:"name"`
	Edit      bool   `json:"edit"`
	Responses bool   `json:"responses"`
}
type Form struct {
	Schema            int         `json:"schema"`
	ID                string      `json:"id"`
	Owner             string      `json:"owner"`
	OwnerSubject      string      `json:"ownerSubject"`
	OwnerName         string      `json:"ownerName"`
	Revision          int         `json:"revision"`
	PublishedRevision int         `json:"publishedRevision"`
	Status            string      `json:"status"`
	Folder            string      `json:"folder"`
	Draft             Definition  `json:"draft"`
	Published         *Definition `json:"published,omitempty"`
	Grants            []Grant     `json:"grants"`
	Updated           string      `json:"updated"`
	Actor             string      `json:"actor"`
	Action            string      `json:"action"`
}
type Attachment struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Name     string `json:"name"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Receipt  string `json:"receipt,omitempty"`
}
type Response struct {
	Schema      int                        `json:"schema"`
	ID          string                     `json:"id"`
	Form        string                     `json:"form"`
	Revision    int                        `json:"revision"`
	Version     int                        `json:"version"`
	Created     string                     `json:"created"`
	Updated     string                     `json:"updated"`
	Subject     string                     `json:"subject,omitempty"`
	Email       string                     `json:"email,omitempty"`
	Answers     map[string]json.RawMessage `json:"answers"`
	Labels      map[string]string          `json:"labels"`
	Files       []Attachment               `json:"files"`
	Scores      map[string]int             `json:"scores"`
	Feedback    map[string]string          `json:"feedback"`
	Graded      bool                       `json:"graded"`
	Deleted     bool                       `json:"deleted"`
	EditHash    string                     `json:"editHash,omitempty"`
	EditExpires string                     `json:"editExpires,omitempty"`
}

func (f Form) Permission(subject string, responses bool) bool {
	if subject == f.OwnerSubject {
		return true
	}
	for _, g := range f.Grants {
		if g.Subject == subject && ((responses && g.Responses) || (!responses && g.Edit)) {
			return true
		}
	}
	return false
}
func Validate(d Definition) error {
	if strings.TrimSpace(d.Title) == "" || len(d.Title) > 200 || len(d.Description) > 10000 || len(d.Questions) > 200 {
		return errors.New("Use a title up to 200 characters and at most 200 questions")
	}
	s := d.Settings
	if s.ResponseCap < 0 || s.ResponseCap > 100000 || len(s.Confirmation) > 2000 {
		return errors.New("Invalid response settings")
	}
	if s.Deadline != "" {
		if _, e := time.Parse(time.RFC3339, s.Deadline); e != nil {
			return errors.New("Choose a valid deadline")
		}
	}
	color := regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)
	if (s.Accent != "" && !color.MatchString(s.Accent)) || (s.Background != "" && !color.MatchString(s.Background)) {
		return errors.New("Use a six-digit theme colour")
	}
	if s.Font != "" && !slices.Contains([]string{"system", "serif", "mono"}, s.Font) {
		return errors.New("Choose a supported font")
	}
	if s.Header != "" && !idPattern.MatchString(s.Header) {
		return errors.New("Choose an uploaded header image")
	}
	ids := map[string]bool{}
	sections := map[string]int{"start": 0}
	current := "start"
	sectionOf := map[string]string{}
	for i, q := range d.Questions {
		if !idPattern.MatchString(q.ID) || ids[q.ID] || !slices.Contains(Types, q.Type) || len(q.Title) > 1000 || len(q.Description) > 10000 {
			return errors.New("Question IDs, types and labels must be valid and unique")
		}
		ids[q.ID] = true
		if q.Type == "section" {
			current = q.ID
			sections[current] = i + 1
		}
		sectionOf[q.ID] = current
		if q.Points < 0 || q.Points > 1000 || q.Min < 0 || q.Max < 0 || q.Max > 100000 || len(q.Options) > 100 || len(q.Rows) > 100 {
			return errors.New("Question limits are out of range")
		}
		if q.Max > 0 && q.Min > q.Max {
			return errors.New("Minimum must not exceed maximum")
		}
		if len(q.Pattern) > 500 {
			return errors.New("Validation pattern is too long")
		}
		if q.Pattern != "" {
			if _, e := regexp.Compile(q.Pattern); e != nil {
				return errors.New("Invalid safe regular expression")
			}
		}
		if !slices.Contains([]string{"", "email", "url", "number", "length", "regex"}, q.Validation) {
			return errors.New("Unsupported validation")
		}
		if slices.Contains([]string{"choice", "checkboxes", "dropdown", "grid", "checkbox-grid"}, q.Type) {
			if len(q.Options) < 1 {
				return errors.New("Add at least one option")
			}
			seen := map[string]bool{}
			for _, v := range q.Options {
				if v == "" || len(v) > 500 || seen[v] {
					return errors.New("Options must be nonempty and unique")
				}
				seen[v] = true
			}
		}
		seenRows := map[string]bool{}
		for _, row := range q.Rows {
			if strings.TrimSpace(row) == "" || len(row) > 500 || seenRows[row] {
				return errors.New("Grid rows must be nonempty and unique")
			}
			seenRows[row] = true
		}
		if (q.Type == "grid" || q.Type == "checkbox-grid") && len(q.Rows) == 0 {
			return errors.New("Add grid rows")
		}
		if q.Type == "scale" || q.Type == "rating" {
			if q.Max < 1 || q.Max > 10 {
				return errors.New("Scales and ratings end between 1 and 10")
			}
		}
		if q.Type == "file" {
			if q.FileSize < 1 || q.FileSize > 10<<20 || q.FileCount < 1 || q.FileCount > 5 || len(q.FileTypes) == 0 {
				return errors.New("Files need an allow-list, up to 10 MB and five files")
			}
			for _, v := range q.FileTypes {
				if !slices.Contains([]string{"text/plain", "image/png", "image/jpeg", "application/pdf"}, v) {
					return errors.New("Unsupported attachment type")
				}
			}
		}
		if q.Type == "image" && q.Media != "" && !idPattern.MatchString(q.Media) {
			return errors.New("Use an uploaded image")
		}
		if q.Type == "video" && q.Media != "" {
			u, e := url.Parse(q.Media)
			if e != nil || u.Scheme != "https" || u.User != nil || !slices.Contains([]string{"www.youtube.com", "youtu.be", "vimeo.com"}, u.Hostname()) {
				return errors.New("Use a YouTube or Vimeo HTTPS video link")
			}
		}
	}
	// Forward-only section edges rule out cycles and preserve an accessible back path.
	for _, q := range d.Questions {
		for option, target := range q.Branch {
			if !slices.Contains([]string{"choice", "dropdown"}, q.Type) || !slices.Contains(q.Options, option) {
				return errors.New("Branching requires a choice or dropdown option")
			}
			if target != "submit" {
				pos, ok := sections[target]
				if !ok || pos <= sections[sectionOf[q.ID]] {
					return errors.New("Branches must target a later section or Submit")
				}
			}
		}
	}
	return nil
}
func stringAnswer(raw json.RawMessage) (string, bool) {
	var s string
	e := json.Unmarshal(raw, &s)
	return s, e == nil
}
func choices(raw json.RawMessage) ([]string, bool) {
	var s []string
	e := json.Unmarshal(raw, &s)
	return s, e == nil
}
func selectedPath(d Definition, answers map[string]json.RawMessage) map[string]bool {
	enabled := map[string]bool{}
	blocks := [][]Question{{}}
	names := []string{"start"}
	for _, q := range d.Questions {
		if q.Type == "section" {
			blocks = append(blocks, []Question{})
			names = append(names, q.ID)
		}
		blocks[len(blocks)-1] = append(blocks[len(blocks)-1], q)
	}
	for i := 0; i < len(blocks); {
		next := i + 1
		for _, q := range blocks[i] {
			enabled[q.ID] = true
			if v, ok := stringAnswer(answers[q.ID]); ok {
				if t := q.Branch[v]; t != "" {
					if t == "submit" {
						next = len(blocks)
					} else {
						next = slices.Index(names, t)
					}
				}
			}
		}
		if next <= i || next < 0 {
			break
		}
		i = next
	}
	return enabled
}
func ValidateAnswers(d Definition, answers map[string]json.RawMessage, files []Attachment) error {
	active := selectedPath(d, answers)
	known := map[string]Question{}
	for _, q := range d.Questions {
		known[q.ID] = q
	}
	for id := range answers {
		if _, ok := known[id]; !ok {
			return errors.New("Unknown question")
		}
		if !active[id] {
			return errors.New("An answer was supplied for a skipped section")
		}
	}
	for _, q := range d.Questions {
		if !active[q.ID] || slices.Contains([]string{"section", "heading", "image", "video"}, q.Type) {
			continue
		}
		raw := answers[q.ID]
		s, isString := stringAnswer(raw)
		empty := len(raw) == 0 || string(raw) == "null" || string(raw) == `""` || string(raw) == "[]" || string(raw) == "{}"
		if q.Type == "file" {
			n := 0
			for _, f := range files {
				if f.Question == q.ID {
					n++
					if f.Size > q.FileSize || !slices.Contains(q.FileTypes, f.MIME) {
						return fmt.Errorf("%s: attachment not allowed", q.Title)
					}
				}
			}
			if n > q.FileCount || (q.Required && n == 0) {
				return fmt.Errorf("%s: check file count", q.Title)
			}
			continue
		}
		if empty {
			if q.Required {
				return fmt.Errorf("%s is required", q.Title)
			}
			continue
		}
		fail := func() error { return fmt.Errorf("%s: check this answer", q.Title) }
		switch q.Type {
		case "short", "paragraph":
			if !isString || len(s) > 100000 {
				return fail()
			}
			if q.Validation != "number" && q.Min > 0 && len([]rune(s)) < q.Min {
				return fail()
			}
			if q.Validation != "number" && q.Max > 0 && len([]rune(s)) > q.Max {
				return fail()
			}
			if q.Validation == "email" {
				if a, e := mail.ParseAddress(s); e != nil || a.Address != s {
					return fail()
				}
			}
			if q.Validation == "url" {
				u, e := url.Parse(s)
				if e != nil || u.Host == "" || !(u.Scheme == "https" || u.Scheme == "http") {
					return fail()
				}
			}
			if q.Validation == "number" {
				if n, e := strconv.ParseFloat(s, 64); e != nil || math.IsNaN(n) || math.IsInf(n, 0) || (q.Min > 0 && n < float64(q.Min)) || (q.Max > 0 && n > float64(q.Max)) {
					return fail()
				}
			}
			if q.Pattern != "" {
				re, e := regexp.Compile(q.Pattern)
				if e != nil || !re.MatchString(s) {
					return fail()
				}
			}
		case "choice", "dropdown":
			if !isString || (!slices.Contains(q.Options, s) && !(q.Other && strings.HasPrefix(s, "Other: ") && len(s) < 1000)) {
				return fail()
			}
		case "checkboxes":
			v, ok := choices(raw)
			if !ok || len(v) > 101 {
				return fail()
			}
			seen := map[string]bool{}
			for _, x := range v {
				if seen[x] || (!slices.Contains(q.Options, x) && !(q.Other && strings.HasPrefix(x, "Other: ") && len(x) < 1000)) {
					return fail()
				}
				seen[x] = true
			}
		case "scale", "rating":
			var n int
			if json.Unmarshal(raw, &n) != nil || n < q.Min || n > q.Max {
				return fail()
			}
		case "date":
			if !isString {
				return fail()
			}
			if _, e := time.Parse("2006-01-02", s); e != nil {
				return fail()
			}
		case "time":
			if !isString {
				return fail()
			}
			if _, e := time.Parse("15:04", s); e != nil {
				return fail()
			}
		case "grid", "checkbox-grid":
			var rows map[string]json.RawMessage
			if json.Unmarshal(raw, &rows) != nil {
				return fail()
			}
			for row := range rows {
				if !slices.Contains(q.Rows, row) {
					return fail()
				}
			}
			for _, row := range q.Rows {
				v := rows[row]
				if len(v) == 0 && !q.Required {
					continue
				}
				if q.Type == "grid" {
					x, ok := stringAnswer(v)
					if !ok || !slices.Contains(q.Options, x) {
						return fail()
					}
				} else {
					x, ok := choices(v)
					if !ok || (q.Required && len(x) == 0) {
						return fail()
					}
					for _, c := range x {
						if !slices.Contains(q.Options, c) {
							return fail()
						}
					}
				}
			}
		default:
			return fail()
		}
	}
	for _, f := range files {
		q, ok := known[f.Question]
		if !ok || q.Type != "file" || !active[q.ID] {
			return errors.New("Attachment does not belong to an active file question")
		}
	}
	return nil
}
func Grade(d Definition, r *Response) {
	r.Scores = map[string]int{}
	r.Feedback = map[string]string{}
	r.Graded = true
	if !d.Settings.Quiz {
		return
	}
	active := selectedPath(d, r.Answers)
	for _, q := range d.Questions {
		if q.Points == 0 || !active[q.ID] {
			continue
		}
		if len(q.Answer) == 0 {
			r.Graded = false
			continue
		}
		var want, got any
		_ = json.Unmarshal(q.Answer, &want)
		_ = json.Unmarshal(r.Answers[q.ID], &got)
		a, _ := json.Marshal(canonicalAnswer(want))
		b, _ := json.Marshal(canonicalAnswer(got))
		if string(a) == string(b) {
			r.Scores[q.ID] = q.Points
		} else {
			r.Scores[q.ID] = 0
		}
		r.Feedback[q.ID] = q.Feedback
	}
}

// Multiple selections are sets; their order must not change quiz scores.
func canonicalAnswer(v any) any {
	switch x := v.(type) {
	case []any:
		sort.Slice(x, func(i, j int) bool { return fmt.Sprint(x[i]) < fmt.Sprint(x[j]) })
		return x
	case map[string]any:
		for k, val := range x {
			x[k] = canonicalAnswer(val)
		}
		return x
	}
	return v
}
