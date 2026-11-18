package bw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebHandler(t *testing.T) {
	root := fixture(t, `{"coordinators":["coord"],"tasks":[{"id":"a","title":"A"}]}`)
	invoke(t, root, 0, "ask", "a", "Question?", "--as", "worker")
	for _, tc := range []struct {
		who, host, origin, body string
		status                  int
	}{
		{"worker", "127.0.0.1:1234", "", `{"id":1,"text":"No"}`, 409},
		{"coord", "127.0.0.1:1234", "http://evil.test", `{"id":1,"text":"No"}`, 403},
		{"coord", "evil.test:1234", "", `{"id":1,"text":"No"}`, 403},
		{"coord", "127.0.0.1:1234", "", `{"id":1,"text":"Yes"}`, 200},
	} {
		hub := &webHub{}
		hub.publish(map[string]any{"ok": true})
		handler := webHandler(context.Background(), root, tc.who, hub)
		request := httptest.NewRequest("POST", "http://"+tc.host+"/answer", strings.NewReader(tc.body))
		request.Host = tc.host
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	data, e := readWeb(context.Background(), root, "coord")
	if e != nil || len(data.Questions[0].Answers) != 1 {
		t.Fatal(data, e)
	}
	hub := &webHub{}
	hub.publish(data)
	handler := webHandler(context.Background(), root, "coord", hub)
	for _, path := range []string{"/", "/assets/tabler.min.css", "/assets/tabler.min.js", "/assets/dashboard.js", "/assets/LICENSE"} {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:1234"+path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal(path, w.Code)
		}
	}
}
func TestWebEventsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	hub := &webHub{}
	hub.publish(map[string]any{"ok": true})
	handler := webHandler(ctx, "", "", hub)
	cancel()
	r := httptest.NewRequest("GET", "http://127.0.0.1:1234/events", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var v map[string]any
	if e := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(w.Body.String(), "data: "))), &v); e != nil || v["ok"] != true {
		t.Fatal(w.Body.String(), e)
	}
}
