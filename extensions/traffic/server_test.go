package traffic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func testServer(t *testing.T) (*Server, *Manager) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	d := testDB(t)
	m, e := NewManager(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/status" {
			w.Write([]byte("original-app"))
			return
		}
		if r.Header.Get("Cookie") == "broken" {
			w.WriteHeader(500)
			return
		}
		var s *Session
		switch r.Header.Get("Cookie") {
		case "admin", "reader", "nohistory":
			s = &Session{}
			s.Caps.Routers.Readable = []string{"r"}
			if r.Header.Get("Cookie") != "nohistory" {
				s.Caps.Routers.History = []string{"r"}
			}
			if r.Header.Get("Cookie") == "admin" {
				s.Caps.ManageSettings = true
				s.Caps.Routers.Manageable = []string{"r"}
			}
		}
		jsonOut(w, 200, map[string]any{"session": s})
	}))
	s, e := NewServer(m, upstream.URL)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cancel(); m.Close(); upstream.Close() })
	return s, m
}
func call(s *Server, method, path, cookie, body, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://dashboard.local"+path, strings.NewReader(body))
	r.Header.Set("Cookie", cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Traffic-Extension", "1")
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestAuthAndRouterIsolation(t *testing.T) {
	s, _ := testServer(t)
	for _, tc := range []struct {
		path, cookie string
		want         int
	}{
		{"live?router=r", "", 401}, {"live?router=other", "admin", 403}, {"live?router=r", "broken", 503}, {"live?router=r", "reader", 200}, {"report?router=r", "nohistory", 403}, {"report?router=r", "reader", 200}, {"config?router=r", "reader", 403}, {"config?router=r", "admin", 200},
	} {
		w := call(s, "GET", "/api/traffic-extension/"+tc.path, tc.cookie, "", "")
		if w.Code != tc.want {
			t.Fatalf("%s %s => %d: %s", tc.path, tc.cookie, w.Code, w.Body)
		}
	}
}
func TestCSRFAndConfigSecrets(t *testing.T) {
	s, m := testServer(t)
	c := Config{RouterID: "r", ClashURL: "http://192.168.20.20:9090", ClashSecret: "never-return-this", ROS: ROSConfig{Port: 8728}, Infrastructure: []string{}}
	body := toJSON(map[string]any{"config": c})
	for _, origin := range []string{"", "https://evil.example"} {
		if w := call(s, "POST", "/api/traffic-extension/config?router=r", "admin", body, origin); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	w := call(s, "POST", "/api/traffic-extension/config?router=r", "admin", body, "http://dashboard.local")
	if w.Code != 200 || strings.Contains(w.Body.String(), c.ClashSecret) {
		t.Fatal(w.Code, w.Body)
	}
	if m.Config("r").ClashSecret != c.ClashSecret {
		t.Fatal("secret not stored")
	}
	c.ClashSecret = ""
	body = toJSON(map[string]any{"config": c})
	w = call(s, "POST", "/api/traffic-extension/config?router=r", "admin", body, "http://dashboard.local")
	if w.Code != 200 || m.Config("r").ClashSecret != "never-return-this" {
		t.Fatal("blank did not preserve")
	}
	c.ClashURL = "http://192.168.20.21:9090"
	body = toJSON(map[string]any{"config": c})
	if w = call(s, "POST", "/api/traffic-extension/config?router=r", "admin", body, "http://dashboard.local"); w.Code != 400 {
		t.Fatal("old secret would be sent to new endpoint")
	}
}
func TestNoControllerWriteProxy(t *testing.T) {
	s, _ := testServer(t)
	for _, p := range []string{"/api/traffic-extension/restart?router=r", "/api/traffic-extension/connections?router=r"} {
		if w := call(s, "DELETE", p, "admin", "", "http://dashboard.local"); w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
}
func TestOriginalAppProxyAndGuardedAssets(t *testing.T) {
	s, _ := testServer(t)
	if w := call(s, "GET", "/api/original", "", "", ""); w.Code != 200 || w.Body.String() != "original-app" {
		t.Fatal(w.Code, w.Body)
	}
	if w := call(s, "GET", "/extensions/traffic/", "", "", ""); w.Code != 302 {
		t.Fatal(w.Code)
	}
	w := call(s, "GET", "/extensions/traffic/", "reader", "", "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatal(w.Code, w.Header())
	}
}
func TestEndpointValidation(t *testing.T) {
	for _, host := range []string{"https://public.example", "http://169.254.169.254", "http://127.0.0.1/x", "http://user:pass@192.168.2.1", "http://192.168.2.1?token=secret", "file:///etc/passwd"} {
		c := Config{RouterID: "r", ClashURL: host}
		if c.Validate() == nil {
			t.Fatal(host)
		}
	}
	c := Config{RouterID: "r", ClashURL: "http://192.168.2.1:9090/", Infrastructure: []string{"02:00:00:00:00:aa"}}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	if c.Infrastructure[0] != "02:00:00:00:00:AA" {
		t.Fatal(c)
	}
}
func TestPrunePreservesStateAndConfig(t *testing.T) {
	d := testDB(t)
	record(t, d, sample(1000, 0, 0))
	record(t, d, sample(2000, 11, 12))
	if e := d.Prune(timeNow()); e != nil {
		t.Fatal(e)
	}
	a, b, _ := total(t, d)
	if a != 0 || b != 0 {
		t.Fatal(a, b)
	}
	var n int
	if e := d.sql.QueryRow("SELECT count(*) FROM state").Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
func TestKeyFilePermissions(t *testing.T) {
	dir := t.TempDir()
	d, e := OpenDB(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	info, _ := os.Stat(filepath.Join(dir, ".key"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
func TestRejectUpstreamURLWithCredentials(t *testing.T) {
	_, m := testServer(t)
	if _, e := NewServer(m, "http://user:secret@127.0.0.1:3081"); e == nil {
		t.Fatal("unsafe upstream")
	}
	_, _, e := period(url.Values{"period": {"all"}}, timeNow())
	if e != nil {
		t.Fatal(e)
	}
}
