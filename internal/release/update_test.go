package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// listServer sirve /releases/latest (latest, "" = 404 o status) y la lista.
func listServer(t *testing.T, latest string, latestStatus int, list string) (*Client, *[]string) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch r.URL.Path {
		case "/releases/latest":
			if latestStatus != 0 {
				w.WriteHeader(latestStatus)
				return
			}
			_, _ = w.Write([]byte(latest))
		case "/releases":
			_, _ = w.Write([]byte(list))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{API: srv.URL, HTTP: srv.Client()}, &paths
}

const mixedList = `[
 {"tag_name":"v0.1.0-rc.9","prerelease":true,"draft":false},
 {"tag_name":"v0.1.0-rc.11","prerelease":true,"draft":true},
 {"tag_name":"v0.1.0-rc.10","prerelease":true,"draft":false},
 {"tag_name":"nightly","prerelease":true,"draft":false}]`

func TestLatestStable(t *testing.T) {
	cl, paths := listServer(t, `{"tag_name":"v1.0.0","assets":[{"name":"a","browser_download_url":"u"}]}`, 0, mixedList)
	rel, err := cl.Latest(context.Background(), false)
	if err != nil || rel.Tag != "v1.0.0" || rel.Assets["a"] != "u" {
		t.Fatalf("%+v %v", rel, err)
	}
	if len(*paths) != 1 {
		t.Errorf("consultó la lista: %v", *paths)
	}
}

func TestLatestFallbackOnlyPrereleases(t *testing.T) {
	cl, paths := listServer(t, "", 404, `[{"tag_name":"v0.1.0-rc.12","draft":true},{"tag_name":"v0.1.0-rc.9","prerelease":true}]`)
	rel, err := cl.Latest(context.Background(), false)
	if err != nil || rel.Tag != "v0.1.0-rc.9" || !rel.Prerelease {
		t.Fatalf("%+v %v", rel, err)
	}
	if got := strings.Join(*paths, " "); got != "/releases/latest /releases?per_page=10" {
		t.Errorf("rutas: %s", got)
	}
	cl, _ = listServer(t, "", 404, `[{"tag_name":"v0.1.0-rc.12","draft":true}]`)
	if _, err := cl.Latest(context.Background(), false); err == nil || !strings.Contains(err.Error(), "no hay versiones publicadas") {
		t.Errorf("sin válidas: %v", err)
	}
}

func TestLatestPreChoosesHighest(t *testing.T) {
	cl, paths := listServer(t, `{"tag_name":"v0.0.1"}`, 0, mixedList)
	rel, err := cl.Latest(context.Background(), true)
	if err != nil || rel.Tag != "v0.1.0-rc.10" {
		t.Fatalf("%+v %v", rel, err)
	}
	if got := strings.Join(*paths, " "); got != "/releases?per_page=10" {
		t.Errorf("rutas: %s", got)
	}
}

func TestLatestOtherErrorNoFallback(t *testing.T) {
	cl, paths := listServer(t, "", 403, mixedList)
	_, err := cl.Latest(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
	if len(*paths) != 1 {
		t.Errorf("cayó a la lista: %v", *paths)
	}
}
