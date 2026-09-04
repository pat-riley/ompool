package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"ompool/internal/mempool"
)

func renderActiveModule(cache *overviewRenderCache, active module, snapshot mempool.Overview, activity []activitySample, txValues []transactionValueSample, loading bool, err error, width, height, blockScroll, txPulse int, newTXIDs map[string]struct{}, blockPulse int, newBlockID string) string {
	if active.command == "viewer" {
		return renderTransactionViewer(transactionViewerState{}, width, height)
	}
	if active.command == "overview" {
		return renderOverviewCached(cache, snapshot, activity, loading, err, width, height, blockScroll, txPulse, newTXIDs, blockPulse, newBlockID)
	}
	if width < floorWidth || height < floorHeight {
		return renderTooSmall(width, height)
	}
	if snapshot.Fetched.IsZero() {
		return renderLoadingScreen(active, err, 0, width, height)
	}

	l := newLayout(width, height)
	header := renderModuleHeader(active, snapshot, loading, l)
	footer := labelText.Render(l.footer)
	bodyHeight := max(0, height-l.padY-lipgloss.Height(header)-2-1)
	var body string
	switch active.command {
	case "blockchain":
		body = renderBlockchainModule(cache, snapshot, l, bodyHeight, blockScroll, blockPulse, newBlockID)
	case "blocks":
		body = renderBlocksModule(cache, snapshot, l, bodyHeight, blockScroll, blockPulse, newBlockID)
	case "transactions":
		body = renderTransactionsModule(snapshot, txValues, l, bodyHeight, txPulse, newTXIDs)
	case "mempool":
		body = renderMempoolModule(snapshot, activity, txValues, l, bodyHeight)
	case "fees":
		body = renderFeesModule(snapshot, l, bodyHeight)
	case "difficulty":
		body = renderDifficultyModule(snapshot, l, bodyHeight)
	case "mining":
		body = renderMiningModule(snapshot, l, bodyHeight)
	case "lightning":
		body = renderLightningScaffold(l, bodyHeight)
	case "explorer":
		body = renderExplorerScaffold(snapshot, l, bodyHeight)
	default:
		body = renderModule(active, l.content, bodyHeight)
	}
	return renderWithAnchoredFooter(header+"\n\n"+body, footer, l)
}

func renderModuleHeader(active module, snapshot mempool.Overview, loading bool, l layout) string {
	left := headerText.Render("OMPOOL") + labelText.Render("  / ") + headerText.Render(strings.ToUpper(active.title))
	status := valueText.Render("● LIVE")
	if loading {
		status = labelText.Render("◌ REFRESHING")
	}
	status += labelText.Render("  " + snapshot.Fetched.UTC().Format("15:04:05 UTC"))
	gap := max(1, l.content-lipgloss.Width(left)-lipgloss.Width(status))
	return truncate(left+strings.Repeat(" ", gap)+status, l.content)
}

func placeCentered(content string, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content,
		lipgloss.WithWhitespaceChars(" "))
}

func renderBlockchainModule(cache *overviewRenderCache, snapshot mempool.Overview, l layout, height, scroll, blockPulse int, newBlockID string) string {
	if height < minPanelHeight {
		return ""
	}
	difficulty := renderDifficultyPanel(snapshot.Difficulty, l, l.content)
	difficultyHeight := lipgloss.Height(difficulty)
	miningHeight := min(8, max(0, height-difficultyHeight-minPanelHeight))
	mining := ""
	if miningHeight >= minPanelHeight {
		mining = renderMiningPanel(snapshot.Mining, snapshot.Blocks, l.content, miningHeight)
	}
	chainHeight := max(minPanelHeight, height-difficultyHeight-lipgloss.Height(mining))
	chain := renderBlockAnatomyChain(cache, snapshot.ProjectedBlocks, snapshot.Blocks, l.content, chainHeight, scroll, blockPulse, newBlockID)
	if chainHeight+difficultyHeight+lipgloss.Height(mining) > height {
		return chain
	}
	return lipgloss.JoinVertical(lipgloss.Left, chain, mining, difficulty)
}

func renderBlockAnatomyChain(cache *overviewRenderCache, projected []mempool.ProjectedBlock, confirmed []mempool.Block, width, height, scroll, blockPulse int, newBlockID string) string {
	inner := max(1, width-4)
	visible := max(1, height-4)
	var lines []string
	for i, block := range projected {
		lines = append(lines, blockAnatomyCard(blockAnatomy{
			label: fmt.Sprintf("PROJECTED +%d", i), pool: "mempool candidate", transactions: block.TxCount,
			size: int64(block.BlockSize), weight: int64(block.BlockVSize * 4), fee: block.MedianFee,
			fullness: min(1, block.BlockVSize/1_000_000), projected: true,
		}, inner)...)
		lines = append(lines, timelineConnector("awaiting confirmation", inner)...)
	}
	for i, block := range confirmed {
		fullness := float64(block.Weight) / 4_000_000
		if block.Weight == 0 {
			fullness = float64(block.Size) / 1_800_000
		}
		lines = append(lines, blockAnatomyCard(blockAnatomy{
			label: "#" + formatInt(block.Height), pool: poolName(block), transactions: block.TxCount,
			size: block.Size, weight: block.Weight, fee: block.Extras.MedianFee, totalFees: block.Extras.TotalFees,
			fullness: min(1, max(0, fullness)), fresh: block.ID == newBlockID && blockPulse > 0,
		}, inner)...)
		if i+1 < len(confirmed) {
			interval := confirmed[i].Timestamp - confirmed[i+1].Timestamp
			lines = append(lines, timelineConnector(formatInterval(interval), inner)...)
		}
	}
	maxScroll := max(0, len(lines)-visible)
	cache.blockMaxScroll = maxScroll
	scroll = min(max(0, scroll), maxScroll)
	title := headerText.Render("BLOCKCHAIN / BLOCK ANATOMY")
	title = appendIfItFits(title, labelText.Render("capacity fills bottom-up · braille is available space · ↑/↓ scroll"), inner)
	viewport := strings.Join(lines[scroll:min(len(lines), scroll+visible)], "\n")
	return panelStyle.Width(width).Height(max(1, height-2)).Render(title + "\n" + viewport)
}

type blockAnatomy struct {
	label, pool      string
	transactions     int
	size, weight     int64
	fee              float64
	totalFees        int64
	fullness         float64
	projected, fresh bool
}

func blockAnatomyCard(block blockAnatomy, width int) []string {
	if width < 46 {
		return compactBlockAnatomy(block, width)
	}
	const chamberWidth = 9
	const chamberRows = 5
	leftWidth := max(18, width-chamberWidth-7)
	labels := []string{
		block.label + "  " + block.pool,
		formatInt(int64(block.transactions)) + " transactions",
		formatBytes(block.size, "B"),
		formatBytes(block.weight, "WU"),
		fmt.Sprintf("%.1f sat/vB median", block.fee),
	}
	if block.totalFees > 0 {
		labels[4] += "  ·  " + formatBTC(float64(block.totalFees)) + " fees"
	}
	filledUnits := int(math.Round(block.fullness * chamberRows * 4))
	body := make([]string, chamberRows)
	for row := range chamberRows {
		unitsBelow := (chamberRows - 1 - row) * 4
		cellUnits := min(4, max(0, filledUnits-unitsBelow))
		cell := brailleCapacityCell(cellUnits)
		gauge := "░"
		if cellUnits > 0 {
			gauge = "█"
		}
		capacity := strings.Repeat(cell, chamberWidth) + " " + gauge
		body[row] = pad(ellipsize(labels[row], leftWidth), leftWidth) + "  " + capacity
	}
	style := blockCapacityStyle(block)
	card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(style.GetForeground()).Padding(0, 1).Width(width).Render(strings.Join(body, "\n"))
	return strings.Split(card, "\n")
}

func compactBlockAnatomy(block blockAnatomy, width int) []string {
	inside := max(8, width-4)
	gaugeWidth := max(4, min(12, inside/3))
	filled := min(gaugeWidth, int(math.Round(block.fullness*float64(gaugeWidth))))
	body := ellipsize(block.label+" · "+block.pool, inside) + "\n" +
		ellipsize(fmt.Sprintf("%s tx · %.1f sat/vB", formatInt(int64(block.transactions)), block.fee), inside) + "\n" +
		strings.Repeat("⣿", filled) + strings.Repeat("⠂", max(0, gaugeWidth-filled)) + fmt.Sprintf(" %.1f%%", block.fullness*100)
	style := blockCapacityStyle(block)
	card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(style.GetForeground()).Padding(0, 1).Width(width).Render(body)
	return strings.Split(card, "\n")
}

func brailleCapacityCell(units int) string {
	switch units {
	case 0:
		return "⠂"
	case 1:
		return "⣀"
	case 2:
		return "⣤"
	case 3:
		return "⣶"
	default:
		return "⣿"
	}
}

func blockCapacityStyle(block blockAnatomy) lipgloss.Style {
	color := lipgloss.Color("#38d66b")
	switch {
	case block.projected:
		color = orange
	case block.fresh:
		color = bright
	case block.fullness >= 0.97:
		color = lipgloss.Color("#ff3b30")
	case block.fullness >= 0.82:
		color = lipgloss.Color("#e6c84f")
	}
	return lipgloss.NewStyle().Foreground(color).Bold(block.fresh)
}

func timelineConnector(label string, width int) []string {
	indent := max(2, min(8, width/8))
	return []string{
		strings.Repeat(" ", indent) + labelText.Render("│  "+label),
		strings.Repeat(" ", indent) + valueText.Render("▼"),
	}
}

func formatInterval(seconds int64) string {
	if seconds <= 0 {
		return "unknown interval"
	}
	duration := time.Duration(seconds) * time.Second
	if duration < time.Minute {
		return fmt.Sprintf("%ds between blocks", int(duration.Seconds()))
	}
	return fmt.Sprintf("%dm %02ds between blocks", int(duration.Minutes()), int(duration.Seconds())%60)
}

func renderBlocksModule(cache *overviewRenderCache, snapshot mempool.Overview, l layout, height, scroll, blockPulse int, newBlockID string) string {
	if height < minPanelHeight {
		return ""
	}
	visualHeight := min(8, max(0, height-minPanelHeight))
	if visualHeight < minPanelHeight {
		return renderConfirmedBlocks(cache, snapshot.Blocks, l, l.content, height, scroll, blockPulse, newBlockID)
	}
	visual := renderBlockVisualSummary(snapshot.Blocks, l.content, visualHeight)
	table := renderConfirmedBlocks(cache, snapshot.Blocks, l, l.content, height-lipgloss.Height(visual), scroll, blockPulse, newBlockID)
	return lipgloss.JoinVertical(lipgloss.Left, visual, table)
}

func renderBlockVisualSummary(blocks []mempool.Block, width, height int) string {
	inner := max(1, width-4)
	intervals := make([]float64, 0, max(0, len(blocks)-1))
	poolCounts := map[string]int{}
	totalTx, totalFees := int64(0), int64(0)
	for i, block := range blocks {
		poolCounts[poolName(block)]++
		totalTx += int64(block.TxCount)
		totalFees += block.Extras.TotalFees
		if i+1 < len(blocks) && block.Timestamp > blocks[i+1].Timestamp {
			intervals = append(intervals, float64(block.Timestamp-blocks[i+1].Timestamp)/60)
		}
	}
	avgMinutes := 0.0
	for _, interval := range intervals {
		avgMinutes += interval
	}
	if len(intervals) > 0 {
		avgMinutes /= float64(len(intervals))
	}
	summary := fmt.Sprintf("%d blocks  ·  %.1f min avg  ·  %s tx  ·  %s fees", len(blocks), avgMinutes, formatInt(totalTx), formatBTC(float64(totalFees)))
	lines := []string{headerText.Render("RECENT BLOCK SIGNAL"), valueText.Render(truncate(summary, inner))}
	if len(intervals) > 0 {
		lines = append(lines,
			labelText.Render("CADENCE / MINUTES BETWEEN BLOCKS"),
			sparkline(intervals, inner),
		)
	}
	if len(poolCounts) > 0 {
		lines = append(lines, labelText.Render("POOLS   ")+poolShareLine(poolCounts, len(blocks), max(8, inner-8)))
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
}

func sparkline(values []float64, width int) string {
	if len(values) == 0 || width <= 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	maxValue := 1.0
	for _, value := range values {
		maxValue = max(maxValue, value)
	}
	var out strings.Builder
	for column := range width {
		index := min(len(values)-1, column*len(values)/width)
		value := values[index]
		levelIndex := min(len(blocks)-1, int(value/maxValue*float64(len(blocks)-1)))
		out.WriteRune(blocks[levelIndex])
	}
	return valueText.Render(out.String())
}

func poolShareLine(counts map[string]int, total, width int) string {
	type poolCount struct {
		name  string
		count int
	}
	ordered := make([]poolCount, 0, len(counts))
	for name, count := range counts {
		ordered = append(ordered, poolCount{name, count})
	}
	for i := range ordered {
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j].count > ordered[i].count {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	parts := make([]string, 0, min(3, len(ordered)))
	for _, pool := range ordered[:min(3, len(ordered))] {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", pool.name, float64(pool.count)/float64(max(1, total))*100))
	}
	return ellipsize(strings.Join(parts, "  ·  "), width)
}

func renderMiningPanel(mining mempool.MiningStats, recent []mempool.Block, width, height int) string {
	inner := max(1, width-4)
	hashrate := mining.Hashrate.CurrentHashrate
	if hashrate == 0 {
		hashrate = mining.Pools.LastEstimatedHashrate
	}
	rewardSats, _ := strconv.ParseFloat(mining.Rewards.TotalReward, 64)
	feeSats, _ := strconv.ParseFloat(mining.Rewards.TotalFee, 64)
	blocks := float64(max(1, int(mining.Rewards.EndBlock-mining.Rewards.StartBlock+1)))
	if rewardSats == 0 {
		for _, block := range recent {
			rewardSats += float64(block.Extras.Reward)
			feeSats += float64(block.Extras.TotalFees)
		}
		blocks = float64(max(1, len(recent)))
	}
	summary := fmt.Sprintf("%s  ·  %.2f T difficulty  ·  %.4f BTC avg reward  ·  %.4f BTC avg fees",
		formatHashrate(hashrate), mining.Hashrate.CurrentDifficulty/1e12, rewardSats/blocks/1e8, feeSats/blocks/1e8)
	lines := []string{headerText.Render("MINING NETWORK  /  7 DAYS"), valueText.Render(truncate(summary, inner))}
	if len(mining.Hashrate.Hashrates) > 0 {
		values := make([]float64, len(mining.Hashrate.Hashrates))
		for i, point := range mining.Hashrate.Hashrates {
			values[i] = point.AvgHashrate
		}
		lines = append(lines, labelText.Render("HASHRATE ")+sparkline(values, min(len(values), max(4, inner-11))))
	}
	if len(mining.Pools.Pools) > 0 {
		parts := make([]string, 0, min(4, len(mining.Pools.Pools)))
		for _, pool := range mining.Pools.Pools[:min(4, len(mining.Pools.Pools))] {
			share := float64(pool.BlockCount) / float64(max(1, mining.Pools.BlockCount)) * 100
			parts = append(parts, fmt.Sprintf("%s %.1f%%", pool.Name, share))
		}
		lines = append(lines, labelText.Render("TOP POOLS ")+ellipsize(strings.Join(parts, "  ·  "), max(1, inner-10)))
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines[:min(len(lines), max(1, height-2))], "\n"))
}

func formatHashrate(value float64) string {
	switch {
	case value >= 1e21:
		return fmt.Sprintf("%.2f ZH/s", value/1e21)
	case value >= 1e18:
		return fmt.Sprintf("%.2f EH/s", value/1e18)
	case value > 0:
		return fmt.Sprintf("%.2f PH/s", value/1e15)
	default:
		return "waiting for hashrate"
	}
}

func renderConfirmedBlocks(cache *overviewRenderCache, blocks []mempool.Block, l layout, width, height, scroll, blockPulse int, newBlockID string) string {
	inner := max(1, width-4)
	visible := max(1, height-4)
	maxScroll := max(0, len(blocks)-visible)
	cache.blockMaxScroll = maxScroll
	scroll = min(max(0, scroll), maxScroll)

	title := headerText.Render("CONFIRMED BLOCKS")
	if maxScroll > 0 {
		title = appendIfItFits(title, labelText.Render("↑/↓ history"), inner)
	}
	lines := []string{truncate(title, inner)}
	if len(blocks) == 0 {
		lines = append(lines, labelText.Render("Waiting for blocks…"))
	} else if inner >= 96 {
		widths := []int{11, 10, 18, 9, 11, 11, 13, 11}
		widths[2] += max(0, inner-7-sumInts(widths))
		lines = append(lines, labelText.Render(blockTableRow(widths, "HEIGHT", "AGE", "MINING POOL", "TXS", "SIZE", "WEIGHT", "TOTAL FEES", "MEDIAN")))
		for _, block := range blocks[scroll:min(len(blocks), scroll+visible)] {
			row := blockTableRow(widths, "#"+formatInt(block.Height), age(block.Timestamp), poolName(block), formatInt(int64(block.TxCount)), formatBytes(block.Size, "B"), formatBytes(block.Weight, "WU"), formatBTC(float64(block.Extras.TotalFees)), fmt.Sprintf("%.1f s/vB", block.Extras.MedianFee))
			if block.ID == newBlockID && blockPulse > 0 {
				row = valueText.Render(row)
			}
			lines = append(lines, row)
		}
	} else {
		for _, block := range blocks[scroll:min(len(blocks), scroll+visible)] {
			lines = append(lines, pad("#"+formatInt(block.Height), 13)+pad(age(block.Timestamp), 12)+ellipsize(poolName(block), max(1, inner-25)))
		}
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
}

func blockTableRow(widths []int, values ...string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = pad(value, widths[i])
	}
	return strings.Join(parts, " ")
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func renderTransactionsModule(snapshot mempool.Overview, txValues []transactionValueSample, l layout, height, txPulse int, newTXIDs map[string]struct{}) string {
	if height < minPanelHeight {
		return labelText.Render("Expand the terminal to view live transactions")
	}
	stats := []metric{
		{"MEMPOOL TXS", "MEMPOOL", formatInt(int64(snapshot.Mempool.Count))},
		{"LIVE BUFFER", "BUFFER", fmt.Sprintf("%d tx", len(snapshot.Recent))},
		{"BTC PRICE", "BTC/USD", formatUSD(snapshot.Prices.USD)},
		{"MIN RELAY FEE", "MIN FEE", fmt.Sprintf("%.1f sat/vB", snapshot.Fees.Minimum)},
	}
	top := renderMetricCards(stats, l.content, min(4, metricCardColumns(stats, l.content)), true)
	if height < lipgloss.Height(top)+minPanelHeight {
		return renderRecentTransactions(snapshot.Recent, snapshot.Prices.USD, l, l.content, height, txPulse, newTXIDs)
	}
	sections := []string{top}
	remaining := height - lipgloss.Height(top)
	chart := renderTransactionValuePanel(txValues, compactLayout(l), l.content)
	if remaining >= lipgloss.Height(chart)+minPanelHeight {
		sections = append(sections, chart)
		remaining -= lipgloss.Height(chart)
	}
	const miningHeight = 7
	if remaining >= miningHeight+minPanelHeight {
		sections = append(sections, renderMiningPanel(snapshot.Mining, snapshot.Blocks, l.content, miningHeight))
		remaining -= miningHeight
	}
	sections = append(sections, renderRecentTransactions(snapshot.Recent, snapshot.Prices.USD, l, l.content, remaining, txPulse, newTXIDs))
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func metricCardColumns(metrics []metric, width int) int {
	columns, _ := metricCardLayout(metrics, width)
	return columns
}

func renderMempoolModule(snapshot mempool.Overview, activity []activitySample, txValues []transactionValueSample, l layout, height int) string {
	if height < minPanelHeight {
		return labelText.Render("Expand the terminal to inspect the mempool")
	}
	stats := []metric{
		{"TRANSACTIONS", "TXS", formatInt(int64(snapshot.Mempool.Count))},
		{"VIRTUAL SIZE", "VSIZE", formatBytes(snapshot.Mempool.VSize, "vB")},
		{"TOTAL FEES", "FEES", fmt.Sprintf("%.3f BTC", snapshot.Mempool.TotalFee/100_000_000)},
		{"NEXT BLOCK", "NEXT", fmt.Sprintf("%d sat/vB", snapshot.Fees.Fastest)},
	}
	top := renderMetricCards(stats, l.content, metricCardColumns(stats, l.content), true)
	if lipgloss.Height(top) > height {
		return panelStyle.Width(l.content).Height(max(1, height-2)).Render(headerText.Render("MEMPOOL") + "\n" + valueText.Render(formatInt(int64(snapshot.Mempool.Count))+" transactions"))
	}
	remaining := height - lipgloss.Height(top)
	if remaining < minPanelHeight {
		return top
	}
	chart := renderActivityPanel(activity, l, l.content)
	valueChart := renderTransactionValuePanel(txValues, l, l.content)
	if lipgloss.Height(chart)+lipgloss.Height(valueChart) > remaining {
		compact := compactLayout(l)
		chart = renderActivityPanel(activity, compact, l.content)
		valueChart = renderTransactionValuePanel(txValues, compact, l.content)
		if lipgloss.Height(chart)+lipgloss.Height(valueChart) > remaining {
			return lipgloss.JoinVertical(lipgloss.Left, top, chart)
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, top, chart, valueChart)
}

func compactLayout(l layout) layout {
	l.chartHeight = 5
	return l
}

func renderTransactionValuePanel(samples []transactionValueSample, l layout, width int) string {
	inner := max(1, width-4)
	latest := 0.0
	if len(samples) > 0 {
		latest = samples[len(samples)-1].value
	}
	title := headerText.Render("LIVE TRANSACTION VALUE") + "  " + valueText.Render(fmt.Sprintf("%.4f BTC", latest))
	if l.subtitles {
		title += "  " + labelText.Render("2 min · 5 BTC ceiling")
	}
	chart := renderTransactionValueChart(samples, inner, l.chartHeight)
	return panelStyle.Width(width).Render(truncate(title, inner) + "\n" + chart)
}

func renderTransactionValueChart(samples []transactionValueSample, width, height int) string {
	const ceiling = 5.0
	const gutter = 8
	plotWidth := max(4, width-gutter)
	canvas := make([][]int, height)
	for row := range canvas {
		canvas[row] = make([]int, plotWidth)
	}
	end := time.Now()
	start := end.Add(-2 * time.Minute)
	for _, sample := range samples {
		if sample.at.Before(start) || sample.at.After(end.Add(time.Second)) {
			continue
		}
		x := int(sample.at.Sub(start).Seconds() / end.Sub(start).Seconds() * float64(plotWidth-1))
		x = min(max(0, x), plotWidth-1)
		value := min(ceiling, max(0, sample.value))
		y := int(math.Round((ceiling - value) / ceiling * float64(max(1, height-1))))
		y = min(max(0, y), height-1)
		canvas[y][x]++
	}
	rows := make([]string, 0, height+2)
	for row, cells := range canvas {
		value := ceiling * float64(height-1-row) / float64(max(1, height-1))
		var line strings.Builder
		fmt.Fprintf(&line, "%5.1f │", value)
		for _, count := range cells {
			switch {
			case count >= 4:
				line.WriteString(headerText.Render("▣"))
			case count >= 2:
				line.WriteString(valueText.Render("■"))
			case count == 1:
				line.WriteString(lipgloss.NewStyle().Foreground(orange).Faint(true).Render("▪"))
			default:
				line.WriteByte(' ')
			}
		}
		rows = append(rows, line.String())
	}
	rows = append(rows, strings.Repeat(" ", gutter-1)+"└"+strings.Repeat("─", plotWidth))
	axis := pad(strings.Repeat(" ", gutter)+"2m ago", max(0, width-3)) + "now"
	rows = append(rows, labelText.Render(truncate(axis, width)))
	return strings.Join(rows, "\n")
}

func renderProjectedBlocks(blocks []mempool.ProjectedBlock, width, height int) string {
	inner := max(1, width-4)
	lines := []string{headerText.Render("PROJECTED MEMPOOL BLOCKS")}
	if inner >= 66 {
		lines = append(lines, labelText.Render(pad("POSITION", 12)+pad("TRANSACTIONS", 18)+pad("VIRTUAL SIZE", 18)+pad("MEDIAN FEE", 16)+"FEE RANGE"))
	}
	visible := max(1, height-len(lines)-2)
	for i, block := range blocks[:min(len(blocks), visible)] {
		rangeText := feeRange(block.FeeRange)
		if inner >= 66 {
			lines = append(lines, pad(fmt.Sprintf("NEXT +%d", i), 12)+pad(formatInt(int64(block.TxCount)), 18)+pad(formatBytes(int64(block.BlockVSize), "vB"), 18)+pad(fmt.Sprintf("%.1f sat/vB", block.MedianFee), 16)+rangeText)
		} else {
			lines = append(lines, fmt.Sprintf("+%d  %s tx  %.1f sat/vB", i, formatInt(int64(block.TxCount)), block.MedianFee))
		}
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
}

func feeRange(values []float64) string {
	if len(values) == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f–%.1f", values[0], values[len(values)-1])
}

func renderFeesModule(snapshot mempool.Overview, l layout, height int) string {
	if height < minPanelHeight {
		return labelText.Render("Expand the terminal to inspect fee estimates")
	}
	market := renderFeeMarket(snapshot.Fees, snapshot.Prices.USD, l, l.content)
	if lipgloss.Height(market) > height {
		return panelStyle.Width(l.content).Height(max(1, height-2)).Render(headerText.Render("FEE MARKET") + "\n" + valueText.Render(fmt.Sprintf("%d sat/vB next block", snapshot.Fees.Fastest)))
	}
	remaining := height - lipgloss.Height(market)
	if remaining < minPanelHeight {
		return market
	}
	ladder := renderFeeLadder(snapshot.ProjectedBlocks, snapshot.Prices.USD, l.content, remaining)
	return lipgloss.JoinVertical(lipgloss.Left, market, ladder)
}

func renderFeeLadder(blocks []mempool.ProjectedBlock, usdPrice float64, width, height int) string {
	inner := max(1, width-4)
	lines := []string{headerText.Render("BLOCK FEE LADDER")}
	if inner >= 62 {
		lines = append(lines, labelText.Render(pad("CONFIRMATION", 18)+pad("MEDIAN", 16)+pad("140 vB TX", 16)+"RANGE"))
	}
	visible := max(1, height-len(lines)-2)
	for i, block := range blocks[:min(len(blocks), visible)] {
		cost := block.MedianFee * 140 / 100_000_000 * usdPrice
		if inner >= 62 {
			lines = append(lines, pad(confirmationWindow(i), 18)+pad(fmt.Sprintf("%.1f sat/vB", block.MedianFee), 16)+pad(fmt.Sprintf("~$%.2f", cost), 16)+feeRange(block.FeeRange))
		} else {
			lines = append(lines, fmt.Sprintf("%s  %.1f sat/vB  ~$%.2f", confirmationWindow(i), block.MedianFee, cost))
		}
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
}

func confirmationWindow(index int) string {
	if index == 0 {
		return "NEXT BLOCK"
	}
	return fmt.Sprintf("~%d–%d MIN", index*10, (index+1)*10)
}

func renderDifficultyModule(snapshot mempool.Overview, l layout, height int) string {
	if height < minPanelHeight {
		return labelText.Render("Expand the terminal to inspect difficulty")
	}
	progress := renderDifficultyPanel(snapshot.Difficulty, l, l.content)
	remaining := height - lipgloss.Height(progress)
	if remaining < minPanelHeight {
		return progress
	}
	return lipgloss.JoinVertical(lipgloss.Left, progress, renderMiningCadence(snapshot, l.content, remaining))
}

func renderMiningCadence(snapshot mempool.Overview, width, height int) string {
	inner := max(1, width-4)
	d := snapshot.Difficulty
	retarget := time.UnixMilli(d.EstimatedRetargetDate)
	if d.EstimatedRetargetDate < 10_000_000_000 {
		retarget = time.Unix(d.EstimatedRetargetDate, 0)
	}
	stats := []string{
		headerText.Render("MINING CADENCE"),
		labelText.Render("NEXT RETARGET") + "  " + valueText.Render("#"+formatInt(d.NextRetargetHeight)),
		labelText.Render("ESTIMATED DATE") + "  " + valueText.Render(retarget.Local().Format("Jan 02 · 15:04 MST")),
		labelText.Render("TIME REMAINING") + "  " + valueText.Render((time.Duration(d.RemainingTime) * time.Millisecond).Round(time.Minute).String()),
	}
	if len(snapshot.Blocks) > 1 {
		span := snapshot.Blocks[0].Timestamp - snapshot.Blocks[len(snapshot.Blocks)-1].Timestamp
		average := time.Duration(span/int64(len(snapshot.Blocks)-1)) * time.Second
		stats = append(stats, labelText.Render("RECENT AVERAGE")+"  "+valueText.Render(average.Round(time.Second).String()+" / block"))
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(truncate(strings.Join(stats, "\n"), inner*max(1, height-2)))
}

func renderMiningModule(snapshot mempool.Overview, l layout, height int) string {
	mining := snapshot.Mining
	reward, _ := strconv.ParseFloat(mining.Rewards.TotalReward, 64)
	fees, _ := strconv.ParseFloat(mining.Rewards.TotalFee, 64)
	blockCount := max(1, int(mining.Rewards.EndBlock-mining.Rewards.StartBlock+1))
	stats := []metric{
		{"NETWORK HASHRATE", "HASHRATE", formatHashrate(mining.Hashrate.CurrentHashrate)},
		{"DIFFICULTY", "DIFF", fmt.Sprintf("%.2f T", mining.Hashrate.CurrentDifficulty/1e12)},
		{"AVG BLOCK REWARD", "AVG REWARD", formatBTC(reward / float64(blockCount))},
		{"AVG BLOCK FEES", "AVG FEES", formatBTC(fees / float64(blockCount))},
	}
	if height < minPanelHeight {
		return labelText.Render("Expand the terminal to inspect mining")
	}
	top := renderMetricCards(stats, l.content, metricCardColumns(stats, l.content), true)
	if lipgloss.Height(top) > height {
		return panelStyle.Width(l.content).Height(max(1, height-2)).Render(headerText.Render("MINING") + "\n" + valueText.Render(formatHashrate(mining.Hashrate.CurrentHashrate)))
	}
	remaining := height - lipgloss.Height(top)
	chart := renderHashratePanel(mining.Hashrate, l, l.content)
	if remaining < lipgloss.Height(chart)+minPanelHeight {
		return lipgloss.JoinVertical(lipgloss.Left, top, renderMiningPanel(mining, snapshot.Blocks, l.content, remaining))
	}
	remaining -= lipgloss.Height(chart)
	if remaining >= minPanelHeight && l.content >= 84 {
		gap := 2
		leftWidth := (l.content - gap) * 3 / 5
		rightWidth := l.content - gap - leftWidth
		distribution := renderPoolDistribution(mining.Pools, leftWidth, remaining)
		adjustments := renderMiningAdjustments(mining.Hashrate.Difficulty, rightWidth, remaining)
		lower := lipgloss.JoinHorizontal(lipgloss.Top, distribution, strings.Repeat(" ", gap), adjustments)
		return lipgloss.JoinVertical(lipgloss.Left, top, chart, lower)
	}
	if remaining >= minPanelHeight*2 {
		poolHeight := remaining / 2
		distribution := renderPoolDistribution(mining.Pools, l.content, poolHeight)
		adjustments := renderMiningAdjustments(mining.Hashrate.Difficulty, l.content, remaining-poolHeight)
		return lipgloss.JoinVertical(lipgloss.Left, top, chart, distribution, adjustments)
	}
	distribution := renderPoolDistribution(mining.Pools, l.content, remaining)
	return lipgloss.JoinVertical(lipgloss.Left, top, chart, distribution)
}

func renderHashratePanel(hashrate mempool.HashrateStats, l layout, width int) string {
	inner := max(1, width-4)
	title := headerText.Render("NETWORK HASHRATE") + "  " + valueText.Render(formatHashrate(hashrate.CurrentHashrate))
	if l.subtitles {
		title += "  " + labelText.Render("3 months · normalized change")
	}
	chart := renderHashrateChart(hashrate, inner, l.chartHeight)
	return panelStyle.Width(width).Render(truncate(title, inner) + "\n" + chart)
}

func renderHashrateChart(hashrate mempool.HashrateStats, width, height int) string {
	return renderMiningOverlay(hashrate.Hashrates, hashrate.Difficulty, width, height)
}

func renderMiningOverlay(points []mempool.HashratePoint, difficulty []mempool.DifficultyPoint, width, height int) string {
	plotWidth := max(4, width-9)
	hashrate := sampleHashrate(points, plotWidth)
	moving := movingAverage(hashrate, min(7, max(1, plotWidth/8)))
	difficultySeries := sampleDifficulty(points, difficulty, plotWidth)
	series := [][]float64{percentChange(hashrate), percentChange(moving), percentChange(difficultySeries)}
	low, high := -1.0, 1.0
	for _, values := range series {
		for _, value := range values {
			low, high = min(low, value), max(high, value)
		}
	}
	padding := max(1.0, (high-low)*0.08)
	low, high = low-padding, high+padding
	canvas := make([][]rune, height)
	for row := range canvas {
		canvas[row] = []rune(strings.Repeat(" ", plotWidth))
	}
	glyphs := []rune{'•', '◆', '×'}
	for seriesIndex, values := range series {
		plotSeries(canvas, values, low, high, glyphs[seriesIndex])
	}
	rows := make([]string, 0, height+2)
	for row, cells := range canvas {
		level := high - (high-low)*float64(row)/float64(max(1, height-1))
		var line strings.Builder
		fmt.Fprintf(&line, "%+5.0f%% │", level)
		for _, cell := range cells {
			switch cell {
			case '•':
				line.WriteString(valueText.Render(string(cell)))
			case '◆':
				line.WriteString(headerText.Render(string(cell)))
			case '×':
				line.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#e6c84f")).Render(string(cell)))
			default:
				line.WriteRune(cell)
			}
		}
		rows = append(rows, line.String())
	}
	rows = append(rows, "       └"+strings.Repeat("─", plotWidth))
	legend := valueText.Render("• HASHRATE") + "  " + headerText.Render("◆ 7D MA") + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#e6c84f")).Render("× DIFFICULTY")
	rows = append(rows, truncate(legend, width))
	return strings.Join(rows, "\n")
}

func sampleHashrate(points []mempool.HashratePoint, width int) []float64 {
	values := make([]float64, width)
	for x := range width {
		if len(points) > 0 {
			values[x] = points[min(len(points)-1, x*len(points)/width)].AvgHashrate
		}
	}
	return values
}

func sampleDifficulty(points []mempool.HashratePoint, adjustments []mempool.DifficultyPoint, width int) []float64 {
	values := make([]float64, width)
	if len(adjustments) == 0 {
		return values
	}
	for x := range width {
		timestamp := adjustments[0].Time
		if len(points) > 0 {
			timestamp = points[min(len(points)-1, x*len(points)/width)].Timestamp
		}
		value := adjustments[0].Difficulty
		for _, adjustment := range adjustments {
			if adjustment.Time > timestamp {
				break
			}
			value = adjustment.Difficulty
		}
		values[x] = value
	}
	return values
}

func movingAverage(values []float64, window int) []float64 {
	averages := make([]float64, len(values))
	for i := range values {
		start := max(0, i-window+1)
		for _, value := range values[start : i+1] {
			averages[i] += value
		}
		averages[i] /= float64(i - start + 1)
	}
	return averages
}

func percentChange(values []float64) []float64 {
	changes := make([]float64, len(values))
	base := 0.0
	for _, value := range values {
		if value > 0 {
			base = value
			break
		}
	}
	if base == 0 {
		return changes
	}
	for i, value := range values {
		changes[i] = (value/base - 1) * 100
	}
	return changes
}

func plotSeries(canvas [][]rune, values []float64, low, high float64, glyph rune) {
	if len(canvas) == 0 || len(values) == 0 || high == low {
		return
	}
	previous := -1
	for x, value := range values {
		y := int(math.Round((high - value) / (high - low) * float64(len(canvas)-1)))
		y = min(max(0, y), len(canvas)-1)
		if previous >= 0 {
			start, end := min(previous, y), max(previous, y)
			for row := start; row <= end; row++ {
				canvas[row][x] = glyph
			}
		} else {
			canvas[y][x] = glyph
		}
		previous = y
	}
}

func renderPoolDistribution(pools mempool.PoolStats, width, height int) string {
	lines := []string{headerText.Render("POOL SHARE MOSAIC") + "  " + labelText.Render("each cell ≈ 1% / trailing 7 days")}
	if len(pools.Pools) == 0 {
		lines = append(lines, labelText.Render("Waiting for pool data…"))
		return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
	}
	const columns = 20
	const cells = 100
	mosaic := make([]rune, cells)
	for i := range mosaic {
		mosaic[i] = '·'
	}
	cursor := 0
	for index, pool := range pools.Pools[:min(8, len(pools.Pools))] {
		count := int(math.Round(float64(pool.BlockCount) / float64(max(1, pools.BlockCount)) * cells))
		for range count {
			if cursor >= cells {
				break
			}
			mosaic[cursor] = rune('A' + index)
			cursor++
		}
	}
	rowsAvailable := max(1, min(5, height-3))
	for row := range rowsAvailable {
		lines = append(lines, valueText.Render(string(mosaic[row*columns:(row+1)*columns])))
	}
	legendRoom := max(0, height-2-len(lines))
	for index, pool := range pools.Pools[:min(min(8, len(pools.Pools)), legendRoom)] {
		share := float64(pool.BlockCount) / float64(max(1, pools.BlockCount)) * 100
		lines = append(lines, fmt.Sprintf("%c  %-16s %5.1f%%  %d blocks", 'A'+index, ellipsize(pool.Name, 16), share, pool.BlockCount))
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
}

func renderMiningAdjustments(adjustments []mempool.DifficultyPoint, width, height int) string {
	inner := max(1, width-4)
	lines := []string{headerText.Render("DIFFICULTY ADJUSTMENTS"), labelText.Render(pad("HEIGHT", 13) + pad("DATE", 10) + "CHANGE")}
	visible := max(1, height-4)
	for i := len(adjustments) - 1; i >= 0 && len(lines)-2 < visible; i-- {
		adjustment := adjustments[i]
		change := (adjustment.Adjustment - 1) * 100
		row := pad("#"+formatInt(adjustment.Height), 13) + pad(time.Unix(adjustment.Time, 0).Format("Jan 02"), 10) + fmt.Sprintf("%+.2f%%", change)
		lines = append(lines, truncate(row, inner))
	}
	if len(adjustments) == 0 {
		lines = append(lines, labelText.Render("Waiting for adjustment history…"))
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
}

func renderLightningScaffold(l layout, height int) string {
	if height < minPanelHeight {
		return labelText.Render("Expand the terminal to inspect Lightning")
	}
	stats := []metric{
		{"PUBLIC NODES", "NODES", "connecting"},
		{"PUBLIC CHANNELS", "CHANNELS", "connecting"},
		{"NETWORK CAPACITY", "CAPACITY", "connecting"},
		{"MEDIAN BASE FEE", "BASE FEE", "connecting"},
	}
	top := renderMetricCards(stats, l.content, metricCardColumns(stats, l.content), true)
	body := headerText.Render("LIGHTNING NETWORK") + "\n\n" +
		labelText.Render("Scaffold ready for network statistics, capacity history, channel distribution, and geographic node data.") + "\n\n" +
		valueText.Render("Planned data: latest · 24h · 1w")
	if l.height <= 20 || lipgloss.Height(top)+minPanelHeight+1 > height {
		return panelStyle.Width(l.content).Height(max(1, height-2)).Render(body)
	}
	panelHeight := max(minPanelHeight, height-lipgloss.Height(top)-1)
	panel := panelStyle.Width(l.content).Height(max(1, panelHeight-2)).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, top, panel)
}

func renderExplorerScaffold(snapshot mempool.Overview, l layout, height int) string {
	if height < minPanelHeight {
		return labelText.Render("Expand the terminal to use Explorer")
	}
	inner := max(1, l.content-4)
	search := headerText.Render("UNIVERSAL BITCOIN EXPLORER") + "\n\n" +
		valueText.Render("╭"+strings.Repeat("─", max(1, inner-2))+"╮") + "\n" +
		valueText.Render("│") + " " + labelText.Render(ellipsize("Enter a transaction ID, block hash, height, or address", max(1, inner-4))) + strings.Repeat(" ", max(0, inner-3-len("Enter a transaction ID, block hash, height, or address"))) + valueText.Render("│") + "\n" +
		valueText.Render("╰"+strings.Repeat("─", max(1, inner-2))+"╯") + "\n\n" +
		labelText.Render("Scaffolded routes") + "  " + valueText.Render("transaction  ·  block  ·  address  ·  script") + "\n" +
		labelText.Render("Current chain tip") + "  " + valueText.Render("#"+formatInt(snapshot.Blocks[0].Height))
	return panelStyle.Width(l.content).Height(max(1, height-2)).Render(search)
}
