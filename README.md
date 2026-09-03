# ompool

A Bitcoin mempool and blockchain monitor for the terminal, built for Omarchy.

Run `ompool` to open the interactive module picker, or open a module directly:

```text
ompool overview
ompool blockchain
ompool blocks
ompool mempool
ompool fees
```

The overview refreshes from `https://mempool.space/api` every 15 seconds. To
use a self-hosted mempool instance instead, set `OMPOOL_API_URL` to its API
base URL.

## Status

Early development.
