package main

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ompool/internal/mempool"
)

func TestPickerDefaultsToOverview(t *testing.T) {
	m := newModel("")
	if m.cursor != 0 || modules[m.cursor].command != "overview" {
		t.Fatalf("expected overview to be selected by default")
	}
	if !strings.Contains(renderPicker(m.cursor, 80, 24), "> Overview") {
		t.Fatalf("expected picker to mark Overview as selected")
	}
	if !strings.Contains(renderPicker(m.cursor, 80, 24), "____  __  ___") {
		t.Fatalf("expected picker to include the ompool wordmark")
	}
}

func TestPickerNavigationAndSelection(t *testing.T) {
	m := newModel("")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)

	if m.screen != moduleScreen || m.active.command != "blocks" {
		t.Fatalf("expected recent blocks module after hidden blockchain entry, got %q", m.active.command)
	}
}

func TestHomepageHidesBlockchain(t *testing.T) {
	view := renderPicker(0, 100, 32)
	if strings.Contains(view, "> Blockchain") || strings.Contains(view, "  Blockchain") {
		t.Fatalf("homepage should not expose the blockchain module:\n%s", view)
	}
	if !validCommand("blockchain") {
		t.Fatal("the direct blockchain command should remain available")
	}
}

func TestDirectModule(t *testing.T) {
	m := newModel("fees")
	if m.screen != moduleScreen || m.active.command != "fees" {
		t.Fatalf("expected direct fees module")
	}
	if !m.loading || m.live != nil {
		t.Fatal("fees should fetch immediately without opening an unrelated high-frequency stream")
	}
}

func TestOnlyRealtimeModulesOpenTheLiveStream(t *testing.T) {
	for _, command := range []string{"overview", "transactions", "mempool"} {
		m := newModel(command)
		if m.live == nil {
			t.Errorf("%s should open the live transaction stream", command)
		}
		m.liveStop()
	}
	for _, command := range []string{"blockchain", "blocks", "fees", "difficulty"} {
		m := newModel(command)
		if m.live != nil {
			t.Errorf("%s should remain idle between snapshot refreshes", command)
		}
	}
}

func TestEveryDedicatedModuleRendersItsPrimaryContent(t *testing.T) {
	wants := map[string]string{
		"blockchain":   "BLOCKCHAIN / BLOCK ANATOMY",
		"blocks":       "CONFIRMED BLOCKS",
		"transactions": "LATEST TRANSACTIONS",
		"mempool":      "LIVE TRANSACTION VALUE",
		"fees":         "BLOCK FEE LADDER",
		"difficulty":   "MINING CADENCE",
		"mining":       "DIFFICULTY ADJUSTMENTS",
		"lightning":    "LIGHTNING NETWORK",
		"explorer":     "UNIVERSAL BITCOIN EXPLORER",
	}
	for _, candidate := range modules[1:] {
		view := renderActiveModule(&overviewRenderCache{}, candidate, sampleOverview(), []activitySample{{at: time.Now(), value: 6800}}, nil, false, nil, 120, 40, 0, 0, nil, 0, "")
		if !strings.Contains(view, wants[candidate.command]) {
			t.Errorf("%s module did not render %q:\n%s", candidate.command, wants[candidate.command], view)
		}
		if lipgloss.Width(view) != 120 || lipgloss.Height(view) != 40 {
			t.Errorf("%s module rendered at %dx%d, want 120x40", candidate.command, lipgloss.Width(view), lipgloss.Height(view))
		}
	}
}

func TestTransactionsModuleIncludesFlowMiningAndRollingList(t *testing.T) {
	view := renderActiveModule(&overviewRenderCache{}, moduleByCommand("transactions"), sampleOverview(), nil, []transactionValueSample{{at: time.Now(), value: 2.5}}, false, nil, 120, 40, 0, 0, nil, 0, "")
	for _, want := range []string{"LIVE TRANSACTION VALUE", "MINING NETWORK", "LATEST TRANSACTIONS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("transactions module should contain %q:\n%s", want, view)
		}
	}
}

func TestTransactionValueChartUsesFiveBTCCeiling(t *testing.T) {
	l := newLayout(120, 40)
	now := time.Now()
	view := renderTransactionValuePanel([]transactionValueSample{
		{at: now.Add(-90 * time.Second), value: 0.25},
		{at: now.Add(-45 * time.Second), value: 2.5},
		{at: now, value: 7.25},
	}, l, l.content)
	for _, want := range []string{"LIVE TRANSACTION VALUE", "5.0 │", "0.0 │", "5 BTC ceiling", "▪", "7.2500 BTC"} {
		if !strings.Contains(view, want) {
			t.Fatalf("transaction value chart should contain %q:\n%s", want, view)
		}
	}
	if lipgloss.Height(view) != lipgloss.Height(renderActivityPanel(nil, l, l.content)) {
		t.Fatal("transaction value and incoming-flow charts should have matching heights")
	}
}

func TestTransactionValueChartUsesSquareCollisionDensity(t *testing.T) {
	now := time.Now()
	samples := []transactionValueSample{
		{at: now.Add(-time.Minute), value: 1},
		{at: now.Add(-time.Minute), value: 1},
		{at: now.Add(-30 * time.Second), value: 2},
		{at: now.Add(-30 * time.Second), value: 2},
		{at: now.Add(-30 * time.Second), value: 2},
		{at: now.Add(-30 * time.Second), value: 2},
	}
	view := renderTransactionValueChart(samples, 80, 9)
	if !strings.Contains(view, "■") || !strings.Contains(view, "▣") || strings.Contains(view, "●") {
		t.Fatalf("expected square density markers without circular points:\n%s", view)
	}
}

func TestMempoolUsesLiveFlowAndBTCValueCharts(t *testing.T) {
	l := newLayout(120, 40)
	view := renderMempoolModule(sampleOverview(), []activitySample{{at: time.Now(), value: 3200}}, []transactionValueSample{{at: time.Now(), value: 1.5}}, l, 35)
	for _, want := range []string{"INCOMING TRANSACTION FLOW", "LIVE TRANSACTION VALUE", "1.5000 BTC"} {
		if !strings.Contains(view, want) {
			t.Fatalf("mempool module should contain %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "PROJECTED MEMPOOL BLOCKS") {
		t.Fatal("mempool module should no longer show projected blocks")
	}
}

func TestLiveTransactionsPopulateValueSeries(t *testing.T) {
	m := model{renderCache: &overviewRenderCache{}, live: make(chan mempool.LiveStats), overview: sampleOverview()}
	updated, _ := m.Update(liveStatsMsg{Transactions: []mempool.Transaction{{TxID: "value", Value: 125_000_000}}})
	m = updated.(model)
	if len(m.txValues) != 1 || m.txValues[0].value != 1.25 {
		t.Fatalf("live value series = %+v", m.txValues)
	}
}

func TestSnapshotRefreshPreservesRollingTransactions(t *testing.T) {
	m := newModel("")
	m.overview = sampleOverview()
	m.overview.Recent = make([]mempool.Transaction, recentTransactionLimit)
	for i := range m.overview.Recent {
		m.overview.Recent[i].TxID = fmt.Sprintf("live-%02d", i)
	}
	fresh := sampleOverview()
	fresh.Recent = []mempool.Transaction{{TxID: "api-new"}}
	updated, _ := m.Update(overviewMsg{snapshot: fresh})
	m = updated.(model)
	if len(m.overview.Recent) != recentTransactionLimit || m.overview.Recent[0].TxID != "api-new" {
		t.Fatalf("refresh reset rolling transactions: %d entries, first %q", len(m.overview.Recent), m.overview.Recent[0].TxID)
	}
	if m.overview.Recent[1].TxID != "live-00" {
		t.Fatalf("refresh discarded live history, second entry %q", m.overview.Recent[1].TxID)
	}
}

func TestDedicatedBlockModulesAcceptMouseWheelAcrossFullWidth(t *testing.T) {
	for _, command := range []string{"blockchain", "blocks"} {
		m := model{renderCache: &overviewRenderCache{}, screen: moduleScreen, active: moduleByCommand(command), overview: sampleOverview(), width: 120, height: 24}
		m.View()
		if !m.overBlockStack(119) {
			t.Fatalf("%s should accept wheel input at the right edge", command)
		}
	}
}

func TestBlockAnatomyUsesVerticalBrailleCapacityChamber(t *testing.T) {
	card := plain(strings.Join(blockAnatomyCard(blockAnatomy{
		label: "#965,356", pool: "Foundry USA", transactions: 2837,
		size: 1_800_000, weight: 2_000_000, fee: 2.5, fullness: 0.5,
	}, 72), "\n"))
	for _, want := range []string{"#965,356", "Foundry USA", "2,837 transactions", "⠂", "⣤", "⣿", "█", "░"} {
		if !strings.Contains(card, want) {
			t.Fatalf("block anatomy should contain %q:\n%s", want, card)
		}
	}
}

func TestBlockTimelineShowsMiningInterval(t *testing.T) {
	connector := plain(strings.Join(timelineConnector(formatInterval(379), 80), "\n"))
	if !strings.Contains(connector, "6m 19s between blocks") || !strings.Contains(connector, "▼") {
		t.Fatalf("unexpected timeline connector:\n%s", connector)
	}
}

func TestRecentBlockCadenceChartSpansPanelWidth(t *testing.T) {
	const width = 120
	view := plain(renderBlockVisualSummary(sampleOverview().Blocks, width, 8))
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "CADENCE / MINUTES BETWEEN BLOCKS") || i+1 >= len(lines) {
			continue
		}
		glyphs := 0
		for _, glyph := range []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"} {
			glyphs += strings.Count(lines[i+1], glyph)
		}
		if glyphs != width-4 {
			t.Fatalf("cadence chart used %d columns, want %d:\n%s", glyphs, width-4, view)
		}
		return
	}
	t.Fatal("cadence chart was not rendered")
}

func moduleByCommand(command string) module {
	for _, candidate := range modules {
		if candidate.command == command {
			return candidate
		}
	}
	return module{}
}

func TestDedicatedModulesFitResponsiveTerminals(t *testing.T) {
	sizes := []struct{ width, height int }{{160, 50}, {120, 40}, {96, 30}, {80, 24}, {60, 20}, {40, 14}, {30, 8}}
	for _, candidate := range modules[1:] {
		for _, size := range sizes {
			view := renderActiveModule(&overviewRenderCache{}, candidate, sampleOverview(), nil, nil, false, nil, size.width, size.height, 0, 0, nil, 0, "")
			if lipgloss.Width(view) != size.width || lipgloss.Height(view) != size.height {
				t.Errorf("%s at %dx%d rendered at %dx%d", candidate.command, size.width, size.height, lipgloss.Width(view), lipgloss.Height(view))
			}
			opened, closed := strings.Count(view, "╭"), strings.Count(view, "╰")
			allowedViewportCard := candidate.command == "blockchain" && opened-closed == 1
			if opened != closed && !allowedViewportCard {
				t.Errorf("%s at %dx%d clipped a panel border (%d opened, %d closed)", candidate.command, size.width, size.height, opened, closed)
			}
		}
	}
}

func TestModuleLoadingScreenIsBorderedAndBranded(t *testing.T) {
	view := renderActiveModule(&overviewRenderCache{}, modules[2], mempool.Overview{}, nil, nil, true, nil, 100, 30, 0, 0, nil, 0, "")
	for _, want := range []string{"╭", "╰", "____  __  ___", "LOADING RECENT BLOCKS", "fetching chain state in parallel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("loading screen should contain %q:\n%s", want, view)
		}
	}
	if lipgloss.Width(view) != 100 || lipgloss.Height(view) != 30 {
		t.Fatalf("loading screen rendered at %dx%d", lipgloss.Width(view), lipgloss.Height(view))
	}
}

func TestMiningHashrateChartMatchesTransactionFlowHeight(t *testing.T) {
	l := newLayout(120, 40)
	snapshot := sampleOverview()
	hashrate := renderHashratePanel(snapshot.Mining.Hashrate, l, l.content)
	flow := renderActivityPanel([]activitySample{{at: time.Now(), value: 4200}}, l, l.content)
	if lipgloss.Height(hashrate) != lipgloss.Height(flow) {
		t.Fatalf("hashrate chart height %d, transaction flow height %d", lipgloss.Height(hashrate), lipgloss.Height(flow))
	}
}

func TestMiningChartOverlaysAllThreeSeries(t *testing.T) {
	view := renderMiningModule(sampleOverview(), newLayout(120, 40), 35)
	for _, want := range []string{"• HASHRATE", "◆ 7D MA", "× DIFFICULTY", "POOL SHARE MOSAIC", "DIFFICULTY ADJUSTMENTS", "+2.50%"} {
		if !strings.Contains(view, want) {
			t.Fatalf("mining module should contain %q:\n%s", want, view)
		}
	}
}

func TestHomeScreenKeepsTheModuleListFocused(t *testing.T) {
	view := renderPicker(0, 100, 32)
	for _, want := range []string{"Overview", "Recent Blocks", "Transactions", "Mempool", "Mining"} {
		if !strings.Contains(view, want) {
			t.Fatalf("home screen should include %q", want)
		}
	}
	for _, hidden := range []string{"Blockchain", "Fees", "Difficulty", "Lightning", "Explorer"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("home screen should not include %q", hidden)
		}
	}
}

func TestOverviewUsesDashboardInsteadOfWordmark(t *testing.T) {
	snapshot := mempool.Overview{
		Fees:    mempool.Fees{Fastest: 12, HalfHour: 8, Hour: 5, Economy: 2},
		Mempool: mempool.Mempool{Count: 12000, VSize: 8_000_000, TotalFee: 500_000},
		Blocks: []mempool.Block{
			{Height: 900001, Timestamp: time.Now().Add(-4 * time.Minute).Unix(), TxCount: 3200, Weight: 3_990_000},
		},
		ProjectedBlocks: []mempool.ProjectedBlock{
			{BlockVSize: 990_000, TxCount: 2500, MedianFee: 8.2},
		},
		Recent:  []mempool.Transaction{{TxID: "1234567890abcdef", Fee: 420, VSize: 140, Value: 25_000_000}},
		Prices:  mempool.Prices{USD: 100_000},
		Fetched: time.Now(),
	}

	newTXIDs := map[string]struct{}{"1234567890abcdef": {}}
	view := renderOverview(snapshot, []activitySample{{at: time.Now(), value: 120}, {at: time.Now(), value: 450}, {at: time.Now(), value: 300}}, false, nil, 120, 40, 0, 3, newTXIDs)
	for _, section := range []string{"BITCOIN MAINNET / OVERVIEW", "MEMPOOL BLOCKS", "CONFIRMED", "INCOMING TRANSACTION FLOW", "8000 │", "4000 │", "LATEST TRANSACTIONS", "$25,000.00", "FEE MARKET", "30 MIN"} {
		if !strings.Contains(view, section) {
			t.Fatalf("expected overview to contain %q", section)
		}
	}
	if strings.Contains(view, "___  _ __ ___") {
		t.Fatal("overview should not contain the launcher wordmark")
	}
	if strings.Contains(view, "#900,002") || strings.Contains(view, "NEXT +") {
		t.Fatal("projected blocks should not show block numbers")
	}
	if strings.Contains(view, "NEW TX") {
		t.Fatal("new transaction animation should not add indicator text")
	}
	if got := lipgloss.Height(view); got != 40 {
		t.Fatalf("overview height = %d, want terminal height 40", got)
	}
}

// A 40 row terminal spends its last rows on the flow chart, so the panels it
// cannot also fit only appear once the terminal is tall enough for both.
func TestATallTerminalShowsEveryPanel(t *testing.T) {
	view := renderOverview(sampleOverview(), []activitySample{{at: time.Now(), value: 6800}}, false, nil, 160, 56, 0, 0, nil)
	for _, section := range []string{"MEMPOOL BLOCKS", "INCOMING TRANSACTION FLOW", "DIFFICULTY ADJUSTMENT", "LATEST TRANSACTIONS", "FEE MARKET"} {
		if !strings.Contains(view, section) {
			t.Fatalf("expected a 160x56 overview to contain %q", section)
		}
	}
	if fees, transactions := strings.Index(view, "FEE MARKET"), strings.Index(view, "LATEST TRANSACTIONS"); fees < 0 || transactions < 0 || fees > transactions {
		t.Fatal("expected Fee Market above Latest Transactions")
	}
}

func TestMetricRowUsesFullWidth(t *testing.T) {
	metrics := []metric{{"ONE", "1", "1"}, {"TWO", "2", "2"}, {"THREE", "3", "3"}, {"FOUR", "4", "4"}}
	if got := lipgloss.Width(renderMetricCards(metrics, 117, 4, false)); got != 117 {
		t.Fatalf("metric row width = %d, want 117", got)
	}
}

func TestFeeMarketShowsEstimatedUSDCost(t *testing.T) {
	view := renderFeeMarket(mempool.Fees{Fastest: 12, HalfHour: 8, Hour: 5, Economy: 2, Minimum: 1}, 100_000, newLayout(120, 40), 80)
	for _, want := range []string{"sat/vB / USD for 140 vB", "12 · ~$1.68", "1 · ~$0.14"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected fee market to contain %q", want)
		}
	}
}

func TestRenderFragmentCachesUntilInvalidated(t *testing.T) {
	var fragment renderFragment
	renders := 0
	render := func() string {
		renders++
		return "panel"
	}

	if got := cached(&fragment, render); got != "panel" {
		t.Fatalf("unexpected cached content %q", got)
	}
	cached(&fragment, render)
	if renders != 1 {
		t.Fatalf("render count = %d, want 1", renders)
	}

	fragment.valid = false
	cached(&fragment, render)
	if renders != 2 {
		t.Fatalf("render count after invalidation = %d, want 2", renders)
	}
}

func TestActivityInvalidationOnlyTouchesDependentPanels(t *testing.T) {
	cache := overviewRenderCache{
		header:       renderFragment{valid: true},
		metrics:      renderFragment{valid: true},
		blocks:       renderFragment{valid: true},
		activity:     renderFragment{valid: true},
		difficulty:   renderFragment{valid: true},
		transactions: renderFragment{valid: true},
		fees:         renderFragment{valid: true},
	}

	cache.invalidateActivity()
	if cache.activity.valid {
		t.Fatal("activity-dependent panels should be invalidated")
	}
	if !cache.header.valid || !cache.metrics.valid || !cache.blocks.valid || !cache.difficulty.valid || !cache.transactions.valid || !cache.fees.valid {
		t.Fatal("activity update invalidated an unrelated panel")
	}
}

func TestMergeRecentTransactionsPrefersLiveArrivals(t *testing.T) {
	incoming := []mempool.Transaction{{TxID: "new"}, {TxID: "shared", Fee: 2}}
	existing := []mempool.Transaction{{TxID: "shared", Fee: 1}, {TxID: "old"}}
	merged := mergeRecentTransactions(incoming, existing, 3)
	if len(merged) != 3 || merged[0].TxID != "new" || merged[1].Fee != 2 || merged[2].TxID != "old" {
		t.Fatalf("unexpected merge result: %+v", merged)
	}
}

func TestTransactionsAnimateOneAtATime(t *testing.T) {
	m := newModel("")
	m.pendingTX = []mempool.Transaction{{TxID: "first"}, {TxID: "second"}}
	m.activateNextTransaction()
	if len(m.newTXIDs) != 1 || m.overview.Recent[0].TxID != "first" || len(m.pendingTX) != 1 {
		t.Fatalf("expected only the first transaction to activate: %+v", m)
	}
	m.txPulse = 0
	m.activateNextTransaction()
	if len(m.newTXIDs) != 1 || m.overview.Recent[0].TxID != "second" || len(m.pendingTX) != 0 {
		t.Fatalf("expected the second transaction to activate next: %+v", m)
	}
}

func TestNewChainTipStartsBlockCelebration(t *testing.T) {
	m := newModel("")
	m.overview.Blocks = []mempool.Block{{ID: "old-tip", Height: 900000}}

	updated, cmd := m.Update(overviewMsg{snapshot: mempool.Overview{
		Blocks:  []mempool.Block{{ID: "new-tip", Height: 900001}},
		Fetched: time.Now(),
	}})
	m = updated.(model)
	if m.newBlockID != "new-tip" || m.blockPulse == 0 {
		t.Fatalf("expected new block celebration, got id %q and pulse %d", m.newBlockID, m.blockPulse)
	}
	if cmd == nil {
		t.Fatal("expected sound and animation commands for a new chain tip")
	}

	card := renderBlockStack(&overviewRenderCache{}, nil, m.overview.Blocks, newLayout(120, 40), 34, 18, 0, m.blockPulse, m.newBlockID)
	if !strings.Contains(card, "NEW BLOCK") {
		t.Fatal("expected the newly confirmed block card to show its animation marker")
	}
}

func TestInitialChainTipDoesNotTriggerBlockCelebration(t *testing.T) {
	m := newModel("")
	updated, _ := m.Update(overviewMsg{snapshot: mempool.Overview{
		Blocks:  []mempool.Block{{ID: "initial-tip", Height: 900000}},
		Fetched: time.Now(),
	}})
	m = updated.(model)
	if m.blockPulse != 0 || m.newBlockID != "" {
		t.Fatal("initial data load should not announce an already-known block")
	}
}

func TestEnqueueTransactionsNeverExceedsLimit(t *testing.T) {
	queue := []mempool.Transaction{{TxID: "one"}, {TxID: "two"}, {TxID: "three"}}
	incoming := []mempool.Transaction{{TxID: "four"}, {TxID: "five"}}

	got := enqueueTransactions(queue, incoming, nil, 2)
	if len(got) != 2 || got[0].TxID != "one" || got[1].TxID != "two" {
		t.Fatalf("queue exceeded limit or changed order: %+v", got)
	}
}

func TestDashboardKeepsEnoughRecentTransactionsForTallPanels(t *testing.T) {
	existing := make([]mempool.Transaction, recentTransactionLimit)
	for i := range existing {
		existing[i].TxID = fmt.Sprintf("old-%d", i)
	}
	got := mergeRecentTransactions([]mempool.Transaction{{TxID: "new"}}, existing, recentTransactionLimit)
	if len(got) != recentTransactionLimit || got[0].TxID != "new" {
		t.Fatalf("recent transaction history = %d items starting with %q", len(got), got[0].TxID)
	}
}

func TestFillActivityGaps(t *testing.T) {
	values := []float64{-1, 100, -1, -1, 400, -1}
	fillActivityGaps(values)
	if values[2] != 200 || values[3] != 300 || values[5] != 400 {
		t.Fatalf("unexpected filled activity values: %v", values)
	}
}

// plain strips the styling escapes so assertions can match rendered text.
func plain(view string) string {
	return ansiEscape.ReplaceAllString(view, "")
}

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// sampleOverview is a full snapshot, so responsive tests exercise every panel
// rather than the early-return placeholder.
func sampleOverview() mempool.Overview {
	now := time.Now()
	blocks := make([]mempool.Block, 0, 8)
	pools := []string{"Foundry USA", "AntPool", "ViaBTC", "F2Pool", "Binance Pool", "MARA Pool", "Luxor", "SpiderPool"}
	for i, pool := range pools {
		b := mempool.Block{
			ID:        fmt.Sprintf("block-%d", i),
			Height:    int64(900123 - i),
			Timestamp: now.Add(-time.Duration(i*11) * time.Minute).Unix(),
			TxCount:   3100 + i*37,
			Size:      1_540_000,
		}
		b.Extras.Pool.Name = pool
		b.Extras.Reward = 315_000_000 + int64(i*100_000)
		b.Extras.TotalFees = 2_500_000 + int64(i*50_000)
		b.Extras.MedianFee = 3.2 + float64(i)/10
		b.Extras.MatchRate = 97.5
		blocks = append(blocks, b)
	}
	recent := make([]mempool.Transaction, 0, 10)
	for i := range 10 {
		recent = append(recent, mempool.Transaction{
			TxID:  fmt.Sprintf("%064x", 0xabcdef12345678+i),
			Fee:   int64(420 + i*133),
			VSize: 141,
			Value: int64(25_000_000 + i*3_100_000),
		})
	}
	snapshot := mempool.Overview{
		Fees:    mempool.Fees{Fastest: 12, HalfHour: 8, Hour: 5, Economy: 2, Minimum: 1},
		Mempool: mempool.Mempool{Count: 128_412, VSize: 84_000_000},
		Blocks:  blocks,
		ProjectedBlocks: []mempool.ProjectedBlock{
			{BlockVSize: 998_000, TxCount: 2513, MedianFee: 8.2},
			{BlockVSize: 997_000, TxCount: 2610, MedianFee: 6.1},
			{BlockVSize: 996_000, TxCount: 2702, MedianFee: 4.4},
		},
		Difficulty: mempool.DifficultyAdjustment{ProgressPercent: 62.4, DifficultyChange: 1.83, RemainingBlocks: 758},
		Recent:     recent,
		Prices:     mempool.Prices{USD: 104_233},
		Fetched:    now,
	}
	snapshot.Mining.Hashrate.CurrentHashrate = 920e18
	snapshot.Mining.Hashrate.CurrentDifficulty = 125.8e12
	snapshot.Mining.Hashrate.Hashrates = []mempool.HashratePoint{{AvgHashrate: 880e18}, {AvgHashrate: 920e18}, {AvgHashrate: 900e18}}
	snapshot.Mining.Hashrate.Difficulty = []mempool.DifficultyPoint{
		{Time: now.Add(-60 * 24 * time.Hour).Unix(), Height: 895000, Difficulty: 120e12, Adjustment: 1.025},
		{Time: now.Add(-30 * 24 * time.Hour).Unix(), Height: 897016, Difficulty: 123e12, Adjustment: 0.99},
	}
	snapshot.Mining.Pools.BlockCount = 100
	snapshot.Mining.Pools.Pools = []mempool.MiningPool{{Name: "Foundry USA", BlockCount: 31}, {Name: "AntPool", BlockCount: 18}}
	snapshot.Mining.Rewards = mempool.RewardStats{StartBlock: 900000, EndBlock: 900143, TotalReward: "45360000000", TotalFee: "360000000"}
	return snapshot
}

var terminalSizes = []struct{ width, height int }{
	{200, 60}, {160, 50}, {120, 40}, {110, 32}, {100, 30}, {96, 26},
	{90, 30}, {80, 24}, {72, 20}, {64, 22}, {60, 20}, {52, 18},
	{48, 16}, {40, 14}, {36, 12}, {34, 10}, {30, 8}, {24, 6}, {20, 5},
}

func TestOverviewFillsEveryTerminalExactly(t *testing.T) {
	snapshot := sampleOverview()
	activity := []activitySample{{at: time.Now(), value: 6800}}
	for _, size := range terminalSizes {
		view := renderOverview(snapshot, activity, false, nil, size.width, size.height, 0, 0, nil)
		if got := lipgloss.Width(view); got != size.width {
			t.Errorf("%dx%d: width = %d, want %d", size.width, size.height, got, size.width)
		}
		if got := lipgloss.Height(view); got != size.height {
			t.Errorf("%dx%d: height = %d, want %d", size.width, size.height, got, size.height)
		}
	}
}

func TestOverviewNeverClipsAPanelBorder(t *testing.T) {
	snapshot := sampleOverview()
	activity := []activitySample{{at: time.Now(), value: 6800}}
	for _, size := range terminalSizes {
		view := renderOverview(snapshot, activity, false, nil, size.width, size.height, 0, 0, nil)
		opened := strings.Count(view, "╭")
		closed := strings.Count(view, "╰")
		// The block stack scrolls, so in the two column layout its bottom card
		// may legitimately be cut by the viewport. Every other panel is whole.
		slack := 0
		if newLayout(size.width, size.height).twoColumn {
			slack = 1
		}
		if opened-closed > slack || closed > opened {
			t.Errorf("%dx%d: %d panels opened but %d closed", size.width, size.height, opened, closed)
		}
	}
}

func TestNarrowTerminalStacksTheBlocksAboveThePanels(t *testing.T) {
	if !newLayout(120, 40).twoColumn {
		t.Fatal("a 120 column terminal should place the block stack beside the panels")
	}
	if newLayout(80, 40).twoColumn {
		t.Fatal("an 80 column terminal should stack the block stack above the panels")
	}

	view := renderOverview(sampleOverview(), nil, false, nil, 80, 30, 0, 0, nil)
	blocks := strings.Index(view, "MEMPOOL BLOCKS")
	transactions := strings.Index(view, "LATEST TRANSACTIONS")
	if blocks < 0 || transactions < 0 || blocks > transactions {
		t.Fatalf("expected the block stack above the transactions, got %d and %d", blocks, transactions)
	}
	// Full-width bordered cards would leave the single column mostly empty.
	if strings.Contains(view, "median fee") {
		t.Fatal("a single column stack should list blocks as rows, not cards")
	}
}

func TestPanelsAreDroppedInPriorityOrderAndTheFlexibleOneAbsorbsTheRest(t *testing.T) {
	panels := []panel{
		{height: 6, min: 3, priority: 90, grow: true, render: func(int) string { return "blocks" }},
		{height: 8, min: 8, priority: 60, render: func(int) string { return "chart" }},
		{height: 5, min: 5, priority: 40, render: func(int) string { return "pulse" }},
	}

	if got := fitPanels(panels, 19); len(got) != 3 || got[0].height != 6 {
		t.Fatalf("everything fits, so nothing should change: %+v", got)
	}
	fitted := fitPanels(panels, 12)
	if len(fitted) != 2 {
		t.Fatalf("expected the lowest priority panel to be dropped, got %d panels", len(fitted))
	}
	if fitted[1].height != 8 {
		t.Fatalf("fixed panels keep their height, got %d", fitted[1].height)
	}
	if fitted[0].height != 4 {
		t.Fatalf("the flexible panel should absorb the remaining rows, got %d", fitted[0].height)
	}
	if panels[0].height != 6 {
		t.Fatal("fitPanels must not mutate the panels it was given")
	}
}

func TestStatColumnsAbbreviateBeforeTheyWrap(t *testing.T) {
	fees := mempool.Fees{Fastest: 12, HalfHour: 8, Hour: 5, Economy: 2, Minimum: 1}
	wide := renderFeeMarket(fees, 100_000, newLayout(160, 50), 120)
	if !strings.Contains(wide, "NEXT BLOCK") || !strings.Contains(wide, "12 · ~$1.68") {
		t.Fatalf("a wide panel should keep the full labels and dollar estimates:\n%s", wide)
	}
	narrow := renderFeeMarket(fees, 100_000, newLayout(100, 30), 62)
	if lipgloss.Height(narrow) != lipgloss.Height(wide) {
		t.Fatalf("the panel should abbreviate rather than wrap onto another row:\n%s", narrow)
	}
	if !strings.Contains(narrow, "NEXT") || strings.Contains(narrow, "NEXT BLOCK") {
		t.Fatalf("expected abbreviated fee labels:\n%s", narrow)
	}
}

func sampleMetrics() []metric {
	return append([]metric{
		{"BLOCK HEIGHT", "HEIGHT", "900,123"},
		{"MEMPOOL TXS", "MEMPOOL", "128,412"},
		{"MEMPOOL SIZE", "SIZE", "84.00 MvB"},
		{"NEXT BLOCK FEE", "NEXT FEE", "12 sat/vB"},
		{"BTC PRICE", "BTC/USD", "$104,233.00"},
	}, networkMetrics(sampleOverview().Blocks)...)
}

func TestMetricStripCarriesTheNetworkFiguresToo(t *testing.T) {
	view := plain(renderOverview(sampleOverview(), nil, false, nil, 120, 40, 0, 0, nil))
	header, _, _ := strings.Cut(view, "MEMPOOL BLOCKS")
	for _, want := range []string{"BLOCK HEIGHT", "MEMPOOL TXS", "NEXT BLOCK FEE", "BTC PRICE", "LAST BLOCK", "AVG INTERVAL", "TX / 8 BLOCKS"} {
		if !strings.Contains(header, want) {
			t.Fatalf("expected %q in the strip above the panels:\n%s", want, header)
		}
	}
	if strings.Contains(view, "NETWORK PULSE") {
		t.Fatal("the network pulse panel should be gone now its figures are in the strip")
	}
}

func TestMetricRowUsesFourColumnsAndTwoRows(t *testing.T) {
	metrics := sampleMetrics()
	row := renderMetricRow(metrics, newLayout(120, 40))
	if got := lipgloss.Height(row); got != 8 {
		t.Fatalf("eight metrics should form two rows of cards, height = %d", got)
	}
	if got := strings.Count(row, "╭"); got != 8 {
		t.Fatalf("every metric should have its own bordered card, got %d:\n%s", got, row)
	}
	lines := strings.Split(plain(row), "\n")
	if got := strings.Count(lines[0], "╭"); got != 4 {
		t.Fatalf("the first row should have four columns, got %d:\n%s", got, row)
	}
}

func TestMetricGridKeepsCardsOnNarrowTerminals(t *testing.T) {
	l := newLayout(36, 12)
	row := renderMetricRow(sampleMetrics(), l)
	if !strings.Contains(row, "╭") || !strings.Contains(plain(row), "HEIGHT") {
		t.Fatalf("the responsive metric grid should retain bordered cards:\n%s", row)
	}
}

func TestTerminalBelowTheFloorExplainsItself(t *testing.T) {
	view := renderOverview(sampleOverview(), nil, false, nil, 24, 6, 0, 0, nil)
	if !strings.Contains(view, "ompool needs") {
		t.Fatalf("expected a size hint in a terminal too small to draw:\n%s", view)
	}
}

func TestPickerDropsChromeItCannotFit(t *testing.T) {
	if !strings.Contains(renderPicker(0, 80, 24), "____  __  ___") {
		t.Fatal("a roomy terminal should keep the wordmark")
	}
	small := renderPicker(0, 30, 10)
	if strings.Contains(small, "____  __  ___") {
		t.Fatal("the wordmark should give way in a small terminal")
	}
	for _, want := range []string{"OMPOOL", "> Overview"} {
		if !strings.Contains(small, want) {
			t.Fatalf("expected the small picker to keep %q:\n%s", want, small)
		}
	}
	if lipgloss.Width(small) > 30 {
		t.Fatalf("the picker overflowed its terminal:\n%s", small)
	}
}

func TestBlockScrollStopsAtTheEndOfTheStack(t *testing.T) {
	m := model{
		renderCache: &overviewRenderCache{},
		screen:      moduleScreen,
		active:      modules[0],
		overview:    sampleOverview(),
		width:       80,
		height:      24,
	}
	m.View() // the renderer records the bound the panel can actually scroll to.
	if m.maxBlockScroll() == 0 {
		t.Fatal("the sample stack is taller than the panel, so it should scroll")
	}

	for range 20 {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(model)
		m.View()
	}
	if m.blockScroll != m.maxBlockScroll() {
		t.Fatalf("scroll ran past the stack: %d, bound %d", m.blockScroll, m.maxBlockScroll())
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(model)
	if m.blockScroll >= m.maxBlockScroll() {
		t.Fatalf("one press should step back into the stack, got %d", m.blockScroll)
	}
}

func TestFlowChartLabelsEveryRowWithARoundNumber(t *testing.T) {
	for _, height := range []int{24, 30, 40, 50, 60} {
		l := newLayout(120, height)
		chart := plain(renderActivityChart([]activitySample{{at: time.Now(), value: 6800}}, 76, l.chartHeight))
		lines := strings.Split(chart, "\n")[:l.chartHeight]
		if len(lines) != l.chartHeight {
			t.Fatalf("%d rows: chart drew %d lines", height, len(lines))
		}
		step := activityCeiling / float64(l.chartHeight-1)
		if step != math.Trunc(step) {
			t.Errorf("%d rows: axis step %.2f is not a round number", height, step)
		}
		for row, line := range lines {
			want := fmt.Sprintf("%.0f │", axisValueAt(row, l.chartHeight))
			if !strings.HasPrefix(strings.TrimLeft(line, " "), want) {
				t.Errorf("%d rows: row %d = %q, want it labelled %q", height, row, line, want)
			}
		}
	}
}

func TestATallerTerminalGetsATallerFlowChart(t *testing.T) {
	previous := 0
	for _, height := range []int{24, 30, 40, 50, 60} {
		got := newLayout(120, height).chartHeight
		if got < previous {
			t.Fatalf("%d rows gave a shorter chart (%d) than a shorter terminal (%d)", height, got, previous)
		}
		previous = got
	}
	if newLayout(120, 60).chartHeight <= newLayout(120, 24).chartHeight {
		t.Fatal("a 60 row terminal should plot more detail than a 24 row one")
	}
}
