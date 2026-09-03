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

func (c *Client) StreamStats(ctx context.Context) <-chan LiveStats {
	out := make(chan LiveStats)
	go func() {
		defer close(out)
		for ctx.Err() == nil {
			if err := c.streamOnce(ctx, out); err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()
	return out
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
