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
		Fetched: time.Now(),
	}

	view := renderOverview(snapshot, []activitySample{{at: time.Now(), value: 120}, {at: time.Now(), value: 450}, {at: time.Now(), value: 300}}, false, nil, 120, 40, 0)
	for _, section := range []string{"BITCOIN MAINNET / OVERVIEW", "MEMPOOL BLOCKS", "CONFIRMED", "INCOMING TRANSACTION FLOW"} {
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
}

func TestMetricRowUsesFullWidth(t *testing.T) {
	metrics := []struct{ label, value string }{
		{"ONE", "1"}, {"TWO", "2"}, {"THREE", "3"}, {"FOUR", "4"},
	}
	if got := lipgloss.Width(renderMetricRow(metrics, 117)); got != 117 {
		t.Fatalf("metric row width = %d, want 117", got)
	}
}
