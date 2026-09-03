package mempool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
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
		case "/api/blocks":
			return response(http.StatusOK, `[{"id":"abc","height":900001,"timestamp":1700000000,"tx_count":3000,"size":1600000,"weight":3990000}]`), nil
		case "/api/v1/fees/mempool-blocks":
			return response(http.StatusOK, `[{"blockSize":1500000,"blockVSize":998000,"nTx":2500,"totalFees":1250000,"medianFee":8.2,"feeRange":[1,3,5,8,12]}]`), nil
		case "/api/v1/difficulty-adjustment":
			return response(http.StatusOK, `{"progressPercent":32.5,"difficultyChange":0.78,"estimatedRetargetDate":1735514828279,"remainingBlocks":1361,"remainingTime":811481279,"nextRetargetHeight":876960}`), nil
		default:
			return response(http.StatusNotFound, "not found"), nil
		}
	})

	snapshot, err := client.FetchOverview(context.Background())
	if err != nil {
		t.Fatalf("FetchOverview() error = %v", err)
	}
	if snapshot.Blocks[0].Height != 900001 || snapshot.Fees.Fastest != 12 || snapshot.Mempool.Count != 12345 || snapshot.ProjectedBlocks[0].MedianFee != 8.2 || snapshot.Difficulty.ProgressPercent != 32.5 {
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
