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
		Reward        int64     `json:"reward"`
		MedianFee     float64   `json:"medianFee"`
		FeeRange      []float64 `json:"feeRange"`
		TotalFees     int64     `json:"totalFees"`
		AvgFee        int64     `json:"avgFee"`
		AvgFeeRate    float64   `json:"avgFeeRate"`
		AvgTxSize     float64   `json:"avgTxSize"`
		SegwitTotalTx int       `json:"segwitTotalTxs"`
		MatchRate     float64   `json:"matchRate"`
		Pool          struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"pool"`
	} `json:"extras"`
}

type HashratePoint struct {
	Timestamp   int64   `json:"timestamp"`
	AvgHashrate float64 `json:"avgHashrate"`
}

type DifficultyPoint struct {
	Time       int64   `json:"time"`
	Height     int64   `json:"height"`
	Difficulty float64 `json:"difficulty"`
	Adjustment float64 `json:"adjustment"`
}

type HashrateStats struct {
	Hashrates         []HashratePoint   `json:"hashrates"`
	Difficulty        []DifficultyPoint `json:"difficulty"`
	CurrentHashrate   float64           `json:"currentHashrate"`
	CurrentDifficulty float64           `json:"currentDifficulty"`
}

type MiningPool struct {
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	BlockCount   int     `json:"blockCount"`
	Rank         int     `json:"rank"`
	EmptyBlocks  int     `json:"emptyBlocks"`
	AvgMatchRate float64 `json:"avgMatchRate"`
}

type PoolStats struct {
	Pools                 []MiningPool `json:"pools"`
	BlockCount            int          `json:"blockCount"`
	LastEstimatedHashrate float64      `json:"lastEstimatedHashrate"`
}

type RewardStats struct {
	StartBlock  int64  `json:"startBlock"`
	EndBlock    int64  `json:"endBlock"`
	TotalReward string `json:"totalReward"`
	TotalFee    string `json:"totalFee"`
	TotalTx     string `json:"totalTx"`
}

type MiningStats struct {
	Hashrate HashrateStats
	Pools    PoolStats
	Rewards  RewardStats
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
	Mining          MiningStats
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

// FetchMining loads slower-moving mining analytics independently from the
// core snapshot so screens that do not display them avoid the extra requests.
func (c *Client) FetchMining(ctx context.Context) (MiningStats, error) {
	type result struct {
		name string
		err  error
	}
	var mining MiningStats
	results := make(chan result, 3)
	go func() { results <- result{"hashrate", c.getJSON(ctx, "/v1/mining/hashrate/3m", &mining.Hashrate)} }()
	go func() { results <- result{"pools", c.getJSON(ctx, "/v1/mining/pools/1w", &mining.Pools)} }()
	go func() { results <- result{"rewards", c.getJSON(ctx, "/v1/mining/reward-stats/144", &mining.Rewards)} }()
	for range 3 {
		item := <-results
		if item.err != nil {
			return MiningStats{}, fmt.Errorf("fetch mining %s: %w", item.name, item.err)
		}
	}
	return mining, nil
}

// FetchBlockHistory requests older v1 block pages concurrently. The caller
// supplies the current tip, allowing deterministic non-overlapping 15-block
// windows without a serial pagination waterfall.
func (c *Client) FetchBlockHistory(ctx context.Context, tip int64, pages int) ([]Block, error) {
	if tip <= 0 || pages <= 0 {
		return nil, nil
	}
	type result struct {
		page   int
		blocks []Block
		err    error
	}
	results := make(chan result, pages)
	for page := range pages {
		go func() {
			start := tip - int64((page+1)*15)
			var blocks []Block
			err := c.getJSON(ctx, fmt.Sprintf("/v1/blocks/%d", start), &blocks)
			results <- result{page: page, blocks: blocks, err: err}
		}()
	}
	ordered := make([][]Block, pages)
	for range pages {
		item := <-results
		if item.err != nil {
			return nil, fmt.Errorf("fetch block history page %d: %w", item.page+1, item.err)
		}
		ordered[item.page] = item.blocks
	}
	var history []Block
	for _, blocks := range ordered {
		history = append(history, blocks...)
	}
	return history, nil
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
