package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ompool/internal/mempool"
)

type module struct {
	command     string
	title       string
	description string
}

var modules = []module{
	{command: "overview", title: "Overview", description: "The complete Bitcoin network dashboard"},
	{command: "blockchain", title: "Blockchain", description: "An ASCII visualization of the chain"},
	{command: "blocks", title: "Recent Blocks", description: "A live feed of newly mined blocks"},
	{command: "mempool", title: "Mempool", description: "Transaction backlog, weight, and activity"},
	{command: "fees", title: "Fees", description: "Current fee estimates and recent trends"},
}

const wordmark = `
  ___  _ __ ___  _ __   ___   ___ | |
 / _ \| '_ ' _ \| '_ \ / _ \ / _ \| |
| (_) | | | | | | |_) | (_) | (_) | |
 \___/|_| |_| |_| .__/ \___/ \___/|_|
                |_|
`

type screen int

const (
	pickerScreen screen = iota
	moduleScreen
)

type model struct {
	cursor      int
	screen      screen
	active      module
	client      *mempool.Client
	overview    mempool.Overview
	live        <-chan mempool.LiveStats
	liveStop    context.CancelFunc
	activity    []activitySample
	blockScroll int
	txPulse     int
	newTXIDs    map[string]struct{}
	pendingTX   []mempool.Transaction
	loading     bool
	err         error
	width       int
	height      int
}

type activitySample struct {
	at    time.Time
	value float64
}

func newModel(command string) model {
	m := model{
		active: modules[0],
		client: mempool.NewClient(os.Getenv("OMPOOL_API_URL")),
	}
	if command == "" {
		return m
	}

	for _, candidate := range modules {
		if candidate.command == command {
			m.screen = moduleScreen
			m.active = candidate
			m.loading = candidate.command == "overview"
			if candidate.command == "overview" {
				ctx, cancel := context.WithCancel(context.Background())
				m.live = m.client.StreamStats(ctx)
				m.liveStop = cancel
			}
			return m
		}
	}

	return m
}

type overviewMsg struct {
	snapshot mempool.Overview
	err      error
}

type refreshMsg time.Time
type liveStatsMsg mempool.LiveStats
type liveStreamClosedMsg struct{}
type txPulseMsg struct{}

func (m model) Init() tea.Cmd {
	if m.screen == moduleScreen && m.active.command == "overview" {
		return tea.Batch(fetchOverview(m.client), waitForLiveStats(m.live))
	}
	return nil
}

func waitForLiveStats(stream <-chan mempool.LiveStats) tea.Cmd {
	return func() tea.Msg {
		stats, ok := <-stream
		if !ok {
			return liveStreamClosedMsg{}
		}
		return liveStatsMsg(stats)
	}
}

func fetchOverview(client *mempool.Client) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := client.FetchOverview(context.Background())
		return overviewMsg{snapshot: snapshot, err: err}
	}
}

func scheduleRefresh() tea.Cmd {
	return tea.Tick(15*time.Second, func(t time.Time) tea.Msg { return refreshMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case overviewMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.overview = msg.snapshot
		}
		return m, scheduleRefresh()
	case liveStatsMsg:
		now := time.Now()
		if msg.HasFlow {
			m.activity = append(m.activity, activitySample{at: now, value: msg.VBytesPerSecond})
		}
		if len(msg.Transactions) > 0 {
			m.pendingTX = enqueueTransactions(m.pendingTX, msg.Transactions, m.overview.Recent, 40)
			if m.txPulse == 0 {
				m.activateNextTransaction()
			}
		}
		cutoff := now.Add(-2 * time.Minute)
		first := 0
		for first < len(m.activity) && m.activity[first].at.Before(cutoff) {
			first++
		}
		if first > 0 {
			m.activity = m.activity[first:]
		}
		if m.txPulse > 0 {
			return m, tea.Batch(waitForLiveStats(m.live), scheduleTXPulse())
		}
		return m, waitForLiveStats(m.live)
	case txPulseMsg:
		if m.txPulse > 0 {
			m.txPulse--
		}
		if m.txPulse == 0 {
			m.newTXIDs = nil
			m.activateNextTransaction()
		}
		if m.txPulse > 0 {
			return m, scheduleTXPulse()
		}
		return m, nil
	case liveStreamClosedMsg:
		return m, nil
	case refreshMsg:
		if m.screen == moduleScreen && m.active.command == "overview" {
			m.loading = true
			return m, fetchOverview(m.client)
		}
		return m, nil
	case tea.MouseWheelMsg:
		if m.screen == moduleScreen && m.active.command == "overview" && msg.X < max(36, m.width/3+4) {
			switch msg.Button {
			case tea.MouseWheelDown:
				m.blockScroll = min(m.maxBlockScroll(), m.blockScroll+3)
			case tea.MouseWheelUp:
				m.blockScroll = max(0, m.blockScroll-3)
			}
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "ctrl+c", "q":
		if m.liveStop != nil {
			m.liveStop()
		}
		return m, tea.Quit
	case "esc":
		if m.screen == moduleScreen {
			if m.liveStop != nil {
				m.liveStop()
				m.liveStop = nil
			}
			m.screen = pickerScreen
			m.err = nil
			return m, nil
		}
		return m, tea.Quit
	}

	if m.screen != pickerScreen {
		if m.active.command == "overview" {
			switch key.String() {
			case "down", "j":
				m.blockScroll = min(m.maxBlockScroll(), m.blockScroll+3)
			case "up", "k":
				m.blockScroll = max(0, m.blockScroll-3)
			case "home", "g":
				m.blockScroll = 0
			}
		}
		return m, nil
	}

	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(modules)-1 {
			m.cursor++
		}
	case "enter":
		m.active = modules[m.cursor]
		m.screen = moduleScreen
		if m.active.command == "overview" {
			m.loading = true
			ctx, cancel := context.WithCancel(context.Background())
			m.live = m.client.StreamStats(ctx)
			m.liveStop = cancel
			return m, tea.Batch(fetchOverview(m.client), waitForLiveStats(m.live))
		}
	}

	return m, nil
}

func mergeRecentTransactions(incoming, existing []mempool.Transaction, limit int) []mempool.Transaction {
	merged := make([]mempool.Transaction, 0, min(limit, len(incoming)+len(existing)))
	seen := make(map[string]struct{}, len(incoming)+len(existing))
	for _, group := range [][]mempool.Transaction{incoming, existing} {
		for _, tx := range group {
			if tx.TxID == "" {
				continue
			}
			if _, exists := seen[tx.TxID]; exists {
				continue
			}
			seen[tx.TxID] = struct{}{}
			merged = append(merged, tx)
			if len(merged) == limit {
				return merged
			}
		}
	}
	return merged
}

func enqueueTransactions(queue, incoming, existing []mempool.Transaction, limit int) []mempool.Transaction {
	if limit <= 0 {
		return nil
	}
	if len(queue) > limit {
		queue = queue[:limit]
	}
	seen := make(map[string]struct{}, len(queue)+len(existing))
	for _, tx := range queue {
		seen[tx.TxID] = struct{}{}
	}
	for _, tx := range existing {
		seen[tx.TxID] = struct{}{}
	}
	for _, tx := range incoming {
		if len(queue) >= limit {
			break
		}
		if tx.TxID == "" {
			continue
		}
		if _, exists := seen[tx.TxID]; exists {
			continue
		}
		seen[tx.TxID] = struct{}{}
		queue = append(queue, tx)
	}
	return queue
}

func (m *model) activateNextTransaction() {
	if len(m.pendingTX) == 0 {
		return
	}
	tx := m.pendingTX[0]
	m.pendingTX = m.pendingTX[1:]
	m.overview.Recent = mergeRecentTransactions([]mempool.Transaction{tx}, m.overview.Recent, 10)
	m.newTXIDs = map[string]struct{}{tx.TxID: {}}
	m.txPulse = 5
}

func scheduleTXPulse() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return txPulseMsg{} })
}

func (m model) maxBlockScroll() int {
	panelHeight := max(18, m.height-15)
	contentLines := (len(m.overview.ProjectedBlocks)+len(m.overview.Blocks))*7 + 1
	return max(0, contentLines-max(1, panelHeight-5))
}

func (m model) View() tea.View {
	var view tea.View
	if m.screen == moduleScreen {
		if m.active.command == "overview" {
			view = tea.NewView(renderOverview(m.overview, m.activity, m.loading, m.err, m.width, m.height, m.blockScroll, m.txPulse, m.newTXIDs))
		} else {
			view = tea.NewView(renderModule(m.active))
		}
	} else {
		view = tea.NewView(renderPicker(m.cursor))
	}
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func renderPicker(cursor int) string {
	var out strings.Builder
	out.WriteString(wordmark)
	out.WriteString("             Bitcoin from the terminal\n\n")

	for i, item := range modules {
		marker := "  "
		if i == cursor {
			marker = "> "
		}
		fmt.Fprintf(&out, "%s%-16s %s\n", marker, item.title, item.description)
	}

	out.WriteString("\n↑/↓ navigate  enter open  q quit\n")
	return out.String()
}

func renderModule(item module) string {
	return fmt.Sprintf("ompool / %s\n\n%s\n\nModule data is coming next.\n\nEsc returns to the module picker. q quits.\n", item.title, item.description)
}

var (
	orange     = lipgloss.Color("#f7931a")
	dim        = lipgloss.Color("#777777")
	panel      = lipgloss.Color("#343434")
	bright     = lipgloss.Color("#f5f5f5")
	headerText = lipgloss.NewStyle().Bold(true).Foreground(bright)
	labelText  = lipgloss.NewStyle().Foreground(dim)
	valueText  = lipgloss.NewStyle().Bold(true).Foreground(orange)
	panelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(panel).Padding(0, 1)
)

func renderOverview(snapshot mempool.Overview, activity []activitySample, loading bool, err error, width, height, blockScroll, txPulse int, newTXIDs map[string]struct{}) string {
	if width < 72 {
		width = 72
	}
	if height < 24 {
		height = 24
	}
	contentWidth := width - 4

	header := renderStatusHeader(snapshot, loading, contentWidth)
	if snapshot.Fetched.IsZero() {
		message := "Connecting to mempool.space…"
		if err != nil {
			message = "Could not load network data\n" + err.Error()
		}
		body := lipgloss.NewStyle().Foreground(dim).PaddingTop(3).Render(message)
		footer := labelText.Width(contentWidth).Render("Esc modules  ·  q quit  ·  data: mempool.space")
		return renderWithAnchoredFooter(header+"\n"+body, footer, width, height)
	}

	block := snapshot.Blocks[0]
	metrics := []struct{ label, value string }{
		{"BLOCK HEIGHT", formatInt(block.Height)},
		{"MEMPOOL TXS", formatInt(int64(snapshot.Mempool.Count))},
		{"MEMPOOL SIZE", formatBytes(snapshot.Mempool.VSize, "vB")},
		{"NEXT BLOCK FEE", fmt.Sprintf("%d sat/vB", snapshot.Fees.Fastest)},
	}

	metricRow := renderMetricRow(metrics, contentWidth)

	mainGrid := renderMainGrid(snapshot.ProjectedBlocks, snapshot.Blocks, activity, snapshot.Difficulty, snapshot.Recent, snapshot.Prices, snapshot.Fees, contentWidth, max(18, height-15), blockScroll, txPulse, newTXIDs)

	footer := labelText.Render("Esc modules  ·  q quit  ·  data: mempool.space")

	dashboard := strings.Join([]string{header, metricRow, mainGrid}, "\n\n")
	return renderWithAnchoredFooter(dashboard, labelText.Width(contentWidth).Render(footer), width, height)
}

func renderWithAnchoredFooter(body, footer string, width, height int) string {
	// Top padding is retained, but the bottom padding is deliberately zero so
	// the status line occupies the terminal's final row.
	innerHeight := max(1, height-1)
	footerHeight := max(1, lipgloss.Height(footer))
	bodyHeight := max(1, innerHeight-footerHeight)
	body = lipgloss.NewStyle().MaxHeight(bodyHeight).Render(body)
	spacer := max(0, bodyHeight-lipgloss.Height(body))
	content := body + strings.Repeat("\n", spacer) + "\n" + footer
	return lipgloss.NewStyle().Width(width).Height(height).Padding(1, 2, 0, 2).Render(content)
}

func renderMetricRow(metrics []struct{ label, value string }, width int) string {
	const gap = 1
	available := width - gap*(len(metrics)-1)
	baseWidth := available / len(metrics)
	remainder := available % len(metrics)
	cards := make([]string, 0, len(metrics)*2-1)
	for i, metric := range metrics {
		targetWidth := baseWidth
		if i < remainder {
			targetWidth++
		}
		card := labelText.Render(metric.label) + "\n" + valueText.Render(metric.value)
		cards = append(cards, panelStyle.Width(max(1, targetWidth)).Render(card))
		if i < len(metrics)-1 {
			cards = append(cards, " ")
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

func renderStatusHeader(snapshot mempool.Overview, loading bool, width int) string {
	left := headerText.Render("OMPOOL") + "  " + labelText.Render("BITCOIN MAINNET / OVERVIEW")
	status := valueText.Render("● LIVE")
	if loading {
		status = labelText.Render("◌ REFRESHING")
	}
	if !snapshot.Fetched.IsZero() {
		status += labelText.Render("  " + snapshot.Fetched.UTC().Format("15:04:05 UTC"))
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(status)
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + status
}

func renderBlockStack(projected []mempool.ProjectedBlock, confirmed []mempool.Block, width, height, scroll int) string {
	title := headerText.Render("MEMPOOL BLOCKS") + "  " + labelText.Render("↑/↓ scroll")
	cardWidth := width - 4
	cardHeight := 5
	items := make([]string, 0, len(projected)+len(confirmed)+1)
	for _, b := range projected {
		body := valueText.Render(fmt.Sprintf("%.1f sat/vB", b.MedianFee)) + "\n" +
			labelText.Render("median fee") + "\n\n" +
			headerText.Render(formatBytes(int64(b.BlockVSize), "vB")) + "  " +
			labelText.Render(formatInt(int64(b.TxCount))+" tx")
		items = append(items, panelStyle.Width(cardWidth).Height(cardHeight).BorderForeground(dim).Render(body))
	}

	dividerWidth := max(8, cardWidth-19)
	divider := labelText.Render(strings.Repeat("─", dividerWidth/2) + " CONFIRMED " + strings.Repeat("─", dividerWidth-dividerWidth/2))
	items = append(items, divider)
	for _, b := range confirmed {
		pool := b.Extras.Pool.Name
		if pool == "" {
			pool = "Unknown pool"
		}
		body := valueText.Render("#"+formatInt(b.Height)) + "\n" +
			headerText.Render(pool) + "\n" +
			labelText.Render(age(b.Timestamp)) + "\n" +
			headerText.Render(formatInt(int64(b.TxCount))+" tx") + "  " +
			labelText.Render(formatBytes(b.Size, "B"))
		items = append(items, panelStyle.Width(cardWidth).Height(cardHeight).BorderForeground(orange).Render(body))
	}

	lines := strings.Split(lipgloss.JoinVertical(lipgloss.Left, items...), "\n")
	visibleLines := max(1, height-5)
	maxScroll := max(0, len(lines)-visibleLines)
	scroll = min(max(0, scroll), maxScroll)
	end := min(len(lines), scroll+visibleLines)
	viewport := strings.Join(lines[scroll:end], "\n")
	content := title + "\n\n" + viewport
	return panelStyle.Width(width).Height(height - 2).Render(content)
}

func renderMainGrid(projected []mempool.ProjectedBlock, confirmed []mempool.Block, activity []activitySample, difficulty mempool.DifficultyAdjustment, recent []mempool.Transaction, prices mempool.Prices, fees mempool.Fees, width, height, scroll, txPulse int, newTXIDs map[string]struct{}) string {
	gap := 2
	leftWidth := min(34, max(28, width/3))
	rightWidth := width - gap - leftWidth
	left := renderBlockStack(projected, confirmed, leftWidth, height, scroll)
	right := renderActivityAndDifficulty(activity, difficulty, recent, prices, fees, confirmed, rightWidth, height, txPulse, newTXIDs)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}

func renderActivityAndDifficulty(activity []activitySample, difficulty mempool.DifficultyAdjustment, recent []mempool.Transaction, prices mempool.Prices, fees mempool.Fees, blocks []mempool.Block, width, height, txPulse int, newTXIDs map[string]struct{}) string {
	barWidth := max(10, width-6)
	filled := int(math.Round(math.Max(0, math.Min(100, difficulty.ProgressPercent)) / 100 * float64(barWidth)))
	bar := valueText.Render(strings.Repeat("█", filled)) + labelText.Render(strings.Repeat("░", barWidth-filled))
	change := fmt.Sprintf("%+.2f%%", difficulty.DifficultyChange)
	difficultyBody := headerText.Render("DIFFICULTY ADJUSTMENT") + "\n" + bar + "\n" +
		valueText.Render(fmt.Sprintf("%.1f%%", difficulty.ProgressPercent)) + labelText.Render(" through epoch") + "\n" +
		labelText.Render(fmt.Sprintf("estimate %s  ·  %s blocks left", change, formatInt(int64(difficulty.RemainingBlocks))))
	difficultyPanel := panelStyle.Width(width).Render(difficultyBody)
	transactionsPanel := renderRecentTransactions(recent, prices.USD, width, txPulse, newTXIDs)
	feePanel := renderFeeMarket(fees, width)
	networkPanel := renderNetworkPulse(blocks, activity, width)

	chartWidth := max(12, width-4)
	chart := renderActivityChart(activity, chartWidth, 5)
	latest := 0.0
	if len(activity) > 0 {
		latest = activity[len(activity)-1].value
	}
	activityBody := headerText.Render("INCOMING TRANSACTION FLOW") + "  " + valueText.Render(fmt.Sprintf("%.0f vB/s", latest)) + "  " + labelText.Render("2 min / 15 sec") + "\n\n" + chart
	activityPanel := panelStyle.Width(width).Render(activityBody)

	return lipgloss.JoinVertical(lipgloss.Left, activityPanel, difficultyPanel, transactionsPanel, feePanel, networkPanel)
}

func renderFeeMarket(fees mempool.Fees, width int) string {
	metrics := []struct{ label, value string }{
		{"NEXT BLOCK", fmt.Sprintf("%d", fees.Fastest)},
		{"30 MIN", fmt.Sprintf("%d", fees.HalfHour)},
		{"1 HOUR", fmt.Sprintf("%d", fees.Hour)},
		{"ECONOMY", fmt.Sprintf("%d", fees.Economy)},
		{"MINIMUM", fmt.Sprintf("%.1f", fees.Minimum)},
	}
	innerWidth := width - 4
	columnWidth := innerWidth / len(metrics)
	var labels, values strings.Builder
	for i, metric := range metrics {
		cellWidth := columnWidth
		if i == len(metrics)-1 {
			cellWidth = innerWidth - columnWidth*(len(metrics)-1)
		}
		labels.WriteString(fmt.Sprintf("%-*s", cellWidth, metric.label))
		values.WriteString(fmt.Sprintf("%-*s", cellWidth, metric.value+" sat/vB"))
	}
	body := headerText.Render("FEE MARKET") + "\n" + labelText.Render(labels.String()) + "\n" + valueText.Render(values.String())
	return panelStyle.Width(width).Render(body)
}

func renderNetworkPulse(blocks []mempool.Block, activity []activitySample, width int) string {
	flow := 0.0
	if len(activity) > 0 {
		flow = activity[len(activity)-1].value
	}

	averageInterval := time.Duration(0)
	if len(blocks) > 1 {
		span := blocks[0].Timestamp - blocks[len(blocks)-1].Timestamp
		if span > 0 {
			averageInterval = time.Duration(span/int64(len(blocks)-1)) * time.Second
		}
	}
	confirmedTX := int64(0)
	for _, block := range blocks {
		confirmedTX += int64(block.TxCount)
	}
	latestAge := "waiting"
	if len(blocks) > 0 {
		latestAge = age(blocks[0].Timestamp)
	}
	interval := "waiting"
	if averageInterval > 0 {
		interval = fmt.Sprintf("%dm %02ds", int(averageInterval.Minutes()), int(averageInterval.Seconds())%60)
	}

	metrics := []struct{ label, value string }{
		{"LIVE FLOW", fmt.Sprintf("%.0f vB/s", flow)},
		{"LAST BLOCK", latestAge},
		{"AVG INTERVAL", interval},
		{fmt.Sprintf("TX / %d BLOCKS", len(blocks)), formatInt(confirmedTX)},
	}
	innerWidth := width - 4
	columnWidth := innerWidth / len(metrics)
	var labels, values strings.Builder
	for i, metric := range metrics {
		cellWidth := columnWidth
		if i == len(metrics)-1 {
			cellWidth = innerWidth - columnWidth*(len(metrics)-1)
		}
		labels.WriteString(fmt.Sprintf("%-*s", cellWidth, metric.label))
		values.WriteString(fmt.Sprintf("%-*s", cellWidth, metric.value))
	}
	body := headerText.Render("NETWORK PULSE") + "\n" + labelText.Render(labels.String()) + "\n" + valueText.Render(values.String())
	return panelStyle.Width(width).Render(body)
}

func renderRecentTransactions(transactions []mempool.Transaction, usdPrice float64, width, txPulse int, newTXIDs map[string]struct{}) string {
	var out strings.Builder
	out.WriteString(headerText.Render("LATEST TRANSACTIONS"))
	out.WriteString("  ")
	out.WriteString(labelText.Render("live mempool arrivals"))
	out.WriteString("\n")

	visible := min(10, len(transactions))
	if visible == 0 {
		out.WriteString("\n")
		out.WriteString(labelText.Render("Waiting for transactions…"))
		return panelStyle.Width(width).Render(out.String())
	}

	if width >= 64 {
		innerWidth := width - 4
		available := innerWidth - 3
		txWidth := available * 30 / 100
		usdWidth := available * 20 / 100
		btcWidth := available * 24 / 100
		feeWidth := available - txWidth - usdWidth - btcWidth
		header := fmt.Sprintf("%-*s %*s %*s %*s", txWidth, "TXID", usdWidth, "USD", btcWidth, "BTC", feeWidth, "FEE")
		out.WriteString("\n" + labelText.Render(header))
		for _, tx := range transactions[:visible] {
			btc := float64(tx.Value) / 100_000_000
			feeRate := 0.0
			if tx.VSize > 0 {
				feeRate = float64(tx.Fee) / float64(tx.VSize)
			}
			row := fmt.Sprintf("%-*s %*s %*s %*s", txWidth, shortenTXIDTo(tx.TxID, txWidth), usdWidth, formatUSD(btc*usdPrice), btcWidth, fmt.Sprintf("%.8f", btc), feeWidth, fmt.Sprintf("%d (%.1f/vB)", tx.Fee, feeRate))
			out.WriteString("\n" + styleTransactionRow(row, tx.TxID, txPulse, newTXIDs, innerWidth))
		}
	} else {
		for _, tx := range transactions[:visible] {
			btc := float64(tx.Value) / 100_000_000
			fmt.Fprintf(&out, "\n%s  %s  %.8f BTC\n%s", valueText.Render(shortenTXID(tx.TxID)), formatUSD(btc*usdPrice), btc, labelText.Render(fmt.Sprintf("fee %d sats", tx.Fee)))
		}
	}
	return panelStyle.Width(width).Render(out.String())
}

func styleTransactionRow(row, txid string, pulse int, newTXIDs map[string]struct{}, width int) string {
	if pulse <= 0 {
		return row
	}
	if _, isNew := newTXIDs[txid]; !isNew {
		return row
	}
	colors := []string{"#8a6b45", "#a87331", "#c77d25", "#e58a1d", "#f7931a"}
	index := min(pulse-1, len(colors)-1)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colors[index])).Width(width).Render(row)
}

func shortenTXID(txid string) string {
	return shortenTXIDTo(txid, 13)
}

func shortenTXIDTo(txid string, width int) string {
	if len(txid) <= width {
		return txid
	}
	if width < 5 {
		return txid[:width]
	}
	left := (width - 1) / 2
	right := width - left - 1
	return txid[:left] + "…" + txid[len(txid)-right:]
}

func formatUSD(value float64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("$%.2fM", value/1_000_000)
	}
	centsTotal := int64(math.Round(value * 100))
	return fmt.Sprintf("$%s.%02d", formatInt(centsTotal/100), centsTotal%100)
}

func renderActivityChart(samples []activitySample, width, height int) string {
	const maxValue = 7000.0
	plotColumns := max(20, width-7)
	end := time.Now().UTC()
	start := end.Add(-2 * time.Minute)
	values := make([]float64, plotColumns)
	for i := range values {
		values[i] = -1
	}
	for _, sample := range samples {
		if sample.at.Before(start) {
			continue
		}
		x := int(sample.at.Sub(start).Seconds() / end.Sub(start).Seconds() * float64(plotColumns-1))
		x = min(max(0, x), plotColumns-1)
		if sample.value > values[x] {
			values[x] = sample.value
		}
	}
	fillActivityGaps(values)

	rows := make([]string, height)
	brailleFill := []string{" ", "⡀", "⡄", "⡆", "⡇"}
	logicalHeight := height * 4
	for row := range height {
		axisValue := maxValue * float64(height-1-row) / float64(max(1, height-1))
		var line strings.Builder
		if row == 0 || row == height-1 || row == height/2 {
			fmt.Fprintf(&line, "%4.0f │", axisValue)
		} else {
			line.WriteString("     │")
		}
		for x := range plotColumns {
			value := values[x]
			barDots := int(math.Ceil(math.Max(0, value) / maxValue * float64(logicalHeight)))
			dotsBelow := (height - 1 - row) * 4
			cellDots := min(4, max(0, barDots-dotsBelow))
			if value >= 0 && cellDots > 0 {
				level := maxValue * float64(dotsBelow+cellDots) / float64(logicalHeight)
				line.WriteString(activityColor(level).Render(brailleFill[cellDots]))
			} else {
				line.WriteString(" ")
			}
		}
		rows[row] = line.String()
	}
	rows = append(rows, "     └"+strings.Repeat("─", plotColumns))
	rows = append(rows, renderTimeAxis(start, end, plotColumns))
	return strings.Join(rows, "\n")
}

func fillActivityGaps(values []float64) {
	previous := -1
	for x, value := range values {
		if value < 0 {
			continue
		}
		if previous >= 0 && x-previous > 1 {
			left, right := values[previous], value
			for gap := previous + 1; gap < x; gap++ {
				ratio := float64(gap-previous) / float64(x-previous)
				values[gap] = left + (right-left)*ratio
			}
		}
		previous = x
	}
	if previous >= 0 {
		for x := previous + 1; x < len(values); x++ {
			values[x] = values[previous]
		}
	}
}

func activityColor(value float64) lipgloss.Style {
	color := lipgloss.Color("#38d66b")
	if value >= 4000 {
		color = lipgloss.Color("#ff3b30")
	} else if value >= 2500 {
		color = lipgloss.Color("#f7931a")
	} else if value >= 1000 {
		color = lipgloss.Color("#e6c84f")
	}
	return lipgloss.NewStyle().Bold(true).Foreground(color)
}

func renderTimeAxis(start, end time.Time, width int) string {
	line := make([]rune, width+6)
	for i := range line {
		line[i] = ' '
	}
	for tick := start.Truncate(15 * time.Second).Add(15 * time.Second); !tick.After(end); tick = tick.Add(15 * time.Second) {
		x := 6 + int(tick.Sub(start).Seconds()/end.Sub(start).Seconds()*float64(width-1))
		label := tick.Format(":05")
		for i, char := range label {
			if x+i < len(line) {
				line[x+i] = char
			}
		}
	}
	return labelText.Render(string(line))
}

func renderRecentChain(blocks []mempool.Block, width int) string {
	title := headerText.Render("RECENT BLOCKS") + "  " + labelText.Render("chain tip → history")
	blockWidth := 17
	visible := min(len(blocks), max(1, width/(blockWidth+2)))
	items := make([]string, 0, visible*2-1)
	for i := range visible {
		b := blocks[i]
		body := valueText.Render("#"+formatInt(b.Height)) + "\n" + age(b.Timestamp) + "\n" + formatInt(int64(b.TxCount)) + " tx"
		items = append(items, panelStyle.Width(blockWidth-4).Render(body))
		if i < visible-1 {
			items = append(items, labelText.Render("─"))
		}
	}
	return title + "\n" + lipgloss.JoinHorizontal(lipgloss.Center, items...)
}

func formatInt(value int64) string {
	raw := fmt.Sprintf("%d", value)
	for i := len(raw) - 3; i > 0; i -= 3 {
		raw = raw[:i] + "," + raw[i:]
	}
	return raw
}

func formatBytes(value int64, unit string) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.2f M%s", float64(value)/1_000_000, unit)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%.1f k%s", float64(value)/1_000, unit)
	}
	return fmt.Sprintf("%d %s", value, unit)
}

func formatBTC(sats float64) string {
	return fmt.Sprintf("%.4f BTC", sats/100_000_000)
}

func age(timestamp int64) string {
	d := time.Since(time.Unix(timestamp, 0))
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %dm ago", int(d.Hours()), int(d.Minutes())%60)
}

func main() {
	command := ""
	if len(os.Args) > 1 {
		command = os.Args[1]
		if !validCommand(command) {
			fmt.Fprintf(os.Stderr, "unknown module %q\n", command)
			fmt.Fprintf(os.Stderr, "available modules: %s\n", availableCommands())
			os.Exit(2)
		}
	}

	if _, err := tea.NewProgram(newModel(command)).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ompool: %v\n", err)
		os.Exit(1)
	}
}

func validCommand(command string) bool {
	for _, item := range modules {
		if item.command == command {
			return true
		}
	}
	return false
}

func availableCommands() string {
	commands := make([]string, 0, len(modules))
	for _, item := range modules {
		commands = append(commands, item.command)
	}
	return strings.Join(commands, ", ")
}
