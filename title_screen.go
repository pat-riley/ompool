package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The title screen is the first thing ompool draws: the wordmark and module
// menu sit in the middle of the terminal while a dimmed chain of blocks
// scrolls above them and dummy charts play beneath. The same backdrop carries the loading screen
// so opening a module feels continuous with the launcher.
//
// Every element of the backdrop is a pure function of the frame counter, so a
// frame can be rendered for any size without state and tests can compare
// frames directly.

const titleFrameInterval = 100 * time.Millisecond

type titleTickMsg struct{ gen int }

func scheduleTitleTick(gen int) tea.Cmd {
	return tea.Tick(titleFrameInterval, func(time.Time) tea.Msg { return titleTickMsg{gen: gen} })
}

// backdropStyle indexes the dimmed palette. The backdrop must stay
// subordinate to the box in front of it, so nothing here uses a full
// brightness colour except the short glow that marks a freshly mined block.
type backdropStyle uint8

const (
	bdPlain backdropStyle = iota
	bdLine
	bdText
	bdOrange
	bdGreen
	bdYellow
	bdRed
	bdBright
	bdGlow
)

var backdropStyles = [...]lipgloss.Style{
	bdPlain:  lipgloss.NewStyle(),
	bdLine:   lipgloss.NewStyle().Foreground(lipgloss.Color("#353535")),
	bdText:   lipgloss.NewStyle().Foreground(lipgloss.Color("#5a5a5a")),
	bdOrange: lipgloss.NewStyle().Foreground(lipgloss.Color("#96580f")),
	bdGreen:  lipgloss.NewStyle().Foreground(lipgloss.Color("#24743e")),
	bdYellow: lipgloss.NewStyle().Foreground(lipgloss.Color("#857328")),
	bdRed:    lipgloss.NewStyle().Foreground(lipgloss.Color("#882a22")),
	bdBright: lipgloss.NewStyle().Foreground(lipgloss.Color("#9a9a9a")),
	bdGlow:   lipgloss.NewStyle().Bold(true).Foreground(orange),
}

// backdropSGR holds each style's opening and closing escape sequences, taken
// once from lipgloss, so a frame's hundreds of styled runs are plain string
// concatenation instead of a full style render each.
var backdropSGR = func() [len(backdropStyles)]struct{ open, close string } {
	var sequences [len(backdropStyles)]struct{ open, close string }
	for i, style := range backdropStyles {
		const marker = "\x00"
		rendered := style.Render(marker)
		open, close, _ := strings.Cut(rendered, marker)
		sequences[i] = struct{ open, close string }{open, close}
	}
	return sequences
}()

type backdropCell struct {
	r     rune
	style backdropStyle
}

// backdrop is a fixed-size grid of single-width cells. Drawing into a grid
// rather than concatenating strings is what lets the box in front simply
// replace a rectangle of cells, and it guarantees the output is exactly the
// terminal's size no matter what the animations do near the edges.
type backdrop struct {
	width, height int
	cells         []backdropCell
}

func newBackdrop(width, height int) *backdrop {
	b := &backdrop{width: width, height: height, cells: make([]backdropCell, width*height)}
	for i := range b.cells {
		b.cells[i].r = ' '
	}
	return b
}

func (b *backdrop) set(x, y int, r rune, style backdropStyle) {
	if x < 0 || y < 0 || x >= b.width || y >= b.height {
		return
	}
	b.cells[y*b.width+x] = backdropCell{r: r, style: style}
}

func (b *backdrop) text(x, y int, s string, style backdropStyle) {
	for i, r := range []rune(s) {
		b.set(x+i, y, r, style)
	}
}

func (b *backdrop) clear(x, y, w, h int) {
	for row := y; row < y+h; row++ {
		for column := x; column < x+w; column++ {
			b.set(column, row, ' ', bdPlain)
		}
	}
}

func (b *backdrop) renderRow(y, from, to int) string {
	from, to = max(0, from), min(b.width, to)
	if y < 0 || y >= b.height || from >= to {
		return ""
	}
	var out strings.Builder
	current := bdPlain
	for x := from; x < to; x++ {
		cell := b.cells[y*b.width+x]
		style := cell.style
		if cell.r == ' ' {
			style = bdPlain
		}
		if style != current {
			out.WriteString(backdropSGR[current].close)
			out.WriteString(backdropSGR[style].open)
			current = style
		}
		out.WriteRune(cell.r)
	}
	out.WriteString(backdropSGR[current].close)
	return out.String()
}

// render composes the backdrop with a block of pre-styled lines placed in the
// centre. The lines replace the cells beneath them outright, so nothing from
// the animation ever bleeds through the box, and a one-cell margin around it
// keeps the border legible against busy neighbours.
func (b *backdrop) render(overlay []string) string {
	overlayWidth := 0
	for _, line := range overlay {
		overlayWidth = max(overlayWidth, lipgloss.Width(line))
	}
	overlayWidth = min(overlayWidth, b.width)
	ox := max(0, (b.width-overlayWidth)/2)
	oy := max(0, (b.height-len(overlay))/2)
	b.clear(ox-2, oy-1, overlayWidth+4, len(overlay)+2)

	rows := make([]string, b.height)
	for y := range b.height {
		index := y - oy
		if index < 0 || index >= len(overlay) {
			rows[y] = b.renderRow(y, 0, b.width)
			continue
		}
		line := pad(truncate(overlay[index], overlayWidth), overlayWidth)
		rows[y] = b.renderRow(y, 0, ox) + line + b.renderRow(y, ox+overlayWidth, b.width)
	}
	return strings.Join(rows, "\n")
}

// noise is a deterministic hash of an integer onto [0, 1), used wherever the
// dummy data needs to look random while staying identical from frame to frame.
func noise(n int) float64 {
	x := uint64(int64(n))*0x9E3779B97F4A7C15 + 0x632BE59BD9B4E019
	x ^= x >> 31
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 29
	x *= 0x94D049BB133111EB
	x ^= x >> 32
	return float64(x>>11) / float64(uint64(1)<<53)
}

func wave(i int, period float64) float64 {
	return math.Sin(float64(i) * 2 * math.Pi / period)
}

func clampUnit(value, low, high float64) float64 {
	return math.Min(high, math.Max(low, value))
}

// flowValue is the dummy transaction flow as a fraction of the chart ceiling.
func flowValue(i int) float64 {
	v := 0.32 + 0.2*wave(i, 41) + 0.12*wave(i, 13) + 0.07*wave(i, 5) + 0.18*(noise(i)-0.5)
	return clampUnit(v, 0.04, 1)
}

// hashrateValue is the dummy hashrate as a percentage change from its start.
func hashrateValue(i int) float64 {
	return 6*wave(i, 97) + 3*wave(i, 23) + 2.5*(noise(i*7)-0.5) + float64(i%200)/40
}

const hexDigits = "0123456789abcdef"

func hexAt(i int) byte {
	return hexDigits[int(noise(i)*16)&15]
}

func hexString(seed, length int) string {
	out := make([]byte, length)
	for i := range out {
		out[i] = hexAt(seed*64 + i)
	}
	return string(out)
}

var dummyPools = []string{"Foundry USA", "AntPool", "ViaBTC", "F2Pool", "MARA Pool", "SpiderPool", "Luxor", "Braiins", "SECPOOL", "Ocean"}

const (
	dummyBaseHeight = 912_400
	chainCardWidth  = 13
	chainCardGap    = 5
	chainSlot       = chainCardWidth + chainCardGap
	chainCardRows   = 6
	chainRows       = chainCardRows + 1 // cards plus the hash tape beneath them
)

// drawChain scrolls a chain of block cards leftwards across the rows starting
// at top. New candidates enter from the right in orange; as the chain moves
// on, the newest is confirmed with a brief glow and settles into the colour of
// how full it was.
func drawChain(b *backdrop, top, frame int) {
	scroll := frame / 2
	// The two rightmost slots are still being assembled from the mempool.
	tip := (b.width+scroll)/chainSlot - 2
	sinceTip := (b.width + scroll) % chainSlot
	firstSlot := scroll / chainSlot
	for slot := firstSlot; ; slot++ {
		x := slot*chainSlot - scroll
		if x >= b.width {
			break
		}
		height := dummyBaseHeight + slot
		fullness := 0.6 + 0.4*noise(slot*3)
		style := bdGreen
		switch {
		case fullness >= 0.97:
			style = bdRed
		case fullness >= 0.84:
			style = bdYellow
		}
		label := "#" + formatInt(int64(height))
		pool := dummyPools[int(noise(slot*5)*float64(len(dummyPools)))%len(dummyPools)]
		switch {
		case slot > tip:
			style = bdOrange
			label = "NEXT"
			if slot > tip+1 {
				label = "NEXT +1"
			}
			pool = "mempool"
			// Candidates fill up as the chain approaches the moment they are found.
			fullness *= clampUnit(float64(sinceTip)/float64(chainSlot)+0.15, 0, 1)
		case slot == tip && sinceTip < 8:
			style = bdGlow
		}
		drawChainCard(b, x, top, label, pool, fullness, int(2200+noise(slot*11)*2300), style)
		if x+chainCardWidth < b.width {
			drawChainLink(b, x+chainCardWidth, top+chainCardRows/2, frame, slot > tip)
		}
	}
	drawHashTape(b, top+chainCardRows, scroll)
}

func drawChainCard(b *backdrop, x, y int, label, pool string, fullness float64, txCount int, style backdropStyle) {
	inner := chainCardWidth - 2
	b.text(x, y, "╭"+strings.Repeat("─", inner)+"╮", style)
	for row := 1; row < chainCardRows-1; row++ {
		b.set(x, y+row, '│', style)
		b.set(x+chainCardWidth-1, y+row, '│', style)
	}
	b.text(x, y+chainCardRows-1, "╰"+strings.Repeat("─", inner)+"╯", style)

	textStyle := bdText
	if style == bdGlow {
		textStyle = bdBright
	}
	b.text(x+2, y+1, ellipsize(label, inner-2), textStyle)
	b.text(x+2, y+2, ellipsize(pool, inner-2), textStyle)
	units := int(math.Round(fullness * float64(inner-2) * 4))
	for cell := range inner - 2 {
		b.set(x+2+cell, y+3, []rune(brailleCapacityCell(min(4, max(0, units-cell*4))))[0], style)
	}
	b.text(x+2, y+4, ellipsize(formatInt(int64(txCount))+" tx", inner-2), textStyle)
}

// drawChainLink joins two cards with a rail carrying a single pulse, so the
// chain reads as data moving between blocks even while the cards themselves
// are between scroll steps.
func drawChainLink(b *backdrop, x, y, frame int, pending bool) {
	style := bdLine
	if pending {
		style = bdOrange
	}
	pulse := frame % chainCardGap
	for i := range chainCardGap {
		if i == pulse && !pending {
			b.set(x+i, y, '●', bdBright)
			continue
		}
		b.set(x+i, y, '═', style)
	}
}

// drawHashTape scrolls block hashes beneath the chain. Each slot's hash keeps
// the run of leading zeros a real proof of work carries.
func drawHashTape(b *backdrop, y, scroll int) {
	for x := range b.width {
		world := x + scroll
		slot, offset := world/chainSlot, world%chainSlot
		switch {
		case offset == chainSlot-1:
			b.set(x, y, ' ', bdPlain)
		case offset < 7:
			b.set(x, y, '0', bdLine)
		default:
			b.set(x, y, rune(hexAt(slot*chainSlot+offset)), bdText)
		}
	}
}

// drawChainStrip is the one-row chain used when the terminal cannot hold cards.
func drawChainStrip(b *backdrop, y, frame int) {
	const slot = 14
	scroll := frame / 2
	tip := (b.width+scroll)/slot - 2
	for slotIndex := scroll / slot; ; slotIndex++ {
		x := slotIndex*slot - scroll
		if x >= b.width {
			break
		}
		style, label := bdGreen, formatInt(int64(dummyBaseHeight+slotIndex))
		if slotIndex > tip {
			style, label = bdOrange, "next"
		}
		b.set(x, y, '▣', style)
		b.text(x+2, y, label, bdText)
		b.text(x+9, y, "═══", bdLine)
		if frame%3 == 0 {
			b.set(x+10, y, '●', bdBright)
		}
	}
}

type dummyChart func(b *backdrop, x, y, width, height, frame int)

// drawCharts lays the dummy charts across the bottom of the backdrop, adding
// charts from left to right as the width allows.
func drawCharts(b *backdrop, y, height, frame int) {
	if height < 5 {
		drawSparkStrip(b, y+height-1, frame)
		return
	}
	// Only plot heights that divide the flow ceiling evenly are used, so every
	// axis label lands on a round number; the charts are then anchored to the
	// bottom of the region they were given.
	plotRows := 5
	for _, candidate := range []int{17, 11, 9} {
		if height-3 >= candidate {
			plotRows = candidate
			break
		}
	}
	y, height = y+height-(plotRows+3), plotRows+3
	charts := []dummyChart{drawFlowChart, drawHashrateChart, drawValueChart}
	const gap = 3
	const minChart = 34
	count := max(1, min(len(charts), (b.width+gap)/(minChart+gap)))
	width := (b.width - gap*(count-1)) / count
	if width < 20 {
		return
	}
	for i := range count {
		charts[i](b, i*(width+gap), y, width, height, frame)
	}
}

func drawSparkStrip(b *backdrop, y, frame int) {
	blocks := []rune("▁▂▃▄▅▆▇█")
	for x := range b.width {
		level := flowValue(x + frame/2)
		b.set(x, y, blocks[min(len(blocks)-1, int(level*float64(len(blocks))))], bdOrange)
	}
}

func chartFrame(b *backdrop, x, y, width, height int, title, subtitle string, gutter int) int {
	title = ellipsize(title, width)
	b.text(x, y, title, bdBright)
	if rest := width - len([]rune(title)) - 2; rest >= len([]rune(subtitle)) {
		b.text(x+len([]rune(title))+2, y, subtitle, bdText)
	}
	plotRows := height - 3
	b.set(x+gutter-1, y+1+plotRows, '└', bdLine)
	for column := gutter; column < width; column++ {
		b.set(x+column, y+1+plotRows, '─', bdLine)
	}
	return plotRows
}

// drawFlowChart is the braille bar chart from the overview's transaction flow
// panel, fed by a rolling dummy series.
func drawFlowChart(b *backdrop, x, y, width, height, frame int) {
	const gutter = 7
	plotRows := chartFrame(b, x, y, width, height, "INCOMING TRANSACTION FLOW", "vB/s", gutter)
	plotColumns := width - gutter
	fills := []rune(" ⡀⡄⡆⡇")
	for row := range plotRows {
		label := formatAxisValue(activityCeiling*float64(plotRows-1-row)/float64(max(1, plotRows-1)), true)
		b.text(x, y+1+row, padLeft(label, gutter-2)+" │", bdLine)
	}
	for column := range plotColumns {
		level := flowValue(column + frame/2)
		dots := int(math.Ceil(level * float64(plotRows*4)))
		for row := range plotRows {
			below := (plotRows - 1 - row) * 4
			cell := min(4, max(0, dots-below))
			if cell == 0 {
				continue
			}
			value := activityCeiling * float64(below+cell) / float64(plotRows*4)
			style := bdGreen
			switch {
			case value >= 4000:
				style = bdRed
			case value >= 2500:
				style = bdOrange
			case value >= 1000:
				style = bdYellow
			}
			b.set(x+gutter+column, y+1+row, fills[cell], style)
		}
	}
	b.text(x+gutter, y+height-1, "2m ago", bdText)
	b.text(x+width-3, y+height-1, "now", bdText)
}

// drawHashrateChart is the mining module's overlay of hashrate and its moving
// average, as percentage change over a dummy quarter.
func drawHashrateChart(b *backdrop, x, y, width, height, frame int) {
	const gutter = 7
	plotRows := chartFrame(b, x, y, width, height, "NETWORK HASHRATE", "90 days", gutter)
	plotColumns := width - gutter
	values := make([]float64, plotColumns)
	for column := range values {
		values[column] = hashrateValue(column + frame/3)
	}
	average := movingAverage(values, max(1, plotColumns/8))
	low, high := -12.0, 16.0
	for row := range plotRows {
		level := high - (high-low)*float64(row)/float64(max(1, plotRows-1))
		b.text(x, y+1+row, fmt.Sprintf("%+4.0f%% │", level), bdLine)
	}
	plotDummySeries(b, x+gutter, y+1, plotRows, average, low, high, '◆', bdBright)
	plotDummySeries(b, x+gutter, y+1, plotRows, values, low, high, '•', bdOrange)
	b.text(x+gutter, y+height-1, "• HASHRATE", bdOrange)
	b.text(x+gutter+12, y+height-1, "◆ 7D MA", bdBright)
}

func plotDummySeries(b *backdrop, x, y, rows int, values []float64, low, high float64, glyph rune, style backdropStyle) {
	previous := -1
	for column, value := range values {
		row := int(math.Round((high - value) / (high - low) * float64(rows-1)))
		row = min(max(0, row), rows-1)
		start, end := row, row
		if previous >= 0 {
			start, end = min(previous, row), max(previous, row)
		}
		for r := start; r <= end; r++ {
			b.set(x+column, y+r, glyph, style)
		}
		previous = row
	}
}

// drawValueChart is the transaction value scatter from the mempool module.
func drawValueChart(b *backdrop, x, y, width, height, frame int) {
	const gutter = 7
	plotRows := chartFrame(b, x, y, width, height, "TRANSACTION VALUES", "BTC", gutter)
	plotColumns := width - gutter
	for row := range plotRows {
		value := 5 * float64(plotRows-1-row) / float64(max(1, plotRows-1))
		b.text(x, y+1+row, fmt.Sprintf("%5.1f │", value), bdLine)
	}
	for column := range plotColumns {
		world := column + frame/2
		points := int(noise(world*13) * 4)
		for point := range points {
			seed := world*17 + point*5
			value := math.Pow(noise(seed), 2.2) * 5
			row := int(math.Round((5 - value) / 5 * float64(plotRows-1)))
			density := int(noise(seed+1) * 3)
			glyph, style := '▪', bdOrange
			switch density {
			case 1:
				glyph, style = '■', bdOrange
			case 2:
				glyph, style = '▣', bdBright
			}
			b.set(x+gutter+column, y+1+min(max(0, row), plotRows-1), glyph, style)
		}
	}
	b.text(x+gutter, y+height-1, "2m ago", bdText)
	b.text(x+width-3, y+height-1, "now", bdText)
}

// renderTitleBackdrop draws every animated element around a box of the given
// size, leaving the box's own rectangle untouched.
func renderTitleBackdrop(width, height, frame int, boxHeight int) *backdrop {
	b := newBackdrop(width, height)
	oy := max(0, (height-boxHeight)/2)

	// Above the box: the chain, vertically centred in whatever room there is.
	above := oy - 1
	switch {
	case above >= chainRows+1:
		drawChain(b, (above-chainRows)/2+1, frame)
	case above >= 2:
		drawChainStrip(b, above/2, frame)
	}

	// Below the box: the charts, anchored to the bottom edge.
	belowTop := oy + boxHeight + 1
	if below := height - belowTop - 1; below >= 2 {
		chartHeight := min(12, below)
		drawCharts(b, height-1-chartHeight, chartHeight, frame)
	}

	return b
}

var titleBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim).Padding(0, 1)

// centerLines centres a block of lines as a unit, so ASCII art keeps its
// shape instead of each line being centred on its own.
func centerLines(lines []string, width int) []string {
	blockWidth := 0
	for _, line := range lines {
		blockWidth = max(blockWidth, lipgloss.Width(line))
	}
	indent := strings.Repeat(" ", max(0, (width-blockWidth)/2))
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = indent + line
	}
	return out
}

func wordmarkLines() []string {
	return strings.Split(strings.Trim(wordmark, "\n"), "\n")
}

func renderPicker(cursor, frame, width, height int) string {
	if width < floorWidth || height < floorHeight {
		return renderTooSmall(width, height)
	}
	boxWidth := min(width-4, max(32, min(78, width-8)))
	// The style's width covers the border and padding on both sides.
	inner := boxWidth - 4
	roomy := width >= 52 && height >= 18

	var lines []string
	if roomy {
		lines = append(lines, "")
		for _, line := range centerLines(wordmarkLines(), inner) {
			lines = append(lines, wordmarkText.Render(line))
		}
	} else {
		lines = append(lines, centerLines([]string{headerText.Render("OMPOOL")}, inner)...)
	}
	tagline := "Bitcoin from the terminal"
	if inner >= lipgloss.Width(tagline)+len(appVersion)+5 {
		tagline += "  ·  " + appVersion
	}
	lines = append(lines, centerLines([]string{labelText.Render(ellipsize(tagline, inner))}, inner)...)
	if height >= 16 {
		lines = append(lines, "")
	}

	// Titles are padded into a column sized to the longest of them, and only
	// while the descriptions still fit beside them; below that each row is
	// just the title.
	titleColumn := 0
	for _, item := range homeModules() {
		titleColumn = max(titleColumn, lipgloss.Width(item.title)+2)
	}
	showDescription := inner >= titleColumn+30
	rows := make([]string, 0, len(homeModules()))
	for i, item := range homeModules() {
		marker := labelText.Render("  ")
		title := item.title
		if i == cursor {
			marker = valueText.Render("▸ ")
			if frame%12 >= 6 {
				marker = headerText.Render("▸ ")
			}
			title = headerText.Render(title)
		} else {
			title = lipgloss.NewStyle().Foreground(bright).Render(title)
		}
		row := marker + title
		if showDescription {
			row = marker + title + strings.Repeat(" ", max(1, titleColumn-lipgloss.Width(item.title))) +
				labelText.Render(ellipsize(item.description, inner-titleColumn-3))
		}
		rows = append(rows, truncate(row, inner))
	}
	lines = append(lines, centerLines(rows, inner)...)

	hint := "↑/↓ navigate  ·  enter open  ·  q quit"
	if inner < lipgloss.Width(hint) {
		hint = "↑/↓  enter  q"
	}
	if height >= 16 {
		lines = append(lines, "")
	}
	lines = append(lines, centerLines([]string{labelText.Render(ellipsize(hint, inner))}, inner)...)

	box := titleBoxStyle.Width(boxWidth).Render(strings.Join(lines, "\n"))
	overlay := strings.Split(box, "\n")
	backdrop := renderTitleBackdrop(width, height, frame, len(overlay))
	return backdrop.render(overlay)
}

// loadingBar is a sweep of light that crosses the bar and back, so the wait
// for the first snapshot visibly moves even before any data arrives.
func loadingBar(frame, width int) string {
	if width < 4 {
		return ""
	}
	const beam = 6
	span := width + beam
	position := frame % (2 * span)
	if position >= span {
		position = 2*span - 1 - position
	}
	position -= beam
	var out strings.Builder
	for cell := range width {
		distance := cell - position
		switch {
		case distance >= 0 && distance < 2:
			out.WriteString(headerText.Render("█"))
		case distance >= 0 && distance < beam:
			out.WriteString(valueText.Render("▓"))
		default:
			out.WriteString(labelText.Render("░"))
		}
	}
	return out.String()
}

func renderLoadingScreen(active module, err error, frame, width, height int) string {
	if width < floorWidth || height < floorHeight {
		return renderTooSmall(width, height)
	}
	l := newLayout(width, height)
	boxWidth := min(l.content, max(34, min(72, l.content)))
	inner := boxWidth - 4

	var lines []string
	if l.content >= 48 && height >= 16 {
		lines = append(lines, "")
		for _, line := range centerLines(wordmarkLines(), inner) {
			lines = append(lines, wordmarkText.Render(line))
		}
	} else {
		lines = append(lines, centerLines([]string{headerText.Render("OMPOOL // LIVE BITCOIN DATA")}, inner)...)
	}
	lines = append(lines, "")
	if err != nil {
		lines = append(lines, centerLines([]string{headerText.Render("CONNECTION DELAYED")}, inner)...)
		lines = append(lines, centerLines([]string{labelText.Render(ellipsize(err.Error(), max(1, inner-2)))}, inner)...)
	} else {
		lines = append(lines, centerLines([]string{headerText.Render("LOADING " + strings.ToUpper(active.title))}, inner)...)
		lines = append(lines, centerLines([]string{labelText.Render("Fast path → fetching chain state in parallel")}, inner)...)
	}
	lines = append(lines, "")
	lines = append(lines, centerLines([]string{loadingBar(frame, min(32, inner-2))}, inner)...)

	box := titleBoxStyle.Width(boxWidth).Render(strings.Join(lines, "\n"))
	overlay := strings.Split(box, "\n")
	backdrop := renderTitleBackdrop(width, height, frame, len(overlay))
	return backdrop.render(overlay)
}
