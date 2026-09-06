# Contributing

Issues and pull requests are welcome. Two areas are especially open:

- **The dashboard** (Go): new modules, better charts, terminal compatibility.
- **The instrument** (`instrument/ompool.scd`): a different mapping of the data
  onto sound is a legitimate contribution. If it is a new style rather than a
  refinement, add it as a second `.scd` and describe it in the README.

## Working on the code

```bash
git clone https://github.com/pat-riley/ompool
cd ompool
go test ./...
./scripts/dev audio   # rebuilds and relaunches on every .go save (Linux, needs inotify-tools)
```

Before opening a pull request run `gofmt -l .`, `go vet ./...`, and
`go test ./...`; CI runs the same plus cross-compiles for macOS and Windows.

## Working on the instrument

Open `instrument/ompool.scd` in SCIDE and evaluate the whole block. With SCIDE
holding the OSC port, `ompool audio` feeds it instead of launching its own
sclang, so you can edit and re-evaluate while the dashboard runs. To audition
changes without waiting on the network, evaluate `instrument/test_sender.scd`.
Rebuild ompool to embed your changes.

## Commit messages

One change per commit, with a subject line that says what the commit does.
Explain why in the body if it is not obvious from the diff.
