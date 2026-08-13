package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
	"github.com/IceShack/job-scout/scraper/internal/store"
	"github.com/IceShack/job-scout/scraper/internal/version"
)

// Options configures the server.
type Options struct {
	// Title names the instance in the UI and salts the auth cookie.
	Title   string
	Store   *store.Store
	Run     func()
	LastRun func() time.Time
	// Password guards every route except /health; empty disables auth.
	Password string
}

// Server exposes the matched jobs and a manual scrape trigger.
type Server struct {
	title     string
	store     *store.Store
	trigger   func()
	lastRun   func() time.Time
	template  *template.Template
	password  string
	authToken string
}

func New(opts Options) *Server {
	s := &Server{
		title:    opts.Title,
		store:    opts.Store,
		trigger:  opts.Run,
		lastRun:  opts.LastRun,
		template: template.Must(template.New("index").Funcs(template.FuncMap{"reltime": relTime}).Parse(indexHTML)),
		password: opts.Password,
	}
	if s.title == "" {
		s.title = "job-scout"
	}
	if s.password != "" {
		// Salting with the title keeps cookies from one instance out of
		// another running on the same host.
		sum := sha256.Sum256([]byte(s.title + "-auth:" + s.password))
		s.authToken = hex.EncodeToString(sum[:])
	}
	return s
}

// Handler registers every route from the table in openapi.go, which is
// also what the OpenAPI document is generated from.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		mux.HandleFunc(rt.Method+" "+rt.Path, rt.Handler)
	}
	if s.password == "" {
		return mux
	}
	return s.requireAuth(mux)
}

const authCookie = "sj_auth"

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Kubernetes probes must reach /health unauthenticated.
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(authCookie); err == nil &&
			subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.authToken)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/login" {
			if subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(s.password)) == 1 {
				http.SetCookie(w, &http.Cookie{
					Name:     authCookie,
					Value:    s.authToken,
					Path:     "/",
					MaxAge:   30 * 24 * 3600,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(s.loginHTML("Wrong password.")))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(s.loginHTML("")))
	})
}

func (s *Server) loginHTML(msg string) string {
	errLine := ""
	if msg != "" {
		errLine = `<p style="color:#c62828">` + msg + `</p>`
	}
	title := template.HTMLEscapeString(s.title)
	return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + `</title>
<style>:root{color-scheme:light dark;font-family:system-ui,sans-serif}body{display:flex;justify-content:center;margin-top:20vh}
form{display:flex;gap:.5rem;flex-direction:column;width:16rem}input,button{padding:.5rem}</style>
</head><body>
<form method="post" action="/login">
<h1 style="font-size:1.1rem">` + title + `</h1>` + errLine + `
<input type="password" name="password" placeholder="password" autofocus>
<button>enter</button>
</form>
</body></html>`
}

func (s *Server) setFlag(set func(string, bool) (bool, error), value bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		found, err := set(r.PathValue("id"), value)
		switch {
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		case !found:
			http.Error(w, "unknown job id", http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// handleStatus sets a job's application status from ?value=; an empty
// value takes the job back out of the pipeline.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := model.Status(r.FormValue("value"))
	if !status.Valid() {
		http.Error(w, "unknown status "+string(status), http.StatusBadRequest)
		return
	}
	found, err := s.store.SetStatus(r.PathValue("id"), status)
	switch {
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	case !found:
		http.Error(w, "unknown job id", http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// statusFilter reports whether a job passes the ?status= filter. Besides
// the statuses themselves it takes "tracked" (anything applied to) and
// "none" (everything still untouched).
func statusFilter(want string, j *model.Job) bool {
	switch want {
	case "":
		return true
	case "tracked":
		return j.Tracked()
	case "none":
		return !j.Tracked()
	default:
		return string(j.Status) == want
	}
}

func (s *Server) filtered(r *http.Request) []model.Job {
	jobs := s.store.List()
	q := r.URL.Query()
	src := q.Get("source")
	term := strings.ToLower(q.Get("q"))
	minScore, _ := strconv.Atoi(q.Get("min"))
	fit := q.Get("fit")
	// Default view hides "not interested" entries; ?hidden=1 shows only them.
	showHidden := q.Get("hidden") == "1"
	status := q.Get("status")

	out := jobs[:0]
	for _, j := range jobs {
		if j.Hidden != showHidden {
			continue
		}
		if !statusFilter(status, &j) {
			continue
		}
		if src != "" && j.Source != src {
			continue
		}
		if j.Score < minScore {
			continue
		}
		if fit != "" && !strings.HasPrefix(j.Fit, fit) {
			continue
		}
		if term != "" && !strings.Contains(strings.ToLower(j.Title+" "+j.Company+" "+j.Description), term) {
			continue
		}
		out = append(out, j)
	}
	return out
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.filtered(r))
}

// filterOptions derives the dropdown choices from the jobs actually
// stored, so the UI follows the configured sources and location labels
// without knowing anything about them.
func (s *Server) filterOptions() (sources, fits []string) {
	seenSource, seenFit := map[string]bool{}, map[string]bool{}
	for _, j := range s.store.List() {
		if j.Source != "" && !seenSource[j.Source] {
			seenSource[j.Source] = true
			sources = append(sources, j.Source)
		}
		// Fits read "label (marker)"; the filter matches on the label.
		fit, _, _ := strings.Cut(j.Fit, " (")
		if fit != "" && !seenFit[fit] {
			seenFit[fit] = true
			fits = append(fits, fit)
		}
	}
	slices.Sort(sources)
	slices.Sort(fits)
	return sources, fits
}

// statusFilters are the choices in the status dropdown, the statuses
// themselves included so adding one to model.Statuses is enough.
func statusFilters() []struct{ Value, Label string } {
	out := []struct{ Value, Label string }{
		{"", "any status"},
		{"none", "not applied"},
		{"tracked", "applied — any"},
	}
	for _, st := range model.Statuses {
		out = append(out, struct{ Value, Label string }{string(st), string(st)})
	}
	return out
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	jobs := s.filtered(r)
	sources, fits := s.filterOptions()
	data := map[string]any{
		"Title":         s.title,
		"Version":       version.Version,
		"Jobs":          jobs,
		"Sources":       sources,
		"Fits":          fits,
		"Statuses":      model.Statuses,
		"StatusNone":    model.StatusNone,
		"StatusFilters": statusFilters(),
		"Total":         len(jobs),
		"LastRun":       s.lastRun(),
		"Query":         r.URL.Query(),
		"HiddenView":    r.URL.Query().Get("hidden") == "1",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.template.Execute(w, data)
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "–"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; font-family: system-ui, sans-serif; }
body { margin: 2rem auto; max-width: 1100px; padding: 0 1rem; }
h1 { font-size: 1.3rem; }
form { display: flex; gap: .5rem; flex-wrap: wrap; margin: 1rem 0; }
input, select, button { padding: .35rem .5rem; }
table { border-collapse: collapse; width: 100%; font-size: .9rem; }
th, td { text-align: left; padding: .45rem .6rem; border-bottom: 1px solid #8884; vertical-align: top; }
tr:hover td { background: #8881; }
.score { font-weight: 700; text-align: right; }
.muted { opacity: .65; font-size: .8rem; }
tr.st-applied td { background: rgba(255, 193, 7, .18); }
tr.st-applied:hover td { background: rgba(255, 193, 7, .28); }
tr.st-interviewing td { background: rgba(76, 175, 80, .20); }
tr.st-interviewing:hover td { background: rgba(76, 175, 80, .30); }
tr.st-declined td { opacity: .5; }
tr.st-declined .title { text-decoration: line-through; }
select.status { font-size: .8rem; padding: .1rem; }
.actions { white-space: nowrap; }
.fit { white-space: nowrap; }
a { color: inherit; }
</style>
</head>
<body>
<h1>{{.Title}} <span class="muted">v{{.Version}} · {{.Total}} matches · last run {{reltime .LastRun}}</span></h1>
<form method="get">
<input name="q" placeholder="search…" value="{{.Query.Get "q"}}">
<select name="source">
<option value="">all sources</option>
{{range .Sources}}<option {{if eq ($.Query.Get "source") .}}selected{{end}}>{{.}}</option>
{{end}}</select>
<select name="fit">
<option value="">any fit</option>
{{range .Fits}}<option value="{{.}}" {{if eq ($.Query.Get "fit") .}}selected{{end}}>{{.}}</option>
{{end}}</select>
<select name="status">
{{range .StatusFilters}}<option value="{{.Value}}" {{if eq ($.Query.Get "status") .Value}}selected{{end}}>{{.Label}}</option>
{{end}}</select>
<input name="min" type="number" placeholder="min score" value="{{.Query.Get "min"}}" style="width:6rem">
<button>filter</button>
<button formaction="/api/run" formmethod="post">scrape now</button>
{{if .HiddenView}}<a href="/">← back to matches</a>{{else}}<a href="/?hidden=1">hidden entries</a> · <a href="/?status=tracked">pipeline</a>{{end}}
</form>
<table>
<tr><th>Score</th><th>Job</th><th>Company</th><th>Location</th><th>Fit</th><th>Source</th><th>Seen</th><th>Status</th><th></th></tr>
{{range .Jobs}}{{$job := .}}
<tr id="job-{{.ID}}"{{if .Tracked}} class="st-{{.Status}}"{{end}}>
<td class="score">{{.Score}}</td>
<td><a class="title" href="{{.URL}}" target="_blank" rel="noopener">{{.Title}}</a><div class="muted">{{range .Reasons}}{{.}} {{end}}{{if .Tracked}} · {{.Status}} {{reltime .StatusAt}}{{end}}</div></td>
<td>{{.Company}}</td>
<td>{{.Location}}</td>
<td class="fit">{{.Fit}}</td>
<td>{{.Source}}</td>
<td class="muted">{{reltime .FirstSeen}}</td>
<td><select class="status" data-id="{{.ID}}" title="Application status">
<option value="" {{if eq .Status $.StatusNone}}selected{{end}}>— not applied</option>
{{range $.Statuses}}<option value="{{.}}" {{if eq . $job.Status}}selected{{end}}>{{.}}</option>
{{end}}</select></td>
<td class="actions">
{{if $.HiddenView}}<button class="act-btn" data-id="{{.ID}}" data-action="unhide" title="Restore">↩ restore</button>
{{else}}<button class="act-btn" data-id="{{.ID}}" data-action="hide" title="Not interested">✕</button>{{end}}
</td>
</tr>
{{end}}
</table>
<script>
document.querySelectorAll('.act-btn').forEach(btn => {
  btn.addEventListener('click', async () => {
    btn.disabled = true;
    const res = await fetch('/api/jobs/' + btn.dataset.id + '/' + btn.dataset.action, {method: 'POST'});
    if (!res.ok) { btn.disabled = false; return; }
    document.getElementById('job-' + btn.dataset.id).remove();
  });
});
document.querySelectorAll('select.status').forEach(sel => {
  sel.addEventListener('change', async () => {
    sel.disabled = true;
    const res = await fetch('/api/jobs/' + sel.dataset.id + '/status?value=' + encodeURIComponent(sel.value), {method: 'POST'});
    // Reload so the row's colour and any status filter follow the change.
    if (res.ok) { location.reload(); } else { sel.disabled = false; }
  });
});
</script>
</body>
</html>`
