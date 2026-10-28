package forms

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/oneworkspacestudio/one-workspace/pkg/apphost"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func Summarize(all []Response) map[string]any {
	counts := map[string]map[string]int{}
	trend := map[string]int{}
	for _, r := range all {
		if r.Deleted {
			continue
		}
		day := r.Created
		if len(day) > 10 {
			day = day[:10]
		}
		trend[day]++
		for id, raw := range r.Answers {
			if counts[id] == nil {
				counts[id] = map[string]int{}
			}
			var value any
			if json.Unmarshal(raw, &value) != nil {
				continue
			}
			switch v := value.(type) {
			case []any:
				for _, x := range v {
					counts[id][fmt.Sprint(x)]++
				}
			case map[string]any:
				for row, x := range v {
					counts[id][row+": "+fmt.Sprint(x)]++
				}
			default:
				counts[id][fmt.Sprint(v)]++
			}
		}
	}
	return map[string]any{"questions": counts, "trend": trend}
}
func exportRows(rs []Response) [][]string {
	labels := map[string]string{}
	for _, r := range rs {
		for id, label := range r.Labels {
			labels[id] = label
		}
	}
	ids := []string{}
	for id := range labels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	header := []string{"response_id", "published_revision", "created_utc", "updated_utc", "email", "files_json", "scores_json"}
	for _, id := range ids {
		header = append(header, id+" | "+labels[id])
	}
	rows := [][]string{header}
	for _, r := range rs {
		files, _ := json.Marshal(r.Files)
		scores, _ := json.Marshal(r.Scores)
		row := []string{r.ID, strconv.Itoa(r.Revision), r.Created, r.Updated, r.Email, string(files), string(scores)}
		for _, id := range ids {
			v := string(r.Answers[id])
			if s, ok := stringAnswer(r.Answers[id]); ok {
				v = s
			}
			row = append(row, v)
		}
		rows = append(rows, row)
	}
	return rows
}
func safeCell(s string) string {
	trim := strings.TrimLeft(s, " \t\r\n")
	if len(trim) > 0 && strings.ContainsRune("=+-@", rune(trim[0])) {
		return "'" + s
	}
	return s
}
func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func col(n int) string {
	s := ""
	for n++; n > 0; n = (n - 1) / 26 {
		s = string(rune('A'+(n-1)%26)) + s
	}
	return s
}
func makeExport(format string, rs []Response) ([]byte, string, error) {
	switch format {
	case "json":
		b, e := json.MarshalIndent(rs, "", "  ")
		return b, "application/json", e
	case "csv":
		var out bytes.Buffer
		out.WriteString("\xef\xbb\xbf")
		w := csv.NewWriter(&out)
		for _, row := range exportRows(rs) {
			for i := range row {
				row[i] = safeCell(row[i])
			}
			if e := w.Write(row); e != nil {
				return nil, "", e
			}
		}
		w.Flush()
		return out.Bytes(), "text/csv; charset=utf-8", w.Error()
	case "xlsx":
		var out bytes.Buffer
		z := zip.NewWriter(&out)
		entries := map[string]string{
			"[Content_Types].xml":        `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
			"_rels/.rels":                `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
			"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Responses" sheetId="1" r:id="rId1"/></sheets></workbook>`,
			"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`}
		var sheet strings.Builder
		sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
		for i, row := range exportRows(rs) {
			fmt.Fprintf(&sheet, `<row r="%d">`, i+1)
			for j, v := range row {
				fmt.Fprintf(&sheet, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, col(j), i+1, xmlText(v))
			}
			sheet.WriteString(`</row>`)
		}
		sheet.WriteString(`</sheetData></worksheet>`)
		entries["xl/worksheets/sheet1.xml"] = sheet.String()
		names := []string{}
		for k := range entries {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			w, e := z.Create(name)
			if e != nil {
				return nil, "", e
			}
			if _, e = w.Write([]byte(entries[name])); e != nil {
				return nil, "", e
			}
		}
		if e := z.Close(); e != nil {
			return nil, "", e
		}
		return out.Bytes(), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
	}
	return nil, "", fmt.Errorf("Choose csv, xlsx or json")
}
func (a *App) exportData(r *http.Request, f Form) ([]byte, string, error) {
	rs := []Response{}
	for _, v := range a.Store.Responses[f.ID] {
		if v.Deleted {
			continue
		}
		v.EditHash = ""
		v.EditExpires = ""
		rs = append(rs, v)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].Created < rs[j].Created })
	return makeExport(r.URL.Query().Get("format"), rs)
}
func (a *App) export(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.RLock()
	defer a.Store.mu.RUnlock()
	f, ok := a.permit(w, r, true)
	if !ok {
		return
	}
	b, mime, e := a.exportData(r, f)
	if e != nil {
		apphost.Problem(w, 400, e.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", `attachment; filename="responses.`+r.URL.Query().Get("format")+`"`)
	_, _ = w.Write(b)
}
func (a *App) saveExport(w http.ResponseWriter, r *http.Request) {
	a.Store.mu.Lock()
	defer a.Store.mu.Unlock()
	f, ok := a.permit(w, r, true)
	if !ok {
		return
	}
	b, _, e := a.exportData(r, f)
	if e != nil {
		apphost.Problem(w, 400, e.Error())
		return
	}
	name := apphost.ID() + "." + r.URL.Query().Get("format")
	path := prefix(f) + "/exports/" + name
	_, status, e := a.Store.Drive.Call(r.Context(), "write", path, b, "")
	if e != nil || status != 200 {
		apphost.Problem(w, 503, "Export was not saved to My Drive")
		return
	}
	got, status, e := a.Store.Drive.Call(r.Context(), "read", path, nil, "")
	if e != nil || status != 200 || !bytes.Equal(got, b) {
		apphost.Problem(w, 503, "Drive export verification failed")
		return
	}
	apphost.JSON(w, 201, map[string]any{"saved": true, "source": "My Forms", "path": strings.TrimPrefix(path, "/"+f.Owner)})
}
