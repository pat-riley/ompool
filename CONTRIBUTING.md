# Contributing to ompool

Thanks for taking an interest. Issues and pull requests are welcome, whether
you write Go, write SuperCollider, or just have an ear for what the network
should sound like.

## Where help is wanted

- **The dashboard** (Go): new modules, better charts, terminal compatibility,
  and support for self-hosted mempool instances.
- **The instrument** (`instrument/ompool.scd`): a different mapping of the data
  onto sound is a legitimate contribution. If it is a new style rather than a
  refinement of the current one, add it as a second `.scd` and describe it in
  the README rather than replacing what is there.
- **Other instruments**: ompool speaks plain OSC (see the README), so a Pure
  Data patch, a Max patch, or a Tidal script that plays the feed is welcome
  under `contrib/`.

## Reporting a bug

Open an issue with the bug report template. Include the output of
`ompool version`, your terminal and its size, and what you expected. For
anything audio related, attach the tail of `~/.cache/ompool/instrument.log`
(or the platform equivalent) and say whether SCIDE was open at the time.

## Proposing a change

For anything larger than a small fix, open an issue first so the approach can
be agreed before you spend time on it.

1. Fork the repository and create a branch from `main`.
2. Make one logical change per branch. Keep unrelated cleanups for their own
   pull request.
3. Add or update tests. The dashboard renders to strings, so most behaviour can
   be asserted with a `renderX(...)` call and `strings.Contains`.
4. Run the checks CI will run:

   ```bash
   gofmt -l .        # prints nothing when clean
   go vet ./...
   go test ./...
   ```

5. Open a pull request. The template asks for a summary and how you tested it.

CI runs on every pull request and also cross-compiles for macOS and Windows,
so please do not add Linux-only calls outside a `//go:build` guarded file
(see `instrument/proc_unix.go` for the pattern).

## Working on the code

```bash
git clone https://github.com/pat-riley/ompool
cd ompool
go test ./...
./scripts/dev audio   # rebuilds and relaunches on every .go save (Linux, needs inotify-tools)
```

Set `OMPOOL_API_URL` to a self-hosted mempool instance if you are iterating
quickly; the public API is rate limited.

## Working on the instrument

Open `instrument/ompool.scd` in SCIDE and evaluate the whole block. With SCIDE
holding the OSC port, `ompool audio` feeds it instead of launching its own
sclang, so you can edit and re-evaluate while the dashboard runs. To audition
changes without waiting on the network, evaluate `instrument/test_sender.scd`.
Rebuild ompool to embed your changes. Keep the data-to-music map at the top of
the file accurate; it is the documentation people read first.

## Commit messages

One change per commit, with a subject line that says what the commit does in
the imperative ("Add", "Fix", not "Added"). Explain why in the body if it is
not obvious from the diff.

## Releases

Maintainers cut releases by pushing a semver tag (`git tag v0.2.0 && git push
origin v0.2.0`). GitHub Actions builds the binaries and drafts the release
notes from merged pull requests, so a clear PR title becomes a clear
changelog line.

## License

By contributing you agree that your contributions are licensed under the
project's [MIT license](LICENSE).
