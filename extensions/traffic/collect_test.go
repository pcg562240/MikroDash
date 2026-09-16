package traffic

import (
	"context"
	"fmt"
	"mikrodash/internal/routeros"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func timeNow() time.Time { return time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC) }

type fakeROS struct {
	cmds               []routeros.Cmd
	missing, duplicate bool
}

func (f *fakeROS) Do(c routeros.Cmd) ([]routeros.Reply, error) {
	f.cmds = append(f.cmds, c)
	if c.Path == "/ip/arp/print" {
		return []routeros.Reply{{"address": "192.168.20.4", "mac-address": "02:00:00:00:00:01"}}, nil
	}
	if slices.Contains(c.Args, "=stats=") {
		value := "1048576"
		if f.missing {
			value = ""
		}
		r := []routeros.Reply{{".id": "*1", "bytes-down": value, "bytes-up": "2048"}}
		if f.duplicate {
			r = append(r, r[0])
		}
		return r, nil
	}
	return []routeros.Reply{{".id": "*1", "name": "test-device", "mac-address": "02:00:00:00:00:01"}}, nil
}
func TestROSCumulativeFieldsAndReadOnlyCommands(t *testing.T) {
	f := &fakeROS{}
	rows, e := readROS(f)
	if e != nil || len(rows) != 1 || rows[0].Down != 1048576 || rows[0].IP != "192.168.20.4" {
		t.Fatal(rows, e)
	}
	for _, c := range f.cmds {
		if !strings.HasSuffix(c.Path, "/print") || c.Timeout != 5*time.Second {
			t.Fatal(c)
		}
	}
}
func TestROSMissingCounterDoesNotBecomeZero(t *testing.T) {
	for _, f := range []*fakeROS{{missing: true}, {duplicate: true}} {
		if _, e := readROS(f); e == nil {
			t.Fatal("expected explicit invalid data")
		}
	}
}
func TestMihomoStreamRecordsCountersAndNotRateIntegral(t *testing.T) {
	d := testDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, e := NewManager(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/connections" {
			w.Write([]byte(`{"connections":[]}`))
			return
		}
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "{\"up\":99999,\"down\":99999,\"upTotal\":%d,\"downTotal\":%d}\n", 200+i*5, 100+i*11)
			w.(http.Flusher).Flush()
			time.Sleep(15 * time.Millisecond)
		}
	}))
	defer h.Close()
	_ = m.clashStream(ctx, Config{RouterID: "r", ClashURL: h.URL, ClashSecret: "secret"})
	a, b, _ := total(t, d)
	if a != 22 || b != 10 {
		t.Fatal(a, b)
	}
	if m.Snapshot("r").Download != 122 {
		t.Fatal(m.Snapshot("r"))
	}
}
func TestMihomoRejectsMissingTotalFields(t *testing.T) {
	d := testDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, _ := NewManager(ctx, d)
	defer m.Close()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{\"up\":1,\"down\":2}\n")) }))
	defer h.Close()
	if e := m.clashStream(ctx, Config{RouterID: "r", ClashURL: h.URL}); e == nil {
		t.Fatal("missing totals silently accepted")
	}
}
func TestControllerRedirectDoesNotLeakSecret(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer origin.Close()
	client := newHTTPClient()
	req, _ := http.NewRequest("GET", origin.URL, nil)
	req.Header.Set("Authorization", "Bearer secret")
	r, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if hits != 0 {
		t.Fatal("followed credential redirect")
	}
}
