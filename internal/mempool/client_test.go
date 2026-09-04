package mempool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestFetchOverview(t *testing.T) {
	client := NewClient("https://example.test/api")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/fees/recommended":
			return response(http.StatusOK, `{"fastestFee":12,"halfHourFee":8,"hourFee":5,"economyFee":2,"minimumFee":1}`), nil
		case "/api/mempool":
			return response(http.StatusOK, `{"count":12345,"vsize":6789000,"total_fee":321000}`), nil
		case "/api/v1/blocks":
			return response(http.StatusOK, `[{"id":"abc","height":900001,"timestamp":1700000000,"tx_count":3000,"size":1600000,"weight":3990000,"extras":{"pool":{"name":"Foundry USA","slug":"foundryusa"}}}]`), nil
		case "/api/v1/fees/mempool-blocks":
			return response(http.StatusOK, `[{"blockSize":1500000,"blockVSize":998000,"nTx":2500,"totalFees":1250000,"medianFee":8.2,"feeRange":[1,3,5,8,12]}]`), nil
		case "/api/v1/difficulty-adjustment":
			return response(http.StatusOK, `{"progressPercent":32.5,"difficultyChange":0.78,"estimatedRetargetDate":1735514828279,"remainingBlocks":1361,"remainingTime":811481279,"nextRetargetHeight":876960}`), nil
		case "/api/mempool/recent":
			return response(http.StatusOK, `[{"txid":"abc123","fee":420,"vsize":140,"value":25000000}]`), nil
		case "/api/v1/prices":
			return response(http.StatusOK, `{"USD":100000}`), nil
		default:
			return response(http.StatusNotFound, "not found"), nil
		}
	})

	snapshot, err := client.FetchOverview(context.Background())
	if err != nil {
		t.Fatalf("FetchOverview() error = %v", err)
	}
	if snapshot.Blocks[0].Height != 900001 || snapshot.Blocks[0].Extras.Pool.Name != "Foundry USA" || snapshot.Fees.Fastest != 12 || snapshot.Mempool.Count != 12345 || snapshot.ProjectedBlocks[0].MedianFee != 8.2 || snapshot.Difficulty.ProgressPercent != 32.5 || snapshot.Recent[0].TxID != "abc123" || snapshot.Prices.USD != 100000 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestFetchOverviewRejectsBadStatus(t *testing.T) {
	client := NewClient("https://example.test/api")
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusServiceUnavailable, "nope"), nil
	})

	if _, err := client.FetchOverview(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestFetchMining(t *testing.T) {
	client := NewClient("https://example.test/api")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/mining/hashrate/3m":
			return response(http.StatusOK, `{"hashrates":[{"timestamp":1700000000,"avgHashrate":9e20}],"difficulty":[{"time":1699000000,"height":800000,"difficulty":1.2e14,"adjustment":1.02}],"currentHashrate":9.2e20,"currentDifficulty":1.25e14}`), nil
		case "/api/v1/mining/pools/1w":
			return response(http.StatusOK, `{"pools":[{"name":"Foundry USA","blockCount":31,"rank":1,"avgMatchRate":98.2}],"blockCount":100,"lastEstimatedHashrate":9.1e20}`), nil
		case "/api/v1/mining/reward-stats/144":
			return response(http.StatusOK, `{"startBlock":900000,"endBlock":900143,"totalReward":"45360000000","totalFee":"360000000","totalTx":"650000"}`), nil
		default:
			return response(http.StatusNotFound, "not found"), nil
		}
	})

	mining, err := client.FetchMining(context.Background())
	if err != nil {
		t.Fatalf("FetchMining() error = %v", err)
	}
	if mining.Hashrate.CurrentHashrate != 9.2e20 || mining.Pools.Pools[0].Name != "Foundry USA" || mining.Rewards.TotalTx != "650000" {
		t.Fatalf("unexpected mining snapshot: %+v", mining)
	}
}

func TestFetchBlockHistoryKeepsConcurrentPagesInChainOrder(t *testing.T) {
	client := NewClient("https://example.test/api")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/blocks/985":
			return response(http.StatusOK, `[{"id":"page-one","height":985}]`), nil
		case "/api/v1/blocks/970":
			return response(http.StatusOK, `[{"id":"page-two","height":970}]`), nil
		default:
			return response(http.StatusNotFound, "not found"), nil
		}
	})
	blocks, err := client.FetchBlockHistory(context.Background(), 1000, 2)
	if err != nil {
		t.Fatalf("FetchBlockHistory() error = %v", err)
	}
	if len(blocks) != 2 || blocks[0].Height != 985 || blocks[1].Height != 970 {
		t.Fatalf("history out of order: %+v", blocks)
	}
}

func TestCoalesceLiveStats(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan LiveStats, 3)
	out := make(chan LiveStats)
	go coalesceLiveStats(ctx, in, out, time.Hour)

	in <- LiveStats{VBytesPerSecond: 100, HasFlow: true, Transactions: []Transaction{{TxID: "one"}}}
	in <- LiveStats{VBytesPerSecond: 200, HasFlow: true, Transactions: []Transaction{{TxID: "two"}}}
	close(in)

	got := <-out
	if !got.HasFlow || got.VBytesPerSecond != 200 {
		t.Fatalf("expected latest flow measurement, got %+v", got)
	}
	if len(got.Transactions) != 2 || got.Transactions[0].TxID != "one" || got.Transactions[1].TxID != "two" {
		t.Fatalf("expected all transactions in order, got %+v", got.Transactions)
	}
	if _, ok := <-out; ok {
		t.Fatal("expected output to close after the final batch")
	}
}
