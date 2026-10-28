package forms

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/oneworkspacestudio/one-workspace/pkg/apphost"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryDrive struct {
	mu        sync.Mutex
	objects   map[string][]byte
	dirs      map[string]bool
	down      bool
	uncertain bool
}

func newMemory() *memoryDrive {
	return &memoryDrive{objects: map[string][]byte{}, dirs: map[string]bool{"/": true}}
}
func (d *memoryDrive) Call(ctx context.Context, op, path string, data []byte, subject string) ([]byte, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.down {
		return nil, 503, nil
	}
	switch op {
	case "bind":
		return nil, 204, nil
	case "mkdir":
		d.dirs[path] = true
		return nil, 200, nil
	case "write":
		if _, ok := d.objects[path]; ok {
			return nil, 409, nil
		}
		d.objects[path] = bytes.Clone(data)
		if d.uncertain {
			return nil, 503, fmt.Errorf("connection reset")
		}
		return nil, 200, nil
	case "read":
		if b, ok := d.objects[path]; ok {
			return bytes.Clone(b), 200, nil
		}
		return nil, 404, nil
	case "list":
		prefix := strings.TrimRight(path, "/") + "/"
		files, folders := []string{}, []string{}
		for p := range d.objects {
			if strings.HasPrefix(p, prefix) && !strings.Contains(strings.TrimPrefix(p, prefix), "/") {
				files = append(files, strings.TrimPrefix(p, prefix))
			}
		}
		for p := range d.dirs {
			if strings.HasPrefix(p, prefix) && p != path && !strings.Contains(strings.TrimPrefix(p, prefix), "/") {
				folders = append(folders, strings.TrimPrefix(p, prefix))
			}
		}
		sort.Strings(files)
		sort.Strings(folders)
		b, _ := json.Marshal(map[string]any{"files": files, "folders": folders})
		return b, 200, nil
	}
	return nil, 400, nil
}

type testApp struct {
	a       *App
	h       *apphost.Host
	drive   *memoryDrive
	cookies map[string]string
	csrf    map[string]string
}

func fixture(t *testing.T) *testApp {
	t.Helper()
	h, e := apphost.New(apphost.Config{ID: "forms", Name: "My Forms", Version: "test", Data: t.TempDir(), Listen: "127.0.0.1:30233", Origin: "http://localhost:30200/forms", Issuer: "http://localhost:30201", ClientID: "test-client", ClientSecret: strings.Repeat("a", 64), Owner: "owner"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { h.Close() })
	d := newMemory()
	a, e := New(h, d)
	if e != nil {
		t.Fatal(e)
	}
	a.Scan = func(context.Context, []byte) error { return nil }
	if e = a.Store.Reindex(context.Background()); e != nil {
		t.Fatal(e)
	}
	f := &testApp{a: a, h: h, drive: d, cookies: map[string]string{}, csrf: map[string]string{}}
	for _, subject := range []string{"owner", "editor", "viewer", "stranger"} {
		id := apphost.Identity(h.Config.Issuer, subject)
		token, csrf := apphost.ID()+apphost.ID(), apphost.ID()
		_, e = h.DB.Exec(`INSERT INTO host_users(id,issuer,subject,name,email,role)VALUES(?,?,?,?,?,?)`, id, h.Config.Issuer, subject, subject, subject+"@example.test", "member")
		if e != nil {
			t.Fatal(e)
		}
		_, e = h.DB.Exec(`INSERT INTO host_sessions VALUES(?,?,?,?)`, apphost.Hash(token), id, csrf, time.Now().Add(time.Hour).Format(time.RFC3339Nano))
		if e != nil {
			t.Fatal(e)
		}
		f.cookies[subject] = token
		f.csrf[subject] = csrf
	}
	return f
}
func (f *testApp) request(t *testing.T, subject, method, path string, body any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "http://localhost:30200/forms"+path, bytes.NewReader(b))
	r.Header.Set("Origin", "http://localhost:30200")
	if subject != "" {
		r.AddCookie(&http.Cookie{Name: "forms_session", Value: f.cookies[subject]})
		r.Header.Set("X-CSRF-Token", f.csrf[subject])
	}
	w := httptest.NewRecorder()
	f.h.Handler().ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
func (f *testApp) create(t *testing.T) Form {
	t.Helper()
	code, b := f.request(t, "owner", "POST", "/api/v1/forms", map[string]any{"title": "Integration form"})
	if code != 201 {
		t.Fatalf("create %d %s", code, b)
	}
	var v Form
	if e := json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func (f *testApp) publish(t *testing.T, form Form) Form {
	t.Helper()
	code, b := f.request(t, "owner", "POST", "/api/v1/forms/"+form.ID+"/action", map[string]any{"revision": form.Revision, "action": "publish"})
	if code != 200 {
		t.Fatalf("publish %d %s", code, b)
	}
	json.Unmarshal(b, &form)
	return form
}
func (f *testApp) challenge(id string) string {
	v := fmt.Sprintf("%s.%d.%s", id, time.Now().Add(-2*time.Second).Unix(), apphost.ID())
	return v + "." + f.a.signed(v)
}
func TestQuestionTypesAndValidation(t *testing.T) {
	qs := []Question{}
	answers := map[string]json.RawMessage{}
	for _, typ := range Types {
		q := Question{ID: apphost.ID(), Type: typ, Title: typ, Options: []string{"One", "Two"}, Rows: []string{"Row"}, FileSize: 1024, FileCount: 1, FileTypes: []string{"text/plain"}}
		var value any
		switch typ {
		case "short", "paragraph":
			value = "Hello"
		case "choice", "dropdown":
			value = "One"
		case "checkboxes":
			value = []string{"One", "Two"}
		case "grid":
			value = map[string]string{"Row": "One"}
		case "checkbox-grid":
			value = map[string][]string{"Row": {"One", "Two"}}
		case "scale", "rating":
			q.Min = 1
			q.Max = 5
			value = 3
		case "date":
			value = "2026-10-28"
		case "time":
			value = "12:35"
		}
		if value != nil {
			answers[q.ID], _ = json.Marshal(value)
		}
		qs = append(qs, q)
	}
	d := Definition{Title: "All types", Questions: qs}
	if e := Validate(d); e != nil {
		t.Fatal(e)
	}
	if e := ValidateAnswers(d, answers, nil); e != nil {
		t.Fatal(e)
	}
	answers[qs[2].ID] = json.RawMessage(`"unlisted"`)
	if ValidateAnswers(d, answers, nil) == nil {
		t.Fatal("unlisted choice accepted")
	}
	delete(answers, qs[2].ID)
	qs[2].Required = true
	d.Questions = qs
	if ValidateAnswers(d, answers, nil) == nil {
		t.Fatal("missing required accepted")
	}
}
func TestBranchingAndQuiz(t *testing.T) {
	q := Question{ID: apphost.ID(), Type: "choice", Title: "Route", Options: []string{"Skip", "Continue"}, Required: true, Points: 2, Answer: json.RawMessage(`"Skip"`)}
	section := Question{ID: apphost.ID(), Type: "section", Title: "Last"}
	skipped := Question{ID: apphost.ID(), Type: "short", Title: "Skipped", Required: true}
	middle := Question{ID: apphost.ID(), Type: "section", Title: "Middle"}
	q.Branch = map[string]string{"Skip": section.ID}
	d := Definition{Title: "Logic", Questions: []Question{q, middle, skipped, section}, Settings: Settings{Quiz: true}}
	if e := Validate(d); e != nil {
		t.Fatal(e)
	}
	answers := map[string]json.RawMessage{q.ID: json.RawMessage(`"Skip"`)}
	if e := ValidateAnswers(d, answers, nil); e != nil {
		t.Fatal(e)
	}
	r := Response{Answers: answers}
	Grade(d, &r)
	if r.Scores[q.ID] != 2 || !r.Graded {
		t.Fatal("quiz grade")
	}
	d.Questions[0].Branch["Skip"] = "start"
	if Validate(d) == nil {
		t.Fatal("backward cycle accepted")
	}
}
func TestDrivePersistencePermissionsConflictAndOutage(t *testing.T) {
	f := fixture(t)
	form := f.create(t)
	id := form.ID
	q := Question{ID: apphost.ID(), Type: "short", Title: "Name", Required: true}
	form.Draft.Questions = []Question{q}
	code, b := f.request(t, "owner", "PUT", "/api/v1/forms/"+id, map[string]any{"revision": 1, "definition": form.Draft, "folder": "Research"})
	if code != 200 {
		t.Fatalf("save %d %s", code, b)
	}
	json.Unmarshal(b, &form)
	for _, path := range []string{"/api/v1/forms/" + id, "/api/v1/forms/" + id + "/responses", "/api/v1/forms/" + id + "/export?format=json", "/api/v1/forms/" + id + "/versions"} {
		if code, _ := f.request(t, "stranger", "GET", path, nil); code != 404 {
			t.Fatalf("IDOR %s: %d", path, code)
		}
	}
	if code, _ := f.request(t, "owner", "PUT", "/api/v1/forms/"+id, map[string]any{"revision": 1, "definition": form.Draft}); code != 409 {
		t.Fatal("stale edit accepted")
	}
	form = f.publish(t, form)
	rid := apphost.ID()
	body := map[string]any{"id": rid, "revision": form.PublishedRevision, "challenge": f.challenge(id), "answers": map[string]string{q.ID: "Alice"}, "files": []any{}}
	f.drive.down = true
	if code, _ := f.request(t, "", "POST", "/api/v1/public/"+id+"/responses", body); code != 503 {
		t.Fatalf("false acknowledgement %d", code)
	}
	f.drive.down = false
	f.drive.uncertain = true
	if code, b := f.request(t, "", "POST", "/api/v1/public/"+id+"/responses", body); code != 201 {
		t.Fatalf("submit %d %s", code, b)
	}
	if code, _ := f.request(t, "", "POST", "/api/v1/public/"+id+"/responses", body); code != 200 {
		t.Fatal("retry not idempotent")
	}
	rebuilt := NewStore(f.drive)
	if e := rebuilt.Reindex(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(rebuilt.Forms) != 1 || rebuilt.count(id) != 1 || string(rebuilt.Responses[id][rid].Answers[q.ID]) != `"Alice"` {
		t.Fatal("rebuild lost authoritative data")
	}
	f.drive.mu.Lock()
	for path := range f.drive.objects {
		if strings.Contains(path, "/responses/") {
			f.drive.objects[path] = []byte(`{"schema":1,"sha256":"bad","payload":{}}`)
			break
		}
	}
	f.drive.mu.Unlock()
	if rebuilt.Reindex(context.Background()) == nil || rebuilt.Ready {
		t.Fatal("corruption silently accepted")
	}
}
func TestCollaborationPublicRedactionAndCSRF(t *testing.T) {
	f := fixture(t)
	form := f.create(t)
	form.Draft.Questions = []Question{{ID: apphost.ID(), Type: "choice", Title: "Secret key", Options: []string{"A", "B"}, Answer: json.RawMessage(`"B"`), Feedback: "Secret feedback", Points: 3}}
	code, b := f.request(t, "owner", "PUT", "/api/v1/forms/"+form.ID, map[string]any{"revision": 1, "definition": form.Draft})
	if code != 200 {
		t.Fatal(string(b))
	}
	json.Unmarshal(b, &form)
	code, b = f.request(t, "owner", "POST", "/api/v1/forms/"+form.ID+"/action", map[string]any{"revision": form.Revision, "action": "share", "grants": []Grant{{Subject: "editor", Edit: true}, {Subject: "viewer", Responses: true}}})
	if code != 200 {
		t.Fatal(string(b))
	}
	json.Unmarshal(b, &form)
	for _, tc := range []struct {
		user, path string
		code       int
	}{{"editor", "", 200}, {"editor", "/responses", 404}, {"viewer", "", 404}, {"viewer", "/responses", 200}} {
		code, _ := f.request(t, tc.user, "GET", "/api/v1/forms/"+form.ID+tc.path, nil)
		if code != tc.code {
			t.Fatal(tc, code)
		}
	}
	form = f.publish(t, form)
	code, b = f.request(t, "", "GET", "/api/v1/public/"+form.ID, nil)
	if code != 200 || bytes.Contains(b, []byte(`"answer"`)) || bytes.Contains(b, []byte("Secret feedback")) {
		t.Fatalf("public leak %d %s", code, b)
	}
	req := httptest.NewRequest("PUT", "http://localhost:30200/forms/api/v1/forms/"+form.ID, strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: "forms_session", Value: f.cookies["owner"]})
	req.Header.Set("Origin", "https://attacker.invalid")
	w := httptest.NewRecorder()
	f.h.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	if code, _ := f.request(t, "", "POST", "/api/setup", map[string]string{"email": "test@example.test", "password": "not-a-real-password"}); code != 403 {
		t.Fatal("parallel password signup allowed")
	}
}
func TestExportsPreserveIDsAndNeutralizeFormula(t *testing.T) {
	r := Response{ID: apphost.ID(), Labels: map[string]string{"stable": "Renamed question"}, Answers: map[string]json.RawMessage{"stable": json.RawMessage(`"=HYPERLINK(\"https://invalid\")"`)}}
	b, _, e := makeExport("csv", []Response{r})
	if e != nil || !bytes.Contains(b, []byte("stable | Renamed question")) || !bytes.Contains(b, []byte("'=HYPERLINK")) {
		t.Fatal("unsafe or incomplete CSV")
	}
	b, _, e = makeExport("xlsx", []Response{r})
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range z.File {
		if f.Name == "xl/worksheets/sheet1.xml" {
			rd, _ := f.Open()
			sheet, _ := io.ReadAll(rd)
			rd.Close()
			if bytes.Contains(sheet, []byte("<f>")) || !bytes.Contains(sheet, []byte("inlineStr")) {
				t.Fatal("formula interpretation")
			}
		}
	}
}
func TestScaleOneHundredFormsThousandConcurrentResponses(t *testing.T) {
	f := fixture(t)
	start := time.Now()
	var form Form
	for i := 0; i < 100; i++ {
		form = f.create(t)
	}
	form = f.publish(t, form)
	var wg sync.WaitGroup
	errors := make(chan string, 1000)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := Response{Schema: 1, ID: apphost.ID(), Form: form.ID, Revision: form.PublishedRevision, Version: 1, Created: now(), Updated: now(), Answers: map[string]json.RawMessage{}, Labels: map[string]string{}}
			f.a.Store.mu.Lock()
			e := f.a.Store.saveResponse(context.Background(), form, r)
			f.a.Store.mu.Unlock()
			if e != nil {
				errors <- e.Error()
			}
		}()
	}
	wg.Wait()
	close(errors)
	for e := range errors {
		t.Error(e)
	}
	rebuilt := NewStore(f.drive)
	if e := rebuilt.Reindex(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(rebuilt.Forms) != 100 || rebuilt.count(form.ID) != 1000 {
		t.Fatal("scale reconciliation lost data")
	}
	t.Logf("100 forms/1000 concurrent durable-adapter writes + complete rebuild: %s (memory adapter, native benchmark separate)", time.Since(start))
}
