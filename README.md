# job-scout

A job-search scraper you run yourself. It polls a set of job boards every
few hours, scores each ad against a profile you write, keeps the ones that
fit where you'd actually work, and serves them as a small web UI that
doubles as an application tracker. New matches can be pushed to Telegram.

One Go binary, one YAML file, a JSON file for state. No database, no
account, no third party seeing your search.

**You configure two things: what you're looking for, and where to deploy
it.** Everything else has a working default.

```sh
cp scraper/config.example.yaml scraper/config.yaml
$EDITOR scraper/config.yaml
make run           # http://localhost:8080
```

## Sources

| Source | Method | Needs |
|---|---|---|
| remoteok.com | public JSON API | — |
| remotive.com | public API, per configured category | — |
| weworkremotely.com | category RSS feeds you list | — |
| news.ycombinator.com | latest "Ask HN: Who is hiring?" via the Algolia API | — |
| arc.dev | remote listings from the embedded Next.js JSON; its `requiredCountries` field filters eligibility exactly | — |
| justjoin.it | listing pages (Polish/EU board, B2B-heavy); offer pages are re-read to check the ad's real language | — |
| dev.bg | listing pages (Bulgarian board); ads link to the English mirror h512.com | — |
| jobs.bg + tech.bg | searches on both skins of the same platform, deduped by ad id; ads link to the English pages | `FIRECRAWL_API_KEY` (Cloudflare) |
| web search | Serper (Google) finds ads beyond the fixed boards; Firecrawl fetches each page so the matcher scores full text rather than a snippet | `SERPER_API_KEY`, optionally `FIRECRAWL_API_KEY` |

A source with no listing URLs configured is skipped, as is one whose API
key is missing — so a config with no keys at all still runs the free
boards. Web search automatically avoids the domains your enabled boards
already cover, plus aggregators that only republish them.

## How matching works

Every ad runs the same pipeline, all of it driven by `config.yaml`:

1. **Title exclusions** — `profile.exclude` drops the ad outright.
2. **Company exclusions** — `profile.exclude_companies`, substring match.
3. **Language** — `profile.languages` keeps only ads you can read.
   Detection is heuristic (script, diacritics, stopwords); supported codes
   are bg, da, de, en, es, fi, fr, it, nl, pl, pt, ru, sv, uk.
4. **Focus** — `profile.require_any` must appear somewhere in the ad, and
   an `adjacent_titles` role is dropped unless a `core_title_terms` term is
   in the title too ("Python Developer" out, "Go/Python Developer" in).
5. **Geography** — the `location` markers classify the ad as local,
   region-remote, or a reject. Ads that state no geography are kept as
   "(unverified)" unless you set `keep_unverified: false`.
6. **Score** — keyword weights, plus `title_boost_weight` per boost term in
   the title, plus any `bonus_terms`. The ad is kept at `min_score`.

State lives in `$STATE_DIR/jobs.json`; entries unseen for 60 days are
pruned, and hidden ads stay hidden even when the same job reappears under a
different URL. Exclusions are re-applied to stored jobs on every boot, so
tightening the profile also cleans up what you already collected.

## Tracking applications

Each row carries a status, set from the dropdown in the job table:

| Status | Meaning |
|---|---|
| *(none)* | just a match — nothing sent |
| `applied` | sent, waiting for a reply |
| `interviewing` | they came back positively |
| `declined` | negative outcome, from either side |

A job with any status is *tracked*: it is exempt from the 60-day prune and
from the startup exclusion purge, so your application log survives a
profile change. The row is tinted by status, and the status dropdown in the
filter bar narrows the list — `not applied`, `applied — any`, or one
specific status.

Statuses come from `model.Statuses`; adding one there is enough for the
API, both dropdowns and the filter to pick it up. Stores written before
statuses existed are migrated on load: `applied: true` becomes
`status: applied`.

`scraper/config.example.yaml` documents every option inline.

## Running it

```sh
make run     # scrape on start, then every scrape_interval
make test
make vet
make docker  # builds ghcr.io/iceshack/job-scout/scraper:latest
```

Endpoints: `/` (UI with filters), `/api/jobs` (JSON, takes the same
`?source=`/`?fit=`/`?status=`/`?min=`/`?q=`/`?hidden=1` filters),
`POST /api/jobs/{id}/status?value=applied|interviewing|declined` (empty
value clears it), `POST /api/jobs/{id}/hide` and `/unhide`,
`POST /api/run` (trigger a scrape), `/health` (open, for probes).

Environment — all optional, read from `.env` locally (see `.env.example`):

| Variable | Effect |
|---|---|
| `CONFIG_PATH` | config file, default `config.yaml` |
| `STATE_DIR` | state directory, default `data` |
| `PORT` | listen port, default `8080` |
| `SITE_PASSWORD` | single shared password for the UI; unset means no auth |
| `SERPER_API_KEY` | enables web-search discovery |
| `FIRECRAWL_API_KEY` | enables page fetching and the Cloudflare-fronted boards |
| `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID` | push new matches to Telegram |

Auth is one shared password in a cookie, salted with `app.title` — changing
the title logs everyone out once. There are no user accounts; this is a
single-user tool.

## Deploying to Kubernetes

`k8s/base` is the portable workload (PVC, Service, Deployment).
`k8s/example-overlay` shows the four things you change: namespace, host,
image tag, and `config.yaml`.

```sh
cp -r k8s/example-overlay k8s/mine
$EDITOR k8s/mine/{kustomization,ingress,config}.yaml
kubectl apply -k k8s/mine
```

The profile is generated into the ConfigMap the pod mounts, with a content
hash in its name — editing it rolls the pod, which matters because the
config is read once at startup. Ingress is left to the overlay because it
is the most cluster-specific piece; a plain `Ingress` and a Traefik
`IngressRoute` are both provided.

Secrets are read from an optional `scraper-secret` Secret; create only the
keys you use:

```sh
kubectl create secret generic scraper-secret -n job-scout \
  --from-literal=SITE_PASSWORD=... \
  --from-literal=SERPER_API_KEY=... \
  --from-literal=FIRECRAWL_API_KEY=...
```

GitHub Actions builds and pushes `ghcr.io/OWNER/job-scout/scraper` on every
push to `main`. GHCR packages start out private: flip the package to public
once after the first push, or add an `imagePullSecrets` entry to your
overlay.

## Adding a source

Implement `Name()`, `Domains()` and `Fetch(ctx)` in `internal/source`, add
the name to `config.KnownSources`, and wire it in `source.All`. `Domains()`
is what keeps web search off the new board's turf — nothing else needs
updating. If the board publishes its ads in more than one language, emit
the English (or German) URL; see CLAUDE.md.

## License

MIT — see [LICENSE](LICENSE).
