# job-scout

Self-hosted job-search scraper (Go service, Docker + kustomize). See
README.md for what it does and how to run it. Conventions that must hold
when changing code:

## Nothing personal in the code

The whole point of this repo is that the search lives in `config.yaml`, not
in Go. A stack name, a city, a company, a language preference or a listing
URL in a `.go` file is a bug — it belongs in `internal/config` and in
`config.example.yaml`. The one exception is data that describes the *board*
rather than the searcher: dev.bg ads really are in Bulgaria, justjoin.it
really is Polish.

## Job links: always English (or German)

Job URLs stored and shown in the UI must point to a version of the ad the
reader can actually read:

- jobs.bg / tech.bg ads → `https://www.jobs.bg/en/job/<id>` (tech.bg is the
  same platform and shares ad ids; always emit the jobs.bg /en/ URL).
- dev.bg ads → `https://h512.com/...` (dev.bg's English mirror,
  `hreflang="en"`, same paths).
- When adding a source, check for an English variant of the ad URL
  (hreflang tags, `/en/` paths, language subdomains) and emit that. Ads in
  languages outside `profile.languages` are dropped by the language filter
  (`internal/match/lang.go`) anyway.

## Other conventions

- Adding a source: implement `Name()`, `Domains()` and `Fetch(ctx)`, add
  the name to `config.KnownSources`, wire it into `source.All`. The web
  search skip-list is derived from `Domains()`, so there is no second list
  to remember. `source_test.go` asserts the registry and the config list
  agree.
- Everything a board needs to know about *what* to search for comes from
  its `config.Source` block. A source with no listing URLs logs a warning
  and is skipped rather than falling back to a built-in default.
- Job identity: `ID` = source+URL, `ContentKey` = normalized company+title.
  Hidden jobs suppress re-surfacing content-key siblings; keep that
  invariant when touching `store.Merge`.
- The web UI derives its source and fit dropdowns from the stored jobs, so
  it never needs updating when sources or location labels change.
- `config.example.yaml` is the schema's documentation and is parsed by a
  test — keep it current when adding an option.
- Changing an existing source's URL format needs no purge rule here;
  deployments handle their own state. Exclusions are re-applied on boot.
