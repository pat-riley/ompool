package main

import (
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
	if !strings.Contains(renderPicker(m.cursor), "> Overview") {
		t.Fatalf("expected picker to mark Overview as selected")
	}
	if !strings.Contains(renderPicker(m.cursor), "___  _ __ ___") {
		t.Fatalf("expected picker to include the ompool wordmark")
	}
}

func TestPickerNavigationAndSelection(t *testing.T) {
	m := newModel("")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)

	if m.screen != moduleScreen || m.active.command != "blockchain" {
		t.Fatalf("expected blockchain module, got %q", m.active.command)
	}
}

func TestDirectModule(t *testing.T) {
	m := newModel("fees")
	if m.screen != moduleScreen || m.active.command != "fees" {
		t.Fatalf("expected direct fees module")
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
	for _, section := range []string{"BITCOIN MAINNET / OVERVIEW", "MEMPOOL BLOCKS", "CONFIRMED", "INCOMING TRANSACTION FLOW", "7000 │", "LATEST TRANSACTIONS", "$25,000.00", "FEE MARKET", "30 MIN", "NETWORK PULSE", "AVG INTERVAL"} {
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

func TestMetricRowUsesFullWidth(t *testing.T) {
	metrics := []struct{ label, value string }{
		{"ONE", "1"}, {"TWO", "2"}, {"THREE", "3"}, {"FOUR", "4"},
	}
	if got := lipgloss.Width(renderMetricRow(metrics, 117)); got != 117 {
		t.Fatalf("metric row width = %d, want 117", got)
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

func TestEnqueueTransactionsNeverExceedsLimit(t *testing.T) {
	queue := []mempool.Transaction{{TxID: "one"}, {TxID: "two"}, {TxID: "three"}}
	incoming := []mempool.Transaction{{TxID: "four"}, {TxID: "five"}}

	got := enqueueTransactions(queue, incoming, nil, 2)
	if len(got) != 2 || got[0].TxID != "one" || got[1].TxID != "two" {
		t.Fatalf("queue exceeded limit or changed order: %+v", got)
	}
}

func TestFillActivityGaps(t *testing.T) {
	values := []float64{-1, 100, -1, -1, 400, -1}
	fillActivityGaps(values)
	if values[2] != 200 || values[3] != 300 || values[5] != 400 {
		t.Fatalf("unexpected filled activity values: %v", values)
	}
}
