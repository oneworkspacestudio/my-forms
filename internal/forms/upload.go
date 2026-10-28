package forms

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/oneworkspacestudio/one-workspace/pkg/apphost"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type uploaded struct {
	Attachment Attachment `json:"attachment"`
	Data       []byte     `json:"data"`
	Created    string     `json:"created"`
	Media      bool       `json:"media"`
}

// Defender is a system facility. No shell interpolation, runtime installation or
// unscanned fallback: unavailable/failed scanning rejects the upload.
func scanUpload(ctx context.Context, b []byte) error {
	candidates, _ := filepath.Glob(filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows Defender", "Platform", "*", "MpCmdRun.exe"))
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	candidates = append(candidates, filepath.Join(os.Getenv("ProgramFiles"), "Windows Defender", "MpCmdRun.exe"))
	scanner := ""
	for _, p := range candidates {
		if info, e := os.Stat(p); e == nil && !info.IsDir() {
			scanner = p
			break
		}
	}
	if scanner == "" {
		return errors.New("Attachment scanning is unavailable on this host")
	}
	f, e := os.CreateTemp("", "myforms-quarantine-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, scanner, "-Scan", "-ScanType", "3", "-File", name, "-DisableRemediation")
	if e = cmd.Run(); e != nil {
		return errors.New("Attachment scan did not pass; the file was not accepted")
	}
	return nil
}
func (a *App) attachmentSignature(form string, f Attachment) string {
	f.Receipt = ""
	b, _ := json.Marshal(f)
	return a.signed("upload:" + form + ":" + string(b))
}
func (a *App) validAttachment(form string, f Attachment) bool {
	return idPattern.MatchString(f.ID) && apphost.Equal(f.Receipt, a.attachmentSignature(form, f))
}
func (a *App) storeUpload(w http.ResponseWriter, r *http.Request, f Form, media bool) {
	question := r.URL.Query().Get("question")
	name := r.Header.Get("X-File-Name")
	if len(name) > 150 || name == "" || strings.ContainsAny(name, "\\/:\x00\r\n") || name == "." || name == ".." {
		apphost.Problem(w, 400, "Choose a regular filename")
		return
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if e != nil || len(b) == 0 {
		apphost.Problem(w, 400, "Choose a file up to 10 MB")
		return
	}
	mime := strings.Split(http.DetectContentType(b), ";")[0]
	if media {
		if mime != "image/png" && mime != "image/jpeg" {
			apphost.Problem(w, 400, "Use a PNG or JPEG image")
			return
		}
	} else {
		found := false
		for _, q := range f.Published.Questions {
			if q.ID == question && q.Type == "file" {
				found = true
				allowed := false
				for _, v := range q.FileTypes {
					if v == mime {
						allowed = true
					}
				}
				if !allowed || int64(len(b)) > q.FileSize {
					apphost.Problem(w, 400, "This question does not accept that file type or size")
					return
				}
			}
		}
		if !found {
			apphost.Problem(w, 400, "Choose a file-upload question")
			return
		}
	}
	if e = a.Scan(r.Context(), b); e != nil {
		apphost.Problem(w, 503, e.Error())
		return
	}
	// Include orphaned uploads in a bounded per-form quota; never trust a local counter.
	names, _, err := a.Store.list(r.Context(), prefix(f)+"/uploads")
	if err != nil {
		apphost.Problem(w, 503, "Cannot verify upload quota")
		return
	}
	if len(names) >= 1000 {
		apphost.Problem(w, 413, "This form has reached its 1,000 attachment limit")
		return
	}
	file := Attachment{ID: apphost.ID(), Question: question, Name: name, MIME: mime, Size: int64(len(b)), SHA256: hashBytes(b)}
	object := uploaded{file, b, now(), media}
	if e = a.Store.write(r.Context(), prefix(f)+"/uploads/"+file.ID+".json", object); e != nil {
		apphost.Problem(w, 503, e.Error())
		return
	}
	file.Receipt = a.attachmentSignature(f.ID, file)
	apphost.JSON(w, 201, file)
}
func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	if !a.origin(r) || !a.rate(r.PathValue("id")) || !a.validChallenge(r.PathValue("id"), r.Header.Get("X-Form-Challenge")) {
		apphost.Problem(w, 429, "Refresh the form before uploading")
		return
	}
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	f, ok := a.Store.Forms[r.PathValue("id")]
	if !ok || !isOpen(f, a.Store.count(f.ID)) {
		apphost.Problem(w, 404, "Open form not found")
		return
	}
	a.respondent(w, r, f, func(w http.ResponseWriter, r *http.Request) { a.storeUpload(w, r, f, false) })
}
func (a *App) media(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	f, ok := a.permit(w, r, false)
	if ok {
		a.storeUpload(w, r, f, true)
	}
}
func (a *App) serveFile(w http.ResponseWriter, r *http.Request, f Form, media bool) {
	id := r.PathValue("file")
	if !idPattern.MatchString(id) {
		apphost.Problem(w, 404, "File not found")
		return
	}
	var v uploaded
	if e := a.Store.read(r.Context(), prefix(f)+"/uploads/"+id+".json", &v); e != nil || v.Attachment.SHA256 != hashBytes(v.Data) || (media && !v.Media) {
		apphost.Problem(w, 404, "File not found")
		return
	}
	w.Header().Set("Content-Type", v.Attachment.MIME)
	w.Header().Set("Content-Disposition", `attachment; filename="attachment"`)
	if media {
		w.Header().Set("Content-Disposition", "inline")
	}
	_, _ = w.Write(v.Data)
}
func (a *App) download(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	f, ok := a.permit(w, r, true)
	if ok {
		a.serveFile(w, r, f, false)
	}
}
func (a *App) publicMedia(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	f, ok := a.Store.Forms[r.PathValue("id")]
	if !ok || f.Published == nil || f.Status == "archived" {
		apphost.Problem(w, 404, "Media not found")
		return
	}
	id := r.PathValue("file")
	found := f.Published.Settings.Header == id
	for _, q := range f.Published.Questions {
		if q.Type == "image" && q.Media == id {
			found = true
		}
	}
	if !found {
		apphost.Problem(w, 404, "Media not found")
		return
	}
	a.respondent(w, r, f, func(w http.ResponseWriter, r *http.Request) { a.serveFile(w, r, f, true) })
}

func (a *App) authorMedia(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	f, ok := a.permit(w, r, false)
	if ok {
		a.serveFile(w, r, f, true)
	}
}
