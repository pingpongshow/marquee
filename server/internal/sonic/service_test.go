package sonic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// One bad file used to fail its whole batch (and stop the analysis run).
func TestAnalyzeBatchIsolatesBadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(Health{Model: "m", Dims: 2})
			return
		}
		var req struct{ Paths []string }
		json.NewDecoder(r.Body).Decode(&req)
		for _, p := range req.Paths {
			if p == "bad" {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
		}
		out := struct {
			Model   string     `json:"model"`
			Results []Analysis `json:"results"`
		}{Model: "m"}
		for _, p := range req.Paths {
			out.Results = append(out.Results, Analysis{Path: p, Embedding: []float32{1, 0}})
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	s := &Service{Client: &Client{BaseURL: srv.URL}}
	model, res, err := s.analyzeBatch(context.Background(), []string{"a", "bad", "c"})
	if err != nil || model != "m" || len(res) != 3 {
		t.Fatalf("got %q %v %v", model, res, err)
	}
	if res[0].Error != "" || res[2].Error != "" || len(res[0].Embedding) != 2 || res[1].Error == "" {
		t.Fatalf("bad file not isolated: %+v", res)
	}
}
