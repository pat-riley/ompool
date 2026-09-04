# ompool

A Bitcoin mempool and blockchain monitor for the terminal, built for Omarchy.

Run `ompool` to open the interactive module picker, or open a module directly:

```text
ompool overview
ompool blockchain
ompool blocks
ompool transactions
ompool mempool
ompool fees
ompool difficulty
ompool mining
ompool lightning
ompool explorer
```

Every module uses one concurrently fetched network snapshot and a coalesced
live websocket stream, then refreshes the snapshot every 15 seconds. Dedicated
views are available for the chain, confirmed blocks, live transactions, the
mempool, fees, and difficulty adjustment. To use a self-hosted mempool
instance, set `OMPOOL_API_URL` to its API base URL.

Mining provides a three-month line chart overlaying raw hashrate, its seven-day
moving average, and difficulty; reward summaries; a 100-cell pool-share
mosaic; and recent difficulty adjustments. Lightning and Explorer remain
available as direct-command scaffolds but are hidden from the focused launcher.

The Blockchain and Recent Blocks modules also load two older block pages for
roughly 45 scrollable blocks. Mining-aware screens fetch three-month network
hashrate history, seven-day pool distribution, and 144-block reward statistics in parallel;
other screens skip those requests entirely.

For automatic rebuilds while developing, run:

```bash
./scripts/dev overview
```

## Status

Early development.
