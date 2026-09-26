package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/vcs"
)

// fakeGitHub imita los endpoints de pulls de la API REST de GitHub.
type fakeGitHub struct {
	t      *testing.T
	prs    []map[string]any
	calls  []string
	status int // si != 0, todas las respuestas usan este código
}

func (f *fakeGitHub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls = append(f.calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			f.t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("X-GitHub-Api-Version") == "" {
			f.t.Error("falta X-GitHub-Api-Version")
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			io.WriteString(w, `{"message":"Bad credentials","documentation_url":"https://docs.github.com/rest"}`)
			return
		}
		const base = "/repos/acme/acme-api/pulls"
		switch {
		case r.Method == "POST" && r.URL.Path == base:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			n := len(f.prs) + 101
			pr := map[string]any{"number": n, "html_url": "https://github.com/acme/acme-api/pull/" + itoa(n),
				"state": "open", "merged": false, "title": in["title"], "body": in["body"],
				"head": map[string]any{"ref": in["head"]}, "base": map[string]any{"ref": in["base"]}}
			f.prs = append(f.prs, pr)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(pr)
		case r.Method == "GET" && r.URL.Path == base:
			head := r.URL.Query().Get("head") // owner:branch
			var out []map[string]any
			for _, pr := range f.prs {
				if "acme:"+pr["head"].(map[string]any)["ref"].(string) == head && pr["state"] == r.URL.Query().Get("state") {
					out = append(out, pr)
				}
			}
			json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, base+"/"):
			n := strings.TrimPrefix(r.URL.Path, base+"/")
			for _, pr := range f.prs {
				if itoa(pr["number"].(int)) == n {
					if r.Method == "PATCH" {
						var in map[string]any
						json.NewDecoder(r.Body).Decode(&in)
						pr["body"] = in["body"]
					}
					json.NewEncoder(w).Encode(pr)
					return
				}
			}
			w.WriteHeader(404)
			io.WriteString(w, `{"message":"Not Found"}`)
		default:
			f.t.Errorf("endpoint inesperado %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	})
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func setup(t *testing.T) (*Client, *fakeGitHub) {
	f := &fakeGitHub{t: t}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c := New("acme/acme-api", "tok")
	c.API = srv.URL
	return c, f
}

func TestOpenFindUpdateStatus(t *testing.T) {
	c, f := setup(t)
	ctx := context.Background()
	if _, err := c.FindPR(ctx, "feature/X-1"); !errors.Is(err, vcs.ErrNoPR) {
		t.Fatalf("sin PR: %v", err)
	}
	pr, err := c.OpenPR(ctx, vcs.PRSpec{Base: "dev", Head: "feature/X-1", Title: "X-1 · Demo", Body: "## Brief"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 101 || !strings.HasSuffix(pr.URL, "/pull/101") || pr.State != "open" {
		t.Errorf("abierto: %+v", pr)
	}
	found, err := c.FindPR(ctx, "feature/X-1")
	if err != nil || found.Number != 101 {
		t.Errorf("find: %+v %v", found, err)
	}
	if err := c.UpdatePRBody(ctx, 101, "nuevo cuerpo"); err != nil {
		t.Fatal(err)
	}
	if f.prs[0]["body"] != "nuevo cuerpo" {
		t.Errorf("cuerpo: %v", f.prs[0]["body"])
	}
	f.prs[0]["state"], f.prs[0]["merged"] = "closed", true
	st, err := c.PRStatus(ctx, 101)
	if err != nil || !st.Merged || st.State != "merged" {
		t.Errorf("status: %+v %v", st, err)
	}
	// Un PR cerrado no cuenta para FindPR: se abre otro.
	if _, err := c.FindPR(ctx, "feature/X-1"); !errors.Is(err, vcs.ErrNoPR) {
		t.Errorf("un PR cerrado no se reutiliza: %v", err)
	}
}

func TestClosedWithoutMerge(t *testing.T) {
	c, f := setup(t)
	ctx := context.Background()
	c.OpenPR(ctx, vcs.PRSpec{Base: "dev", Head: "feature/X-1", Title: "t"})
	f.prs[0]["state"] = "closed"
	st, _ := c.PRStatus(ctx, 101)
	if st.Merged || st.State != "closed" {
		t.Errorf("cerrado sin merge: %+v", st)
	}
}

func TestErrors(t *testing.T) {
	c, f := setup(t)
	f.status = 401
	_, err := c.OpenPR(context.Background(), vcs.PRSpec{Base: "dev", Head: "h", Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "Bad credentials") || !strings.Contains(err.Error(), "bflow connect github") {
		t.Errorf("401 debe explicar qué hacer: %v", err)
	}
	noTok := New("acme/acme-api", "")
	if _, err := noTok.FindPR(context.Background(), "h"); !errors.Is(err, vcs.ErrNoCredentials) {
		t.Errorf("sin token: %v", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("sin token no debe llamar a la API: %v", f.calls)
	}
}

func TestCompareURL(t *testing.T) {
	c := New("acme/acme-web", "")
	want := "https://github.com/acme/acme-web/compare/dev...feat/fe-web108-x?expand=1"
	if got := c.CompareURL("dev", "feat/fe-web108-x"); got != want {
		t.Errorf("%s", got)
	}
}
