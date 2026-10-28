package forms

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReleaseValidationAndRateBound(t *testing.T) {
	q := Question{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Type: "short", Title: "Number", Validation: "number", Min: 10, Max: 20}
	d := Definition{Title: "Bounds", Questions: []Question{q}}
	for _, value := range []string{"NaN", "+Inf", "9", "21"} {
		b, _ := json.Marshal(value)
		if ValidateAnswers(d, map[string]json.RawMessage{q.ID: b}, nil) == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if e := ValidateAnswers(d, map[string]json.RawMessage{q.ID: json.RawMessage(`"15"`)}, nil); e != nil {
		t.Fatal(e)
	}
	q.Type = "checkboxes"
	q.Options = []string{"A", "B"}
	q.Answer = json.RawMessage(`["A","B"]`)
	q.Points = 3
	d.Questions = []Question{q}
	d.Settings.Quiz = true
	r := Response{Answers: map[string]json.RawMessage{q.ID: json.RawMessage(`["B","A"]`)}}
	Grade(d, &r)
	if r.Scores[q.ID] != 3 {
		t.Fatal("selection order changed score")
	}
	a := &App{rates: map[string][]time.Time{"expired": {time.Now().Add(-2 * time.Minute)}}}
	if !a.rate("live") || len(a.rates) != 1 {
		t.Fatal("expired rate entries retained")
	}
	for i := 0; i < 119; i++ {
		if !a.rate("live") {
			t.Fatal("early throttle")
		}
	}
	if a.rate("live") {
		t.Fatal("missing throttle")
	}
}
