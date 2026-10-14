package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/animesao/cardinal-wings/internal/config"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

func stubPanel(t *testing.T, status int, body string, check func(r *http.Request)) *httptest.Server {
	t.Helper()
	var gotAuth, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		if gotPath != "" && (gotPath != "/api/remote/autostart" || gotMethod != http.MethodGet) {
			t.Errorf("panel hit = %s %s, want GET /api/remote/autostart", gotMethod, gotPath)
		}
		if !strings.HasPrefix(gotAuth, "Bearer ") {
			t.Errorf("panel auth = %q, want Bearer", gotAuth)
		}
	})
	return srv
}

func TestFetchAutostartRefs(t *testing.T) {
	srv := stubPanel(t, http.StatusOK, `{"servers":["srv-a","srv-b"]}`, nil)
	refs, err := fetchAutostartRefs(&panelLink{URL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0] != "srv-a" || refs[1] != "srv-b" {
		t.Fatalf("refs = %v", refs)
	}
}

func TestFetchAutostartRefsDenied(t *testing.T) {
	srv := stubPanel(t, http.StatusUnauthorized, `{"error":"no"}`, nil)
	if _, err := fetchAutostartRefs(&panelLink{URL: srv.URL, Token: "bad"}); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestFetchAutostartRefsNoLink(t *testing.T) {
	if _, err := fetchAutostartRefs(nil); err == nil {
		t.Fatal("expected error without link")
	}
}

func TestBootAutostartNoop(t *testing.T) {
	bootAutostart(&config.Config{}, nil)
	bootAutostart(&config.Config{}, runtime.NewClient("http://127.0.0.1:1", ""))
}

func TestStartAutostartFlow(t *testing.T) {
	var mu sync.Mutex
	started := []string{}
	mux := http.NewServeMux()
	inspect := func(status string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"x","Name":"/x","Image":"img","State":{"Status":"` + status + `"}}`))
		}
	}
	mux.HandleFunc("/containers/srv-off/json", inspect("exited"))
	mux.HandleFunc("/containers/srv-on/json", inspect("running"))
	mux.HandleFunc("/containers/srv-off/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		started = append(started, "srv-off")
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/srv-on/start", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		started = append(started, "srv-on")
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := runtime.NewClient(srv.URL, "")
	startAutostartRef(c, "srv-off")
	startAutostartRef(c, "srv-on")
	startAutostartRef(c, "srv-missing")

	mu.Lock()
	defer mu.Unlock()
	if len(started) != 1 || started[0] != "srv-off" {
		t.Fatalf("started = %v, want only [srv-off]", started)
	}
}
