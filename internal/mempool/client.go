package mempool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultBaseURL = "https://mempool.space/api"

type Client struct {
	baseURL string
	http    *http.Client
}

type Fees struct {
	Fastest  int     `json:"fastestFee"`
	HalfHour int     `json:"halfHourFee"`
	Hour     int     `json:"hourFee"`
	Economy  int     `json:"economyFee"`
	Minimum  float64 `json:"minimumFee"`
}

type Mempool struct {
	Count    int     `json:"count"`
	VSize    int64   `json:"vsize"`
	TotalFee float64 `json:"total_fee"`
}

type Block struct {
	ID        string `json:"id"`
	Height    int64  `json:"height"`
	Timestamp int64  `json:"timestamp"`
	TxCount   int    `json:"tx_count"`
	Size      int64  `json:"size"`
	Weight    int64  `json:"weight"`
	Extras    struct {
		Pool struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"pool"`
	} `json:"extras"`
}

type ProjectedBlock struct {
	BlockSize  int64     `json:"blockSize"`
	BlockVSize float64   `json:"blockVSize"`
	TxCount    int       `json:"nTx"`
	TotalFees  float64   `json:"totalFees"`
	MedianFee  float64   `json:"medianFee"`
	FeeRange   []float64 `json:"feeRange"`
}

type DifficultyAdjustment struct {
	ProgressPercent       float64 `json:"progressPercent"`
	DifficultyChange      float64 `json:"difficultyChange"`
	EstimatedRetargetDate int64   `json:"estimatedRetargetDate"`
	RemainingBlocks       int     `json:"remainingBlocks"`
	RemainingTime         int64   `json:"remainingTime"`
	NextRetargetHeight    int64   `json:"nextRetargetHeight"`
}

type Transaction struct {
	TxID  string  `json:"txid"`
	Fee   int64   `json:"fee"`
	VSize float64 `json:"vsize"`
	Value int64   `json:"value"`
}

type Prices struct {
	USD float64 `json:"USD"`
}

type Overview struct {
	Fees            Fees
	Mempool         Mempool
	Blocks          []Block
	ProjectedBlocks []ProjectedBlock
	Difficulty      DifficultyAdjustment
	Recent          []Transaction
	Prices          Prices
	Fetched         time.Time
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) FetchOverview(ctx context.Context) (Overview, error) {
	type result struct {
		name string
		err  error
	}

	var snapshot Overview
	results := make(chan result, 7)

	go func() { results <- result{"fees", c.getJSON(ctx, "/v1/fees/recommended", &snapshot.Fees)} }()
	go func() { results <- result{"mempool", c.getJSON(ctx, "/mempool", &snapshot.Mempool)} }()
	go func() { results <- result{"blocks", c.getJSON(ctx, "/v1/blocks", &snapshot.Blocks)} }()
	go func() {
		results <- result{"projected blocks", c.getJSON(ctx, "/v1/fees/mempool-blocks", &snapshot.ProjectedBlocks)}
	}()
	go func() {
		results <- result{"difficulty", c.getJSON(ctx, "/v1/difficulty-adjustment", &snapshot.Difficulty)}
	}()
	go func() { results <- result{"recent transactions", c.getJSON(ctx, "/mempool/recent", &snapshot.Recent)} }()
	go func() { results <- result{"prices", c.getJSON(ctx, "/v1/prices", &snapshot.Prices)} }()

	for range 7 {
		item := <-results
		if item.err != nil {
			return Overview{}, fmt.Errorf("fetch %s: %w", item.name, item.err)
		}
	}

	if len(snapshot.Blocks) == 0 {
		return Overview{}, fmt.Errorf("fetch blocks: empty response")
	}
	snapshot.Fetched = time.Now()
	return snapshot, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ompool/dev")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
