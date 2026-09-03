package mempool

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type LiveStats struct {
	VBytesPerSecond float64 `json:"vBytesPerSecond"`
	HasFlow         bool
	Transactions    []Transaction `json:"transactions"`
}

const liveUpdateInterval = 100 * time.Millisecond

func (c *Client) StreamStats(ctx context.Context) <-chan LiveStats {
	raw := make(chan LiveStats)
	out := make(chan LiveStats)
	go func() {
		defer close(raw)
		for ctx.Err() == nil {
			if err := c.streamOnce(ctx, raw); err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()
	go coalesceLiveStats(ctx, raw, out, liveUpdateInterval)
	return out
}

// coalesceLiveStats prevents bursts from the websocket from forcing a full UI
// render for every transaction while retaining the newest flow measurement and
// every transaction received during the interval.
func coalesceLiveStats(ctx context.Context, in <-chan LiveStats, out chan<- LiveStats, interval time.Duration) {
	defer close(out)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var pending LiveStats
	flush := func() bool {
		if !pending.HasFlow && len(pending.Transactions) == 0 {
			return true
		}
		select {
		case out <- pending:
			pending = LiveStats{}
			return true
		case <-ctx.Done():
			return false
		}
	}

	for {
		select {
		case stats, ok := <-in:
			if !ok {
				flush()
				return
			}
			if stats.HasFlow {
				pending.VBytesPerSecond = stats.VBytesPerSecond
				pending.HasFlow = true
			}
			pending.Transactions = append(pending.Transactions, stats.Transactions...)
		case <-ticker.C:
			if !flush() {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) streamOnce(ctx context.Context, out chan<- LiveStats) error {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/ws"

	conn, _, err := websocket.Dial(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	want := map[string]any{"action": "want", "data": []string{"stats"}}
	if err := wsjson.Write(ctx, conn, want); err != nil {
		return err
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"track-mempool-txids": true}); err != nil {
		return err
	}
	for {
		var payload struct {
			VBytesPerSecond *float64      `json:"vBytesPerSecond"`
			Transactions    []Transaction `json:"transactions"`
		}
		if err := wsjson.Read(ctx, conn, &payload); err != nil {
			return err
		}
		if payload.VBytesPerSecond == nil && len(payload.Transactions) == 0 {
			continue
		}
		stats := LiveStats{Transactions: payload.Transactions}
		if payload.VBytesPerSecond != nil {
			stats.VBytesPerSecond = *payload.VBytesPerSecond
			stats.HasFlow = true
		}
		select {
		case out <- stats:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
