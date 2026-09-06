# ompool

A live Bitcoin mempool and blockchain dashboard for the terminal, with an
optional SuperCollider instrument that plays the network as it happens.

Every transaction entering the mempool becomes a plucked note: small amounts
high, whales low, cheap fees left, urgent fees right. New blocks land as a gong
and step a chord progression. Fee pressure sets the harmonic tension, mempool
inflow sets the drum density, and the tempo itself follows how busy the network
is.

ompool is early alpha software: expect rough edges, and expect the sound to
change between releases.

## Install

With Go 1.27 or newer:

```bash
go install github.com/pat-riley/ompool@latest
```

Prebuilt binaries for Linux, macOS, and Windows are attached to each
[release](https://github.com/pat-riley/ompool/releases).

For sound you also need [SuperCollider](https://supercollider.github.io/downloads)
installed so that `sclang` is on your PATH, and a running audio server that it
can use (PipeWire or JACK on Linux, CoreAudio on macOS). The dashboard works
without it.

```bash
sudo pacman -S supercollider      # Arch
sudo apt install supercollider    # Debian / Ubuntu
brew install --cask supercollider # macOS
```

## Usage

`ompool` opens the title screen and module picker. You can also open a module
directly:

```text
ompool overview        the complete network dashboard
ompool audio           the overview, sonified
ompool blocks          a live feed of newly mined blocks
ompool transactions    live transactions entering the mempool
ompool viewer          inspect a transaction, its UTXOs, hex, and embedded data
ompool mempool         backlog, weight, and activity
ompool mining          hashrate, rewards, and pool distribution
ompool version         print the version
```

Press `Esc` to return to the picker and `q` to quit. Dashboard modules fetch a
network snapshot, subscribe to a live websocket stream, and refresh the
snapshot every 15 seconds. Data comes from the public
[mempool.space](https://mempool.space) API; set `OMPOOL_API_URL` to the API
base URL of a self-hosted instance to use that instead.

### Audio

`ompool audio` is the overview with two differences. Transactions are revealed
one per step of a musical clock rather than as they arrive, and the tempo
follows the network: transaction inflow and the mempool backlog map onto 70 bpm
(idle) to 160 bpm (saturated), easing between values so the beat never lurches.
A Tempo panel shows the bpm, the beat, recent history, and the launcher status.

When the view opens, ompool starts `sclang` on the instrument embedded in the
binary, and stops it again when you leave. Booting takes a few seconds. If
something already holds the OSC port, typically SuperCollider's IDE, ompool
feeds that instead of launching a second copy, so you can edit the instrument
live:

```bash
ompool instrument > my-instrument.scd   # dump the embedded script
ompool instrument --test-sender > test.scd   # a fake feed for auditioning it
```

Open the script in SCIDE, evaluate the whole block, then run `ompool audio`.
To have ompool launch your edited version itself, point `OMPOOL_INSTRUMENT` at
the file. The launcher writes the script it runs and `instrument.log` to
`~/.cache/ompool` (or the platform equivalent); look there if it goes quiet.

The mapping from data to music is documented at the top of
[`instrument/ompool.scd`](instrument/ompool.scd).

### Transaction viewer

Paste a transaction ID or a mempool.space transaction URL and press Enter to
inspect inputs, output UTXOs and spend state, decoded metadata, and embedded
payload hex. `Shift+L` loads the Len Sassaman ASCII-art tribute and `Shift+G`
the genesis coinbase with its Times headline. `Tab` moves the scroll focus
between columns.

## OSC protocol

ompool mirrors what it shows as OSC messages over UDP, so any instrument
(SuperCollider, Max, Pure Data, a DAW) can play in sync with the screen. All
arguments are float32.

```text
/btc/tx       value_sats fee_rate_sat_per_vb      as each transaction is revealed on screen
/btc/block    height tx_count                     when a new block is detected
/btc/fees     fastest halfHour hour economy min   sat/vB, from each snapshot
/btc/mempool  count vsize vbytes_per_second       from snapshots and live flow updates
/btc/clock    bpm step steps_per_bar              audio view only: one per grid step
```

The clock message lets an instrument lock its own sequencer to ompool's bar;
the embedded instrument adopts the tempo and re-aligns on each bar start.

## Configuration

| Variable | Default | Effect |
| --- | --- | --- |
| `OMPOOL_API_URL` | `https://mempool.space/api` | API base of a self-hosted mempool instance |
| `OMPOOL_OSC_ADDR` | `127.0.0.1:57120` | Where OSC messages go (sclang's default port) |
| `OMPOOL_OSC` | | `off` disables OSC output and the instrument |
| `OMPOOL_INSTRUMENT` | `auto` | `off` never launches sclang; a path runs that `.scd` instead of the embedded one |
| `OMPOOL_BPM` | | Pin the audio view's tempo instead of following the network |
| `OMPOOL_STEP` | `8` | Steps per 4/4 bar in the audio view |

## Development

```bash
git clone https://github.com/pat-riley/ompool
cd ompool
go test ./...
./scripts/dev overview   # rebuild and relaunch on every save (Linux, needs inotify-tools)
```

See [CONTRIBUTING.md](CONTRIBUTING.md).

Versions follow [semantic versioning](https://semver.org) and live only in git
tags. Pushing a `v*` tag makes GitHub Actions build the binaries, stamp the tag
into `ompool version` and the title screen, and publish a release. Builds from
a checkout report `dev` and the commit instead.

## License

[MIT](LICENSE)
