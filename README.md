# ompool

A Bitcoin mempool and blockchain monitor for the terminal, built for Omarchy.

Run `ompool` to open the title screen: the module picker sits in the centre
while a dimmed chain of blocks scrolls above it and dummy flow, hashrate, and
transaction value charts play beneath. The same backdrop runs while a module
fetches its first snapshot. You can also open a module directly:

```text
ompool overview
ompool blockchain
ompool blocks
ompool transactions
ompool viewer
ompool mempool
ompool fees
ompool difficulty
ompool mining
ompool lightning
ompool explorer
```

Dashboard modules use a concurrently fetched network snapshot and a coalesced
live websocket stream, then refresh the snapshot every 15 seconds. Dedicated
views are available for the chain, confirmed blocks, live transactions, the
mempool, fees, and difficulty adjustment. To use a self-hosted mempool
instance, set `OMPOOL_API_URL` to its API base URL.

Transaction Viewer is available from the home screen or with `ompool viewer`.
Paste a transaction ID (or a mempool.space transaction URL) and press Enter to
inspect its inputs, output UTXOs and spend state, decoded metadata, and embedded
payload hex. Press Shift+L for the Len Sassaman ASCII-art tribute or Shift+G
for the genesis coinbase and its Times headline. On wide terminals, inputs,
outputs, and on-chain data each occupy an equal-width column; the UTXO columns
show only address, direction, and BTC amount. Tab switches the scroll focus
between them.

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
