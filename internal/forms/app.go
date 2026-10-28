package forms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/oneworkspacestudio/one-workspace/pkg/apphost"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type App struct {
	H      *apphost.Host
	Store  *Store
	secret string
	rateMu sync.Mutex
	rates  map[string][]time.Time
	Scan   func(context.Context, []byte) error
}

func New(h *apphost.Host, drive Storage) (*App, error) {
	if h.Config.Issuer == "" {
		return nil, errors.New("My Forms requires OneAccount; local password accounts are disabled")
	}
	a := &App{H: h, Store: NewStore(drive), secret: h.Config.ClientSecret, rates: map[string][]time.Time{}}
	a.Scan = scanUpload
	for path, fn := range map[string]http.HandlerFunc{
		"GET /api/v1/forms": a.list, "POST /api/v1/forms": a.create, "GET /api/v1/forms/{id}": a.detail, "PUT /api/v1/forms/{id}": a.save,
		"POST /api/v1/forms/{id}/action": a.action, "GET /api/v1/forms/{id}/responses": a.responses, "POST /api/v1/forms/{id}/grade": a.grade,
		"GET /api/v1/forms/{id}/export": a.export, "POST /api/v1/forms/{id}/export": a.saveExport, "GET /api/v1/forms/{id}/versions": a.versions,
		"POST /api/v1/forms/{id}/media": a.media, "GET /api/v1/forms/{id}/media/{file}": a.authorMedia, "GET /api/v1/forms/{id}/files/{file}": a.download,
	} {
		h.Mux.HandleFunc(path, h.Require(a.ready(fn)))
	}
	h.Mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		a.Store.mu.RLock()
		defer a.Store.mu.RUnlock()
		status := 200
		if !a.Store.Ready {
			status = 503
		}
		apphost.JSON(w, status, map[string]any{"ready": a.Store.Ready, "lastSync": a.Store.LastSync})
	})
	h.Mux.HandleFunc("POST /api/v1/reindex", h.Owner(func(w http.ResponseWriter, r *http.Request) {
		if e := a.Store.Reindex(r.Context()); e != nil {
			apphost.Problem(w, 503, "Drive reconciliation failed; retry when storage is available")
			return
		}
		apphost.JSON(w, 200, map[string]bool{"ok": true})
	}))
	h.Mux.HandleFunc("GET /api/v1/public/{id}", a.ready(a.public))
	h.Mux.HandleFunc("POST /api/v1/public/{id}/responses", a.ready(a.submit))
	h.Mux.HandleFunc("POST /api/v1/public/{id}/uploads", a.ready(a.upload))
	h.Mux.HandleFunc("GET /api/v1/public/{id}/receipt/{response}", a.ready(a.receipt))
	h.Mux.HandleFunc("GET /api/v1/public/{id}/media/{file}", a.ready(a.publicMedia))
	return a, nil
}
func (a *App) Run(ctx context.Context) error {
	for {
		if e := a.Store.Reindex(ctx); e == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
	<-ctx.Done()
	return nil
}
func (a *App) ready(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.Store.mu.RLock()
		ready := a.Store.Ready
		a.Store.mu.RUnlock()
		if !ready {
			apphost.Problem(w, 503, "My Forms is reconnecting to My Drive. Please retry shortly.")
			return
		}
		fn(w, r)
	}
}
func (a *App) permit(w http.ResponseWriter, r *http.Request, responses bool) (Form, bool) {
	f, ok := a.Store.Forms[r.PathValue("id")]
	if !ok || !f.Permission(apphost.Current(r).User.Subject, responses) {
		apphost.Problem(w, 404, "Form not found or access not granted")
		return Form{}, false
	}
	return f, true
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (a *App) list(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	u := apphost.Current(r).User
	out := []map[string]any{}
	for _, f := range a.Store.Forms {
		if f.Permission(u.Subject, false) || f.Permission(u.Subject, true) {
			out = append(out, map[string]any{"id": f.ID, "title": f.Draft.Title, "owner": f.OwnerName, "owned": u.Subject == f.OwnerSubject, "edit": f.Permission(u.Subject, false), "responses": f.Permission(u.Subject, true), "updated": f.Updated, "status": f.Status, "count": a.Store.count(f.ID), "folder": f.Folder, "quiz": f.Draft.Settings.Quiz})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["updated"].(string) > out[j]["updated"].(string) })
	apphost.JSON(w, 200, out)
}
func (a *App) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title    string `json:"title"`
		Quiz     bool   `json:"quiz"`
		Template string `json:"template"`
	}
	if !apphost.Decode(w, r, &in) {
		return
	}
	if in.Title == "" {
		in.Title = "Untitled form"
	}
	u := apphost.Current(r).User
	f := Form{Schema: Schema, ID: apphost.ID(), Owner: u.ID, OwnerSubject: u.Subject, OwnerName: u.Name, Revision: 1, Status: "draft", Updated: now(), Actor: u.ID, Action: "created", Grants: []Grant{}, Draft: Definition{Title: in.Title, Questions: []Question{}, Settings: Settings{Quiz: in.Quiz, Accent: "#3159e8", Background: "#f1f4fc", Font: "system", Progress: true, Confirmation: "Your response has been recorded. Thank you."}}}
	if in.Template == "feedback" {
		f.Draft.Title = "Your feedback matters"
		f.Draft.Description = "Tell us what worked well and what we can improve."
		f.Draft.Questions = []Question{{ID: apphost.ID(), Type: "rating", Title: "How was your experience?", Required: true, Min: 1, Max: 5}, {ID: apphost.ID(), Type: "paragraph", Title: "What could we do better?"}}
	}
	if in.Template == "event" {
		f.Draft.Title = "Event registration"
		f.Draft.Questions = []Question{{ID: apphost.ID(), Type: "short", Title: "Your name", Required: true}, {ID: apphost.ID(), Type: "short", Title: "Email address", Required: true, Validation: "email"}, {ID: apphost.ID(), Type: "choice", Title: "Will you join us?", Options: []string{"Yes", "No", "Maybe"}, Required: true}}
	}
	if e := Validate(f.Draft); e != nil {
		apphost.Problem(w, 400, e.Error())
		return
	}
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	if len(a.Store.Forms) >= 10000 {
		apphost.Problem(w, 409, "The installation form limit has been reached")
		return
	}
	if _, status, e := a.Store.Drive.Call(r.Context(), "bind", "", nil, u.Subject); e != nil || status != 204 {
		apphost.Problem(w, 503, "My Drive permissions could not be connected")
		return
	}
	for _, p := range []string{"/" + u.ID, prefix(f), prefix(f) + "/events", prefix(f) + "/responses", prefix(f) + "/uploads", prefix(f) + "/exports"} {
		if e := a.Store.mkdir(r.Context(), p); e != nil {
			apphost.Problem(w, 503, e.Error())
			return
		}
	}
	if e := a.Store.saveForm(r.Context(), f); e != nil {
		apphost.Problem(w, 503, e.Error())
		return
	}
	apphost.JSON(w, 201, f)
}
func (a *App) detail(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	f, ok := a.permit(w, r, false)
	if !ok {
		return
	}
	apphost.JSON(w, 200, f)
}
func (a *App) save(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision   int        `json:"revision"`
		Definition Definition `json:"definition"`
		Folder     string     `json:"folder"`
	}
	if !apphost.Decode(w, r, &in) {
		return
	}
	if e := Validate(in.Definition); e != nil {
		apphost.Problem(w, 400, e.Error())
		return
	}
	if len(in.Folder) > 100 {
		apphost.Problem(w, 400, "Folder label is too long")
		return
	}
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	f, ok := a.permit(w, r, false)
	if !ok {
		return
	}
	if f.Revision != in.Revision {
		apphost.Problem(w, 409, "Another editor saved a newer version. Copy your changes, then reload to merge.")
		return
	}
	f.Draft = in.Definition
	f.Folder = in.Folder
	a.commit(w, r, &f, "edited")
}
func (a *App) commit(w http.ResponseWriter, r *http.Request, f *Form, action string) {
	f.Revision++
	f.Updated = now()
	f.Actor = apphost.Current(r).User.ID
	f.Action = action
	if e := a.Store.saveForm(r.Context(), *f); e != nil {
		apphost.Problem(w, 503, e.Error())
		return
	}
	apphost.JSON(w, 200, f)
}
func (a *App) action(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision int     `json:"revision"`
		Action   string  `json:"action"`
		Grants   []Grant `json:"grants"`
		Response string  `json:"response"`
	}
	if !apphost.Decode(w, r, &in) {
		return
	}
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	f, ok := a.permit(w, r, false)
	if !ok {
		return
	}
	if in.Revision != f.Revision {
		apphost.Problem(w, 409, "Reload the latest version first")
		return
	}
	u := apphost.Current(r).User
	if (in.Action == "share" || in.Action == "archive" || in.Action == "delete-response") && f.OwnerSubject != u.Subject {
		apphost.Problem(w, 403, "Only the form owner can do this")
		return
	}
	switch in.Action {
	case "publish":
		if e := Validate(f.Draft); e != nil {
			apphost.Problem(w, 400, e.Error())
			return
		}
		b, _ := json.Marshal(f.Draft)
		var published Definition
		_ = json.Unmarshal(b, &published)
		f.Published = &published
		f.PublishedRevision = f.Revision + 1
		f.Status = "open"
	case "close":
		f.Status = "closed"
	case "archive":
		f.Status = "archived"
	case "restore":
		f.Status = "draft"
	case "share":
		if len(in.Grants) > 50 {
			apphost.Problem(w, 400, "At most 50 collaborators")
			return
		}
		for _, g := range in.Grants {
			if g.Subject == "" || len(g.Subject) > 256 || len(g.Name) > 200 || strings.ContainsAny(g.Subject, "\x00\r\n") {
				apphost.Problem(w, 400, "Use a valid OneAccount subject")
				return
			}
		}
		f.Grants = in.Grants
	case "delete-response":
		old, ok := a.Store.Responses[f.ID][in.Response]
		if !ok {
			apphost.Problem(w, 404, "Response not found")
			return
		}
		old.Deleted = true
		old.Version++
		old.Updated = now()
		if e := a.Store.saveResponse(r.Context(), f, old); e != nil {
			apphost.Problem(w, 503, e.Error())
			return
		}
	default:
		apphost.Problem(w, 400, "Unknown form action")
		return
	}
	a.commit(w, r, &f, in.Action)
}
func (a *App) versions(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	f, ok := a.permit(w, r, false)
	if !ok {
		return
	}
	names, _, e := a.Store.list(r.Context(), prefix(f)+"/events")
	if e != nil {
		apphost.Problem(w, 503, e.Error())
		return
	}
	if v := r.URL.Query().Get("revision"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > f.Revision {
			apphost.Problem(w, 400, "Invalid revision")
			return
		}
		var old Form
		if e = a.Store.read(r.Context(), fmt.Sprintf("%s/events/%09d.json", prefix(f), n), &old); e != nil {
			apphost.Problem(w, 503, e.Error())
			return
		}
		apphost.JSON(w, 200, old.Draft)
		return
	}
	apphost.JSON(w, 200, map[string]any{"versions": names, "current": f.Revision})
}
func (a *App) responses(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	f, ok := a.permit(w, r, true)
	if !ok {
		return
	}
	all := []Response{}
	query := strings.ToLower(r.URL.Query().Get("q"))
	for _, v := range a.Store.Responses[f.ID] {
		if v.Deleted {
			continue
		}
		v.EditHash = ""
		if query != "" {
			b, _ := json.Marshal(v)
			if !strings.Contains(strings.ToLower(string(b)), query) {
				continue
			}
		}
		all = append(all, v)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Created > all[j].Created })
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > len(all) {
		offset = 0
	}
	end := min(offset+100, len(all))
	apphost.JSON(w, 200, map[string]any{"total": len(all), "responses": all[offset:end], "summary": Summarize(all), "definition": f.Published, "next": end})
}
func (a *App) grade(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Response string            `json:"response"`
		Version  int               `json:"version"`
		Scores   map[string]int    `json:"scores"`
		Feedback map[string]string `json:"feedback"`
	}
	if !apphost.Decode(w, r, &in) {
		return
	}
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	f, ok := a.permit(w, r, false)
	if !ok {
		return
	}
	if !f.Permission(apphost.Current(r).User.Subject, true) {
		apphost.Problem(w, 403, "Response access is required for grading")
		return
	}
	res, ok := a.Store.Responses[f.ID][in.Response]
	if !ok || res.Deleted {
		apphost.Problem(w, 404, "Response not found")
		return
	}
	if res.Version != in.Version {
		apphost.Problem(w, 409, "Reload the response before grading")
		return
	}
	var version Form
	if e := a.Store.read(r.Context(), fmt.Sprintf("%s/events/%09d.json", prefix(f), res.Revision), &version); e != nil || version.Published == nil {
		apphost.Problem(w, 503, "Published version unavailable")
		return
	}
	limits := map[string]int{}
	for _, q := range version.Published.Questions {
		limits[q.ID] = q.Points
	}
	for id, n := range in.Scores {
		max, ok := limits[id]
		if !ok || n < 0 || n > max {
			apphost.Problem(w, 400, "Score is outside the answer key limits")
			return
		}
	}
	for id, v := range in.Feedback {
		if _, ok := limits[id]; !ok || len(v) > 2000 {
			apphost.Problem(w, 400, "Invalid feedback")
			return
		}
	}
	res.Scores = in.Scores
	res.Feedback = in.Feedback
	res.Graded = true
	res.Version++
	res.Updated = now()
	if e := a.Store.saveResponse(r.Context(), f, res); e != nil {
		apphost.Problem(w, 503, e.Error())
		return
	}
	res.EditHash = ""
	apphost.JSON(w, 200, res)
}
func (a *App) signed(value string) string {
	m := hmac.New(sha256.New, []byte(a.secret))
	m.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (a *App) challenge(id string) string {
	v := fmt.Sprintf("%s.%d.%s", id, time.Now().Unix(), apphost.ID())
	return v + "." + a.signed(v)
}
func (a *App) validChallenge(id, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != id || !apphost.Equal(a.signed(strings.Join(parts[:3], ".")), parts[3]) {
		return false
	}
	n, e := strconv.ParseInt(parts[1], 10, 64)
	age := time.Now().Unix() - n
	return e == nil && age >= 1 && age < 3600
}
func (a *App) rate(id string) bool {
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	now := time.Now()
	for key, times := range a.rates {
		if len(times) == 0 || now.Sub(times[len(times)-1]) >= time.Minute {
			delete(a.rates, key)
		}
	}
	if _, exists := a.rates[id]; !exists && len(a.rates) >= 10000 {
		return false
	}
	xs := a.rates[id][:0]
	for _, t := range a.rates[id] {
		if now.Sub(t) < time.Minute {
			xs = append(xs, t)
		}
	}
	if len(xs) >= 120 {
		return false
	}
	a.rates[id] = append(xs, now)
	return true
}
func (a *App) origin(r *http.Request) bool {
	u, _ := url.Parse(a.H.Config.Origin)
	return r.Header.Get("Origin") == u.Scheme+"://"+u.Host
}
func (a *App) respondent(w http.ResponseWriter, r *http.Request, f Form, fn http.HandlerFunc) {
	if f.Published == nil {
		apphost.Problem(w, 404, "This form is not published")
		return
	}
	if f.Published.Settings.Restricted {
		a.H.Require(func(w http.ResponseWriter, r *http.Request) {
			sub := apphost.Current(r).User.Subject
			s := f.Published.Settings
			if len(s.Respondents) > 0 && sub != f.OwnerSubject {
				found := false
				for _, v := range s.Respondents {
					if v == sub {
						found = true
					}
				}
				if !found {
					apphost.Problem(w, 403, "This form is restricted to invited OneAccount users")
					return
				}
			}
			fn(w, r)
		})(w, r)
	} else {
		fn(w, r)
	}
}
func (a *App) public(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	f, ok := a.Store.Forms[r.PathValue("id")]
	count := a.Store.count(f.ID)
	a.Store.mu.RUnlock()
	if !ok || f.Status == "archived" || f.Published == nil {
		apphost.Problem(w, 404, "Published form not found")
		return
	}
	a.respondent(w, r, f, func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(f.Published)
		var d Definition
		_ = json.Unmarshal(b, &d)
		for i := range d.Questions {
			d.Questions[i].Answer = nil
			d.Questions[i].Feedback = ""
		}
		apphost.JSON(w, 200, map[string]any{"id": f.ID, "revision": f.PublishedRevision, "definition": d, "open": isOpen(f, count), "challenge": a.challenge(f.ID)})
	})
}
func isOpen(f Form, count int) bool {
	if f.Status != "open" || f.Published == nil {
		return false
	}
	s := f.Published.Settings
	if s.ResponseCap > 0 && count >= s.ResponseCap {
		return false
	}
	if s.Deadline != "" {
		d, e := time.Parse(time.RFC3339, s.Deadline)
		if e != nil || time.Now().After(d) {
			return false
		}
	}
	return true
}
func (a *App) submit(w http.ResponseWriter, r *http.Request) {
	if !a.origin(r) || !a.rate(r.PathValue("id")) {
		apphost.Problem(w, 429, "Wait a moment and submit from the form page")
		return
	}
	var in struct {
		ID        string                     `json:"id"`
		Revision  int                        `json:"revision"`
		Challenge string                     `json:"challenge"`
		Website   string                     `json:"website"`
		Email     string                     `json:"email"`
		Answers   map[string]json.RawMessage `json:"answers"`
		Files     []Attachment               `json:"files"`
		EditToken string                     `json:"editToken"`
	}
	if !apphost.Decode(w, r, &in) {
		return
	}
	if !idPattern.MatchString(in.ID) || in.Website != "" || !a.validChallenge(r.PathValue("id"), in.Challenge) {
		apphost.Problem(w, 400, "Refresh the form and try again")
		return
	}
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	f, ok := a.Store.Forms[r.PathValue("id")]
	if !ok || f.Published == nil {
		apphost.Problem(w, 404, "Form not found")
		return
	}
	a.respondent(w, r, f, func(w http.ResponseWriter, r *http.Request) {
		previous, exists := a.Store.Responses[f.ID][in.ID]
		editing := in.EditToken != ""
		receipt := a.signed("receipt:" + f.ID + ":" + in.ID)
		if exists && !editing {
			if !previous.Deleted {
				editToken := ""
				if previous.EditHash != "" && f.Published.Settings.AllowEdit {
					expiry, _ := time.Parse(time.RFC3339Nano, previous.EditExpires)
					if time.Now().Before(expiry) {
						editToken = a.signed("edit:" + f.ID + ":" + in.ID)
					}
				}
				apphost.JSON(w, 200, map[string]any{"saved": true, "id": in.ID, "receipt": receipt, "duplicate": true, "editToken": editToken, "confirmation": f.Published.Settings.Confirmation})
				return
			}
			apphost.Problem(w, 409, "Choose a new submission ID")
			return
		}
		if !isOpen(f, a.Store.count(f.ID)) || f.PublishedRevision != in.Revision {
			apphost.Problem(w, 409, "This form changed or closed. Refresh before submitting")
			return
		}
		if editing {
			expiry, _ := time.Parse(time.RFC3339Nano, previous.EditExpires)
			if !exists || previous.Deleted || !f.Published.Settings.AllowEdit || !apphost.Equal(apphost.Hash(in.EditToken), previous.EditHash) || time.Now().After(expiry) {
				apphost.Problem(w, 403, "This edit link is invalid or expired")
				return
			}
		}
		subject := apphost.Current(r).User.Subject
		s := f.Published.Settings
		if s.OneResponse {
			if !s.Restricted {
				apphost.Problem(w, 400, "One response per person requires OneAccount sign-in")
				return
			}
			for _, old := range a.Store.Responses[f.ID] {
				if !old.Deleted && old.Subject == subject && old.ID != in.ID {
					apphost.Problem(w, 409, "You have already responded")
					return
				}
			}
		}
		if !s.CollectEmail {
			in.Email = ""
		} else if len(in.Email) > 254 || !regexpEmail(in.Email) {
			apphost.Problem(w, 400, "Enter a valid email address")
			return
		}
		if e := ValidateAnswers(*f.Published, in.Answers, in.Files); e != nil {
			apphost.Problem(w, 400, e.Error())
			return
		}
		for i, attachment := range in.Files {
			if !a.validAttachment(f.ID, attachment) {
				apphost.Problem(w, 400, "Upload permission expired; attach the file again")
				return
			}
			in.Files[i].Receipt = ""
		}
		res := Response{Schema: Schema, ID: in.ID, Form: f.ID, Revision: in.Revision, Version: 1, Created: now(), Updated: now(), Subject: subject, Email: in.Email, Answers: in.Answers, Files: in.Files, Labels: map[string]string{}}
		if editing {
			res.Created = previous.Created
			res.Version = previous.Version + 1
		}
		for _, q := range f.Published.Questions {
			res.Labels[q.ID] = q.Title
		}
		Grade(*f.Published, &res)
		editToken := ""
		if s.AllowEdit {
			editToken = a.signed("edit:" + f.ID + ":" + in.ID)
			res.EditHash = apphost.Hash(editToken)
			res.EditExpires = time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
		}
		if e := a.Store.saveResponse(r.Context(), f, res); e != nil {
			apphost.Problem(w, 503, e.Error())
			return
		}
		apphost.JSON(w, 201, map[string]any{"saved": true, "id": in.ID, "receipt": receipt, "editToken": editToken, "confirmation": s.Confirmation})
	})
}
func regexpEmail(s string) bool {
	if strings.TrimSpace(s) != s || strings.ContainsAny(s, "\r\n ,<>\t") {
		return false
	}
	p := strings.Split(s, "@")
	return len(p) == 2 && p[0] != "" && strings.Contains(p[1], ".")
}
func (a *App) receipt(w http.ResponseWriter, r *http.Request) {
	id, rid := r.PathValue("id"), r.PathValue("response")
	token := r.Header.Get("X-Response-Token")
	if !apphost.Equal(token, a.signed("receipt:"+id+":"+rid)) && !apphost.Equal(token, a.signed("edit:"+id+":"+rid)) {
		apphost.Problem(w, 404, "Receipt not found")
		return
	}
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	f, ok := a.Store.Forms[id]
	res, exists := a.Store.Responses[id][rid]
	if !ok || !exists || res.Deleted || f.Published == nil {
		apphost.Problem(w, 404, "Receipt not found")
		return
	}
	edit := apphost.Equal(apphost.Hash(token), res.EditHash)
	expiry, _ := time.Parse(time.RFC3339Nano, res.EditExpires)
	if edit && (!f.Published.Settings.AllowEdit || time.Now().After(expiry)) {
		apphost.Problem(w, 403, "Edit link expired")
		return
	}
	out := map[string]any{"id": rid, "saved": true, "created": res.Created, "confirmation": f.Published.Settings.Confirmation}
	if edit {
		out["answers"] = res.Answers
		out["email"] = res.Email
		files := append([]Attachment{}, res.Files...)
		for i := range files {
			files[i].Receipt = a.attachmentSignature(id, files[i])
		}
		out["files"] = files
	}
	if f.Published.Settings.ReleaseScores {
		if f.Published.Settings.ShowAnswers {
			var original Form
			if a.Store.read(r.Context(), fmt.Sprintf("%s/events/%09d.json", prefix(f), res.Revision), &original) == nil && original.Published != nil {
				answers := map[string]json.RawMessage{}
				for _, q := range original.Published.Questions {
					if len(q.Answer) > 0 {
						answers[q.Title] = q.Answer
					}
				}
				out["answerKey"] = answers
			}
		}
		out["scores"] = res.Scores
		out["feedback"] = res.Feedback
		out["graded"] = res.Graded
	}
	apphost.JSON(w, 200, out)
}
func hashBytes(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
