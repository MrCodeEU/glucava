# Third-party notices

Glucava is licensed under the AGPL-3.0 (see [LICENSE](LICENSE)). It includes or links these components, all under licenses compatible with it.

## Bundled front-end files (`internal/web/static/`)

| File | Project | License |
|---|---|---|
| `datastar.js` | [Datastar](https://data-star.dev) | MIT |

The other files in that directory (`app.css`, `theme.js`, `glucose-import.js`, `favicon.svg`) are part of Glucava itself.

## Go dependencies

The full, current list is in `go.mod`. At the time of writing they are under MIT (for example PocketBase, cobra, chromedp, gomponents), BSD-3-Clause (for example the `golang.org/x` modules, `modernc.org` SQLite) and Apache-2.0. To regenerate the list with licenses:

```sh
go run github.com/google/go-licenses/v2@latest report ./...
```

## Container image

The Docker image is built on a Debian base with Chromium installed from the distribution. Those packages keep their own licenses; see the image's `/usr/share/doc`.
