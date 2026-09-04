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
	hidden      bool
}

var modules = []module{
	{command: "overview", title: "Overview", description: "The complete Bitcoin network dashboard"},
	{command: "blockchain", title: "Blockchain", description: "An ASCII visualization of the chain", hidden: true},
	{command: "blocks", title: "Recent Blocks", description: "A live feed of newly mined blocks"},
	{command: "transactions", title: "Transactions", description: "Live transactions entering the mempool"},
	{command: "mempool", title: "Mempool", description: "Transaction backlog, weight, and activity"},
	{command: "fees", title: "Fees", description: "Current fee estimates and recent trends", hidden: true},
	{command: "difficulty", title: "Difficulty", description: "Retarget progress and mining cadence", hidden: true},
	{command: "mining", title: "Mining", description: "Hashrate, rewards, and pool distribution"},
	{command: "lightning", title: "Lightning", description: "Lightning Network capacity and activity", hidden: true},
	{command: "explorer", title: "Explorer", description: "Inspect blocks, transactions, and addresses", hidden: true},
}

func homeModules() []module {
	visible := make([]module, 0, len(modules))
	for _, candidate := range modules {
		if !candidate.hidden {
			visible = append(visible, candidate)
		}
	}
	return visible
}

const wordmark = `
   ____  __  ___ ____  ____  ____  __
  / __ \/  |/  // __ \/ __ \/ __ \/ /
 / /_/ / /|_/ // /_/ / /_/ / /_/ / /
/_____/_/  /_// .___/\____/\____/_/
             /_/
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
	txValues    []transactionValueSample
	blockScroll int
	blockPulse  int
	newBlockID  string
	txPulse     int
	newTXIDs    map[string]struct{}
	pendingTX   []mempool.Transaction
	loading     bool
	err         error
	width       int
	height      int
	renderCache *overviewRenderCache
}

type renderFragment struct {
	content string
	valid   bool
}

type overviewRenderCache struct {
	header       renderFragment
	metrics      renderFragment
	blocks       renderFragment
	activity     renderFragment
	difficulty   renderFragment
	transactions renderFragment
	fees         renderFragment

	// blockMaxScroll is written by the block panel once it knows how many rows
	// it can actually show, so key handling can clamp against the real bound.
	blockMaxScroll int
}

func (c *overviewRenderCache) invalidateAll() {
	*c = overviewRenderCache{}
}

func (c *overviewRenderCache) invalidateOverview() {
	c.header.valid = false
	c.metrics.valid = false
	c.blocks.valid = false
	c.difficulty.valid = false
	c.transactions.valid = false
	c.fees.valid = false
}

func (c *overviewRenderCache) invalidateActivity() {
	c.activity.valid = false
}

func cached(fragment *renderFragment, render func() string) string {
	if !fragment.valid {
		fragment.content = render()
		fragment.valid = true
	}
	return fragment.content
}

type activitySample struct {
	at    time.Time
	value float64
}

type transactionValueSample struct {
	at    time.Time
	value float64 // BTC
}

// Keep enough live arrivals for a tall transaction panel to use all of its
// available rows instead of stopping at the API's initial ten-item snapshot.
const recentTransactionLimit = 24

func newModel(command string) model {
	m := model{
		active:      modules[0],
		client:      mempool.NewClient(os.Getenv("OMPOOL_API_URL")),
		renderCache: &overviewRenderCache{},
	}
	if command == "" {
		return m
	}

	for _, candidate := range modules {
		if candidate.command == command {
			m.screen = moduleScreen
			m.active = candidate
			m.loading = true
			m.startLiveStream()
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
type blockPulseMsg struct{}

func (m model) Init() tea.Cmd {
	if m.screen == moduleScreen {
		return moduleDataCommands(m.client, m.live, m.active.command)
	}
	return nil
}

func (m *model) startLiveStream() {
	if m.liveStop != nil {
		m.liveStop()
	}
	m.live = nil
	m.liveStop = nil
	if !moduleNeedsLive(m.active.command) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.live = m.client.StreamStats(ctx)
	m.liveStop = cancel
}

func moduleNeedsLive(command string) bool {
	return command == "overview" || command == "transactions" || command == "mempool"
}

func moduleDataCommands(client *mempool.Client, live <-chan mempool.LiveStats, command string) tea.Cmd {
	fetch := fetchOverview(client, command)
	if live == nil {
		return fetch
	}
	return tea.Batch(fetch, waitForLiveStats(live))
}

func moduleUsesMining(command string) bool {
	return command == "blockchain" || command == "blocks" || command == "transactions" || command == "mining"
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

func fetchOverview(client *mempool.Client, command string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if !moduleUsesMining(command) && !moduleUsesBlockHistory(command) {
			snapshot, err := client.FetchOverview(ctx)
			return overviewMsg{snapshot: snapshot, err: err}
		}
		overviewResult := make(chan overviewMsg, 1)
		miningResult := make(chan mempool.MiningStats, 1)
		go func() {
			snapshot, err := client.FetchOverview(ctx)
			overviewResult <- overviewMsg{snapshot: snapshot, err: err}
		}()
		if moduleUsesMining(command) {
			go func() {
				mining, _ := client.FetchMining(ctx)
				miningResult <- mining
			}()
		} else {
			miningResult <- mempool.MiningStats{}
		}
		result := <-overviewResult
		result.snapshot.Mining = <-miningResult
		if result.err == nil && moduleUsesBlockHistory(command) && len(result.snapshot.Blocks) > 0 {
			history, _ := client.FetchBlockHistory(ctx, result.snapshot.Blocks[0].Height, 2)
			result.snapshot.Blocks = mergeBlocks(result.snapshot.Blocks, history)
		}
		snapshot, err := result.snapshot, result.err
		return overviewMsg{snapshot: snapshot, err: err}
	}
}

func moduleUsesBlockHistory(command string) bool {
	return command == "blockchain" || command == "blocks"
}

func mergeBlocks(groups ...[]mempool.Block) []mempool.Block {
	seen := make(map[string]struct{})
	var merged []mempool.Block
	for _, blocks := range groups {
		for _, block := range blocks {
			key := block.ID
			if key == "" {
				key = fmt.Sprintf("height:%d", block.Height)
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, block)
		}
	}
	return merged
}

func scheduleRefresh() tea.Cmd {
	return tea.Tick(15*time.Second, func(t time.Time) tea.Msg { return refreshMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.renderCache.invalidateAll()
		return m, nil
	case overviewMsg:
		m.loading = false
		m.err = msg.err
		var blockFound bool
		if msg.err == nil {
			blockFound = len(m.overview.Blocks) > 0 && len(msg.snapshot.Blocks) > 0 &&
				m.overview.Blocks[0].ID != "" && msg.snapshot.Blocks[0].ID != m.overview.Blocks[0].ID
			msg.snapshot.Recent = mergeRecentTransactions(msg.snapshot.Recent, m.overview.Recent, recentTransactionLimit)
			m.overview = msg.snapshot
			if blockFound {
				m.newBlockID = msg.snapshot.Blocks[0].ID
				m.blockPulse = 8
			}
		}
		m.renderCache.invalidateOverview()
		if blockFound {
			return m, tea.Batch(scheduleRefresh(), scheduleBlockPulse(), tea.Raw("\a"))
		}
		return m, scheduleRefresh()
	case liveStatsMsg:
		now := time.Now()
		if msg.HasFlow {
			m.activity = append(m.activity, activitySample{at: now, value: msg.VBytesPerSecond})
			m.renderCache.invalidateActivity()
		}
		if len(msg.Transactions) > 0 {
			for i, tx := range msg.Transactions {
				// Preserve ordering within coalesced websocket batches so nearby
				// transactions do not all land on precisely the same timestamp.
				at := now.Add(time.Duration(i-len(msg.Transactions)+1) * time.Millisecond)
				m.txValues = append(m.txValues, transactionValueSample{at: at, value: float64(tx.Value) / 100_000_000})
			}
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
		valueFirst := 0
		for valueFirst < len(m.txValues) && m.txValues[valueFirst].at.Before(cutoff) {
			valueFirst++
		}
		if valueFirst > 0 {
			m.txValues = m.txValues[valueFirst:]
		}
		if m.txPulse > 0 {
			return m, tea.Batch(waitForLiveStats(m.live), scheduleTXPulse())
		}
		return m, waitForLiveStats(m.live)
	case txPulseMsg:
		if m.txPulse > 0 {
			m.txPulse--
			m.renderCache.transactions.valid = false
		}
		if m.txPulse == 0 {
			m.newTXIDs = nil
			m.activateNextTransaction()
		}
		if m.txPulse > 0 {
			return m, scheduleTXPulse()
		}
		return m, nil
	case blockPulseMsg:
		if m.blockPulse > 0 {
			m.blockPulse--
			m.renderCache.blocks.valid = false
		}
		if m.blockPulse == 0 {
			m.newBlockID = ""
			return m, nil
		}
		return m, scheduleBlockPulse()
	case liveStreamClosedMsg:
		return m, nil
	case refreshMsg:
		if m.screen == moduleScreen {
			m.loading = true
			m.renderCache.header.valid = false
			return m, fetchOverview(m.client, m.active.command)
		}
		return m, nil
	case tea.MouseWheelMsg:
		if m.screen == moduleScreen && moduleScrollsBlocks(m.active.command) && m.overBlockStack(msg.X) {
			switch msg.Button {
			case tea.MouseWheelDown:
				m.blockScroll = min(m.maxBlockScroll(), m.blockScroll+3)
				m.renderCache.blocks.valid = false
			case tea.MouseWheelUp:
				m.blockScroll = max(0, m.blockScroll-3)
				m.renderCache.blocks.valid = false
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
			m.renderCache.invalidateAll()
			return m, nil
		}
		return m, tea.Quit
	}

	if m.screen != pickerScreen {
		if moduleScrollsBlocks(m.active.command) {
			switch key.String() {
			case "down", "j":
				m.blockScroll = min(m.maxBlockScroll(), m.blockScroll+3)
				m.renderCache.blocks.valid = false
			case "up", "k":
				m.blockScroll = max(0, m.blockScroll-3)
				m.renderCache.blocks.valid = false
			case "home", "g":
				m.blockScroll = 0
				m.renderCache.blocks.valid = false
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
		if m.cursor < len(homeModules())-1 {
			m.cursor++
		}
	case "enter":
		m.active = homeModules()[m.cursor]
		m.screen = moduleScreen
		m.loading = true
		m.renderCache.invalidateAll()
		m.startLiveStream()
		return m, moduleDataCommands(m.client, m.live, m.active.command)
	}

	return m, nil
}

func moduleScrollsBlocks(command string) bool {
	return command == "overview" || command == "blockchain" || command == "blocks"
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
	m.overview.Recent = mergeRecentTransactions([]mempool.Transaction{tx}, m.overview.Recent, recentTransactionLimit)
	m.newTXIDs = map[string]struct{}{tx.TxID: {}}
	m.txPulse = 5
	m.renderCache.transactions.valid = false
}

func scheduleTXPulse() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return txPulseMsg{} })
}

func scheduleBlockPulse() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return blockPulseMsg{} })
}

func (m model) maxBlockScroll() int {
	// The renderer knows the real viewport height, so it records the bound it
	// clamped to and the model reuses it on the next key press.
	return m.renderCache.blockMaxScroll
}

// overBlockStack reports whether a column belongs to the block stack, the only
// part of the dashboard that scrolls.
func (m model) overBlockStack(x int) bool {
	if m.active.command == "blockchain" || m.active.command == "blocks" {
		return true
	}
	l := newLayout(m.terminalSize())
	return !l.twoColumn || x < l.padX+l.leftWidth
}

func (m model) terminalSize() (int, int) {
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return width, height
}

func (m model) View() tea.View {
	var view tea.View
	width, height := m.terminalSize()
	if m.screen == moduleScreen {
		view = tea.NewView(renderActiveModule(m.renderCache, m.active, m.overview, m.activity, m.txValues, m.loading, m.err, width, height, m.blockScroll, m.txPulse, m.newTXIDs, m.blockPulse, m.newBlockID))
	} else {
		view = tea.NewView(renderPicker(m.cursor, width, height))
	}
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func renderPicker(cursor, width, height int) string {
	if width < floorWidth || height < floorHeight {
		return renderTooSmall(width, height)
	}
	var menu strings.Builder
	// The wordmark is 36 columns of ASCII art; it is the first thing to go when
	// the terminal cannot hold it without wrapping.
	if width >= 52 && height >= 18 {
		menu.WriteString(wordmarkText.Render(wordmark))
	} else {
		menu.WriteString("\n" + headerText.Render("OMPOOL // FAST CHAIN DATA") + "\n")
	}
	tagline := "Bitcoin from the terminal"
	indent := max(0, min(13, (width-lipgloss.Width(tagline))/2))
	menu.WriteString(strings.Repeat(" ", indent) + labelText.Render(tagline) + "\n\n")

	// Titles are padded into a column only while the descriptions still fit
	// beside them; below that each row is just the title.
	const titleColumn = 16
	showDescription := width >= titleColumn+26
	for i, item := range homeModules() {
		marker := "  "
		if i == cursor {
			marker = "> "
		}
		row := marker + item.title
		if showDescription {
			row = fmt.Sprintf("%s%-*s %s", marker, titleColumn, item.title,
				ellipsize(item.description, width-titleColumn-3))
		}
		menu.WriteString(truncate(row, max(1, width-8)) + "\n")
	}

	hint := "↑/↓ navigate  enter open  q quit"
	if width < lipgloss.Width(hint) {
		hint = "↑/↓  enter  q"
	}
	menu.WriteString("\n" + truncate(hint, max(1, width-8)) + "\n")
	boxWidth := min(width-4, max(32, min(78, width-8)))
	box := panelStyle.Width(boxWidth).Render(menu.String())
	return placeCentered(box, width, height)
}

func renderModule(item module, width, height int) string {
	body := lipgloss.NewStyle().Width(max(1, width-4)).Render(item.description +
		"\n\nModule data is coming next.\n\nEsc returns to the module picker. q quits.")
	content := headerText.Render("ompool / "+item.title) + "\n\n" + body
	return lipgloss.NewStyle().Width(width).MaxWidth(width).MaxHeight(height).Padding(1, 2).Render(content)
}

var (
	orange       = lipgloss.Color("#f7931a")
	dim          = lipgloss.Color("#777777")
	panelBorder  = lipgloss.Color("#343434")
	bright       = lipgloss.Color("#f5f5f5")
	headerText   = lipgloss.NewStyle().Bold(true).Foreground(bright)
	labelText    = lipgloss.NewStyle().Foreground(dim)
	valueText    = lipgloss.NewStyle().Bold(true).Foreground(orange)
	wordmarkText = lipgloss.NewStyle().Bold(true).Italic(true).Foreground(orange)
	panelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(panelBorder).Padding(0, 1)
)

// Breakpoints. Every size-dependent decision is made once, in newLayout, so the
// panels themselves never have to guess how much room they were given.
const (
	// Below this the dashboard cannot be drawn legibly at any density.
	floorWidth  = 30
	floorHeight = 6

	// The block stack only moves beside the panel column when both can breathe.
	twoColumnWidth = 96
	// Panel titles keep their explanatory suffix above this.
	subtitleWidth = 74
	// Below this, labels drop to their abbreviated forms.
	shortLabelWidth = 52
)

// layout is the single source of truth for how the dashboard adapts to the
// terminal it has been given.
type layout struct {
	width, height int
	padX, padY    int
	content       int // usable width inside the outer padding

	twoColumn  bool
	columnGap  int
	leftWidth  int
	rightWidth int

	shortLabels bool
	subtitles   bool
	metricLines int
	blockCards  bool
	chartHeight int
	footer      string
}

func newLayout(width, height int) layout {
	l := layout{width: width, height: height, padX: 2, padY: 1, columnGap: 2}
	if width < 72 {
		l.padX = 1
	}
	if height < 20 {
		l.padY = 0
	}
	l.content = max(1, width-2*l.padX)

	l.twoColumn = l.content >= twoColumnWidth
	if l.twoColumn {
		l.leftWidth = min(34, max(28, l.content/3))
		l.rightWidth = l.content - l.columnGap - l.leftWidth
	} else {
		l.leftWidth, l.rightWidth = l.content, l.content
	}

	l.subtitles = l.content >= subtitleWidth
	// The strip of figures may not crowd out the panels it introduces.
	l.metricLines = max(1, height/5)
	l.shortLabels = l.content < shortLabelWidth
	// Bordered block cards only read well in the narrow left column; stretched
	// across a full-width single column they are mostly empty space.
	l.blockCards = l.twoColumn

	// Only heights that divide the chart's ceiling evenly are used, so every
	// row of the axis lands on a round number.
	switch {
	case height < 26:
		l.chartHeight = 5
	case height < 46:
		l.chartHeight = 9
	case height < 56:
		l.chartHeight = 11
	default:
		l.chartHeight = 17
	}

	switch {
	case l.content >= 58:
		l.footer = "Esc modules  ·  q quit  ·  data: mempool.space"
	case l.content >= 30:
		l.footer = "esc modules  ·  q quit"
	default:
		l.footer = "q quit"
	}
	return l
}

// metric is one figure in the row beneath the status header. Both label forms
// are kept so the row can pick whichever fits the terminal.
type metric struct{ label, short, value string }

func (m metric) name(short bool) string {
	if short && m.short != "" {
		return m.short
	}
	return m.label
}

// panel is one bordered block in a vertical stack. Panels with grow set absorb
// whatever rows are left once the fixed ones are placed, and the least
// important panels are dropped when even their minimum will not fit.
// minPanelHeight is a border, a title, one row of content and a border.
const minPanelHeight = 5

type panel struct {
	height   int
	min      int
	priority int
	grow     bool
	render   func(height int) string
}

func panelsHeight(panels []panel) int {
	total := 0
	for _, p := range panels {
		total += p.height
	}
	return total
}

// fitPanels trims a stack to a row budget: growable panels give up rows down to
// the least they can still draw, then whole panels are dropped in ascending
// priority, and any slack left at the end goes back to the first panel that can
// use it. A budget too small for even one panel yields none, because a panel
// with its bottom border cut off reads worse than no panel at all.
func fitPanels(panels []panel, budget int) []panel {
	fitted := append([]panel(nil), panels...)
	for {
		shrinkPanels(fitted, panelsHeight(fitted)-budget, func(p panel) int { return p.min })
		if len(fitted) == 0 || panelsHeight(fitted) <= budget {
			break
		}
		drop := 0
		for i := range fitted {
			if fitted[i].priority < fitted[drop].priority {
				drop = i
			}
		}
		fitted = append(fitted[:drop], fitted[drop+1:]...)
	}

	if slack := budget - panelsHeight(fitted); slack > 0 {
		for i := range fitted {
			if fitted[i].grow {
				fitted[i].height += slack
				break
			}
		}
	}
	return fitted
}

func shrinkPanels(panels []panel, excess int, floor func(panel) int) {
	for i := range panels {
		if excess <= 0 {
			return
		}
		if !panels[i].grow {
			continue
		}
		if give := min(excess, panels[i].height-floor(panels[i])); give > 0 {
			panels[i].height -= give
			excess -= give
		}
	}
}

func renderPanels(panels []panel) string {
	rendered := make([]string, 0, len(panels))
	for _, p := range panels {
		rendered = append(rendered, p.render(p.height))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rendered...)
}

func renderOverview(snapshot mempool.Overview, activity []activitySample, loading bool, err error, width, height, blockScroll, txPulse int, newTXIDs map[string]struct{}) string {
	return renderOverviewCached(&overviewRenderCache{}, snapshot, activity, loading, err, width, height, blockScroll, txPulse, newTXIDs, 0, "")
}

func renderOverviewCached(cache *overviewRenderCache, snapshot mempool.Overview, activity []activitySample, loading bool, err error, width, height, blockScroll, txPulse int, newTXIDs map[string]struct{}, blockPulse int, newBlockID string) string {
	if width < floorWidth || height < floorHeight {
		return renderTooSmall(width, height)
	}
	l := newLayout(width, height)

	header := cached(&cache.header, func() string { return renderStatusHeader(snapshot, loading, l) })
	footer := labelText.Render(l.footer)

	if snapshot.Fetched.IsZero() {
		return renderLoadingScreen(modules[0], err, width, height)
	}

	block := snapshot.Blocks[0]
	metrics := append([]metric{
		{"BLOCK HEIGHT", "HEIGHT", formatInt(block.Height)},
		{"MEMPOOL TXS", "MEMPOOL", formatInt(int64(snapshot.Mempool.Count))},
		{"MEMPOOL SIZE", "SIZE", formatBytes(snapshot.Mempool.VSize, "vB")},
		{"NEXT BLOCK FEE", "NEXT FEE", fmt.Sprintf("%d sat/vB", snapshot.Fees.Fastest)},
		{"BTC PRICE", "BTC/USD", formatUSD(snapshot.Prices.USD)},
	}, networkMetrics(snapshot.Blocks)...)
	metricRow := cached(&cache.metrics, func() string { return renderMetricRow(metrics, l) })

	// Everything between the metric row and the footer belongs to the grid.
	gridHeight := height - l.padY - lipgloss.Height(header) - 1 - lipgloss.Height(metricRow) - 1 - 1
	grid := ""
	if gridHeight > 0 {
		grid = renderGrid(cache, snapshot, activity, l, gridHeight, blockScroll, txPulse, newTXIDs, blockPulse, newBlockID)
	}

	sections := []string{header, metricRow}
	if grid != "" {
		sections = append(sections, grid)
	}
	return renderWithAnchoredFooter(strings.Join(sections, "\n\n"), footer, l)
}

func pickLabel(l layout, long, short string) string {
	if l.shortLabels {
		return short
	}
	return long
}

func renderTooSmall(width, height int) string {
	message := fmt.Sprintf("ompool needs %d×%d", floorWidth, floorHeight)
	if width < lipgloss.Width(message) {
		message = "too small"
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).
		Foreground(dim).Render(truncate(message, width))
}

func renderWithAnchoredFooter(body, footer string, l layout) string {
	// Top padding is retained, but the bottom padding is deliberately zero so
	// the status line occupies the terminal's final row.
	innerHeight := max(1, l.height-l.padY)
	bodyHeight := max(1, innerHeight-max(1, lipgloss.Height(footer)))
	body = lipgloss.NewStyle().MaxWidth(l.content).MaxHeight(bodyHeight).Render(body)
	spacer := max(0, bodyHeight-lipgloss.Height(body))
	content := body + strings.Repeat("\n", spacer) + "\n" + lipgloss.NewStyle().MaxWidth(l.content).Render(footer)
	return lipgloss.NewStyle().Width(l.width).Height(l.height).MaxWidth(l.width).MaxHeight(l.height).
		Padding(l.padY, l.padX, 0, l.padX).Render(content)
}

// renderMetricRow draws the bordered figures under the status header. A normal
// dashboard gets a four-by-two grid; narrower terminals progressively use
// fewer columns while keeping every figure in a card.
func renderMetricRow(metrics []metric, l layout) string {
	columns, short := metricCardLayout(metrics, l.content)
	// A complete card is four rows tall. On unusually short terminals retain
	// the leading metrics that fit instead of clipping borders off the grid.
	maxRows := max(1, l.metricLines/4)
	if limit := columns * maxRows; len(metrics) > limit {
		metrics = metrics[:limit]
	}
	return renderMetricCards(metrics, l.content, columns, short)
}

// packMetrics lays the strip out on as few lines as it can, abbreviating the
// labels whenever that saves a line.
func packMetrics(metrics []metric, width int) []string {
	full := packMetricLabels(metrics, width, false)
	if short := packMetricLabels(metrics, width, true); len(short) < len(full) {
		return short
	}
	return full
}

func packMetricLabels(metrics []metric, width int, short bool) []string {
	parts := make([]string, 0, len(metrics))
	for _, m := range metrics {
		parts = append(parts, labelText.Render(m.name(short))+" "+valueText.Render(m.value))
	}
	return packLine(parts, "  ·  ", width)
}

// metricCardLayout finds the most columns that can hold the metrics legibly,
// capped at four so the primary layout remains a balanced two-row grid.
func metricCardLayout(metrics []metric, width int) (int, bool) {
	for columns := min(4, len(metrics)); columns >= 1; columns-- {
		cardWidth := (width - metricCardGap*(columns-1)) / columns
		for _, short := range []bool{false, true} {
			// Each card spends two columns on its border and two on its padding.
			if cardWidth >= widestMetric(metrics, short)+4 {
				return columns, short
			}
		}
	}
	return 1, true
}

func widestMetric(metrics []metric, short bool) int {
	widest := 1
	for _, m := range metrics {
		widest = max(widest, max(lipgloss.Width(m.name(short)), lipgloss.Width(m.value)))
	}
	return widest
}

const metricCardGap = 1

func renderMetricCards(metrics []metric, width, columns int, short bool) string {
	rows := make([]string, 0, (len(metrics)+columns-1)/columns)
	for start := 0; start < len(metrics); start += columns {
		rowMetrics := metrics[start:min(len(metrics), start+columns)]
		available := width - metricCardGap*(columns-1)
		baseWidth := available / columns
		remainder := available % columns
		cards := make([]string, 0, columns*2-1)
		for i := range columns {
			targetWidth := baseWidth
			if i < remainder {
				targetWidth++
			}
			card := "\n"
			if i < len(rowMetrics) {
				m := rowMetrics[i]
				card = labelText.Render(m.name(short)) + "\n" + valueText.Render(m.value)
			}
			cards = append(cards, panelStyle.Width(max(1, targetWidth)).Render(card))
			if i < columns-1 {
				cards = append(cards, strings.Repeat(" ", metricCardGap))
			}
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// packLine greedily fits pre-styled fragments onto as few lines as it can.
func packLine(parts []string, separator string, width int) []string {
	lines := make([]string, 0, len(parts))
	current := ""
	for _, part := range parts {
		switch {
		case current == "":
			current = part
		case lipgloss.Width(current)+lipgloss.Width(separator)+lipgloss.Width(part) <= width:
			current += separator + part
		default:
			lines = append(lines, current)
			current = part
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func renderStatusHeader(snapshot mempool.Overview, loading bool, l layout) string {
	status := valueText.Render("● LIVE")
	if loading {
		status = labelText.Render("◌ REFRESHING")
	}
	statuses := []string{status}
	if !snapshot.Fetched.IsZero() {
		statuses = []string{
			status + labelText.Render("  "+snapshot.Fetched.UTC().Format("15:04:05 UTC")),
			status + labelText.Render("  "+snapshot.Fetched.UTC().Format("15:04")),
			status,
		}
	}
	brand := headerText.Render("OMPOOL")
	lefts := []string{
		brand + "  " + labelText.Render("BITCOIN MAINNET / OVERVIEW"),
		brand + "  " + labelText.Render("OVERVIEW"),
		brand,
	}

	left, right := widestHeaderPair(lefts, statuses, l.content)
	gap := max(1, l.content-lipgloss.Width(left)-lipgloss.Width(right))
	return truncate(left+strings.Repeat(" ", gap)+right, l.content)
}

// widestHeaderPair picks the fullest pair of header halves that still leaves a
// visible gap between them, falling back to the shortest of each.
func widestHeaderPair(lefts, rights []string, width int) (string, string) {
	for _, left := range lefts {
		for _, right := range rights {
			if lipgloss.Width(left)+2+lipgloss.Width(right) <= width {
				return left, right
			}
		}
	}
	return lefts[len(lefts)-1], rights[len(rights)-1]
}

func renderGrid(cache *overviewRenderCache, snapshot mempool.Overview, activity []activitySample, l layout, height, scroll, txPulse int, newTXIDs map[string]struct{}, blockPulse int, newBlockID string) string {
	blocks := panel{
		min:      minPanelHeight,
		height:   7,
		priority: 90,
		grow:     true,
		render: func(panelHeight int) string {
			return cached(&cache.blocks, func() string {
				return renderBlockStack(cache, snapshot.ProjectedBlocks, snapshot.Blocks, l, l.leftWidth, panelHeight, scroll, blockPulse, newBlockID)
			})
		},
	}

	if height < minPanelHeight {
		return ""
	}
	if l.twoColumn {
		left := blocks
		left.height = height
		right := renderPanelColumn(cache, snapshot, activity, l, l.rightWidth, height, txPulse, newTXIDs, nil)
		return lipgloss.JoinHorizontal(lipgloss.Top, left.render(left.height), strings.Repeat(" ", l.columnGap), right)
	}
	return renderPanelColumn(cache, snapshot, activity, l, l.content, height, txPulse, newTXIDs, &blocks)
}

// renderPanelColumn stacks the data panels into height rows, including the
// block stack at the top when there is no second column to put it in.
func renderPanelColumn(cache *overviewRenderCache, snapshot mempool.Overview, activity []activitySample, l layout, width, height, txPulse int, newTXIDs map[string]struct{}, blocks *panel) string {
	activityPanel := cached(&cache.activity, func() string { return renderActivityPanel(activity, l, width) })
	difficultyPanel := cached(&cache.difficulty, func() string { return renderDifficultyPanel(snapshot.Difficulty, l, width) })
	feePanel := cached(&cache.fees, func() string { return renderFeeMarket(snapshot.Fees, snapshot.Prices.USD, l, width) })

	fixed := func(content string, priority int) panel {
		return panel{height: lipgloss.Height(content), min: lipgloss.Height(content), priority: priority,
			render: func(int) string { return content }}
	}

	panels := make([]panel, 0, 5)
	if blocks != nil {
		panels = append(panels, *blocks)
	}
	panels = append(panels,
		fixed(activityPanel, 72),
		fixed(difficultyPanel, 50),
		fixed(feePanel, 70),
		panel{
			min: minPanelHeight, height: 8, priority: 75, grow: true,
			render: func(panelHeight int) string {
				return cached(&cache.transactions, func() string {
					return renderRecentTransactions(snapshot.Recent, snapshot.Prices.USD, l, width, panelHeight, txPulse, newTXIDs)
				})
			},
		},
	)
	return renderPanels(fitPanels(panels, height))
}

func renderBlockStack(cache *overviewRenderCache, projected []mempool.ProjectedBlock, confirmed []mempool.Block, l layout, width, height, scroll, blockPulse int, newBlockID string) string {
	inner := max(1, width-4)
	visible := max(1, height-3)
	rowsPerBlock := 1
	if l.blockCards {
		visible--
		rowsPerBlock = 5
	}

	// mempool.space projects up to eight blocks. Handing them the whole panel
	// would push the chain tip out of view, so they get at most half of it and
	// the title says how many were held back.
	forecast := len(projected)
	if fit := max(1, visible/2/rowsPerBlock); forecast > fit {
		projected = projected[:fit]
	}

	var lines []string
	if l.blockCards {
		lines = blockCardLines(projected, confirmed, width, blockPulse, newBlockID)
	} else {
		lines = blockRowLines(projected, confirmed, width, blockPulse, newBlockID)
	}

	maxScroll := max(0, len(lines)-visible)
	cache.blockMaxScroll = maxScroll

	title := headerText.Render("MEMPOOL BLOCKS")
	if len(projected) < forecast {
		title = appendIfItFits(title, labelText.Render(fmt.Sprintf("%d of %d projected", len(projected), forecast)), inner)
	}
	if maxScroll > 0 {
		title = appendIfItFits(title, labelText.Render("↑/↓ scroll"), inner)
	}

	scroll = min(max(0, scroll), maxScroll)
	viewport := strings.Join(lines[scroll:min(len(lines), scroll+visible)], "\n")
	separator := "\n"
	if l.blockCards {
		separator = "\n\n"
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(title + separator + viewport)
}

// appendIfItFits adds a trailing detail to a title only when the width is there
// for it, so narrow panels shed their hints instead of wrapping.
func appendIfItFits(base, extra string, width int) string {
	if lipgloss.Width(base)+2+lipgloss.Width(extra) > width {
		return base
	}
	return base + "  " + extra
}

func blockCardLines(projected []mempool.ProjectedBlock, confirmed []mempool.Block, width, blockPulse int, newBlockID string) []string {
	cardWidth := max(1, width-4)
	const cardHeight = 5
	items := make([]string, 0, len(projected)+len(confirmed)+1)
	for _, b := range projected {
		body := valueText.Render(fmt.Sprintf("%.1f sat/vB", b.MedianFee)) + "\n" +
			labelText.Render("median fee") + "\n\n" +
			headerText.Render(formatBytes(int64(b.BlockVSize), "vB")) + "  " +
			labelText.Render(formatInt(int64(b.TxCount))+" tx")
		items = append(items, panelStyle.Width(cardWidth).Height(cardHeight).BorderForeground(dim).Render(body))
	}
	items = append(items, confirmedDivider(cardWidth))
	for _, b := range confirmed {
		heightLine := valueText.Render("#" + formatInt(b.Height))
		borderColor := orange
		if marker, hex, pulsing := blockPulseMarker(b.ID, blockPulse, newBlockID); pulsing {
			borderColor = lipgloss.Color(hex)
			heightLine += "  " + lipgloss.NewStyle().Bold(true).Foreground(borderColor).Render(marker)
		}
		body := heightLine + "\n" +
			headerText.Render(poolName(b)) + "\n" +
			labelText.Render(age(b.Timestamp)) + "\n" +
			headerText.Render(formatInt(int64(b.TxCount))+" tx") + "  " +
			labelText.Render(formatBytes(b.Size, "B"))
		items = append(items, panelStyle.Width(cardWidth).Height(cardHeight).BorderForeground(borderColor).Render(body))
	}
	return strings.Split(lipgloss.JoinVertical(lipgloss.Left, items...), "\n")
}

// blockRowLines is the single-column form of the stack: one line per block, so
// a short terminal still shows the shape of the chain rather than two cards.
// Columns are dropped from the right as the panel narrows.
func blockRowLines(projected []mempool.ProjectedBlock, confirmed []mempool.Block, width, blockPulse int, newBlockID string) []string {
	inner := max(1, width-4)
	lines := make([]string, 0, len(projected)+len(confirmed)+1)
	for _, b := range projected {
		right := ""
		if inner >= 24 {
			right = labelText.Render(padLeft(formatBytes(int64(b.BlockVSize), "vB"), 9))
		}
		if inner >= 34 {
			right += "  " + labelText.Render(padLeft(formatInt(int64(b.TxCount))+" tx", 9))
		}
		lines = append(lines, justify(valueText.Render(fmt.Sprintf("~%.1f sat/vB", b.MedianFee)), right, inner))
	}
	lines = append(lines, confirmedDivider(inner))
	for _, b := range confirmed {
		heightCell := "#" + formatInt(b.Height)
		style := valueText
		if marker, hex, pulsing := blockPulseMarker(b.ID, blockPulse, newBlockID); pulsing {
			style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hex))
			heightCell = string([]rune(marker)[:1]) + " " + heightCell
		}
		right := ""
		if inner >= 30 {
			right = labelText.Render(padLeft(age(b.Timestamp), 9))
		}
		// 10 for the height, 2 to separate it from the pool, 1 for the gap that
		// justify keeps before the right-hand columns.
		poolWidth := inner - lipgloss.Width(right) - 13
		if poolWidth >= 23 {
			right = labelText.Render(padLeft(formatInt(int64(b.TxCount))+" tx", 9)) + "  " + right
			poolWidth -= 11
		}
		left := style.Render(pad(heightCell, 10))
		if poolWidth >= 12 {
			left += "  " + headerText.Render(ellipsize(poolName(b), poolWidth))
		}
		lines = append(lines, justify(left, right, inner))
	}
	return lines
}

// justify pins right to the far edge of width, keeping at least one space
// between the two halves.
func justify(left, right string, width int) string {
	if right == "" {
		return truncate(left, width)
	}
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return truncate(left+strings.Repeat(" ", gap)+right, width)
}

func poolName(b mempool.Block) string {
	if b.Extras.Pool.Name == "" {
		return "Unknown pool"
	}
	return b.Extras.Pool.Name
}

// blockPulseMarker reports the animation frame for a freshly confirmed block:
// its badge text and the hex colour both the badge and the card border use.
func blockPulseMarker(id string, blockPulse int, newBlockID string) (string, string, bool) {
	if blockPulse <= 0 || id == "" || id != newBlockID {
		return "", "", false
	}
	pulseColors := []string{"#5a3a16", "#8a511a", "#bd6d18", "#f7931a"}
	marker := "◇ NEW BLOCK"
	if blockPulse%2 == 0 {
		marker = "◆ NEW BLOCK"
	}
	return marker, pulseColors[min(len(pulseColors)-1, (blockPulse-1)/2)], true
}

func confirmedDivider(width int) string {
	const label = " CONFIRMED "
	rule := max(2, width-len(label))
	return labelText.Render(strings.Repeat("─", rule/2) + label + strings.Repeat("─", rule-rule/2))
}

func renderDifficultyPanel(difficulty mempool.DifficultyAdjustment, l layout, width int) string {
	inner := max(1, width-4)
	barWidth := max(4, inner-2)
	filled := int(math.Round(math.Max(0, math.Min(100, difficulty.ProgressPercent)) / 100 * float64(barWidth)))
	bar := valueText.Render(strings.Repeat("█", filled)) + labelText.Render(strings.Repeat("░", barWidth-filled))
	summary := packLine([]string{
		valueText.Render(fmt.Sprintf("%.1f%%", difficulty.ProgressPercent)) + labelText.Render(" through epoch"),
		labelText.Render(fmt.Sprintf("estimate %+.2f%%", difficulty.DifficultyChange)),
		labelText.Render(formatInt(int64(difficulty.RemainingBlocks)) + " blocks left"),
	}, labelText.Render("  ·  "), inner)
	title := headerText.Render(pickLabel(l, "DIFFICULTY ADJUSTMENT", "DIFFICULTY"))
	return panelStyle.Width(width).Render(title + "\n" + bar + "\n" + strings.Join(summary, "\n"))
}

func renderActivityPanel(activity []activitySample, l layout, width int) string {
	chartWidth := max(8, width-4)
	latest := 0.0
	if len(activity) > 0 {
		latest = activity[len(activity)-1].value
	}
	title := headerText.Render(pickLabel(l, "INCOMING TRANSACTION FLOW", "TX FLOW")) + "  " +
		valueText.Render(fmt.Sprintf("%.0f vB/s", latest))
	if l.subtitles {
		title += "  " + labelText.Render("2 min / 15 sec")
	}
	chart := renderActivityChart(activity, chartWidth, l.chartHeight)
	return panelStyle.Width(width).Render(truncate(title, chartWidth) + "\n" + chart)
}

func renderFeeMarket(fees mempool.Fees, usdPrice float64, l layout, width int) string {
	const typicalVSize = 140.0
	tiers := []struct {
		long, short string
		rate        float64
	}{
		{"NEXT BLOCK", "NEXT", float64(fees.Fastest)},
		{"30 MIN", "30M", float64(fees.HalfHour)},
		{"1 HOUR", "1H", float64(fees.Hour)},
		{"ECONOMY", "ECON", float64(fees.Economy)},
		{"MINIMUM", "MIN", fees.Minimum},
	}
	stats := make([]stat, 0, len(tiers))
	for _, tier := range tiers {
		rate := fmt.Sprintf("%.1f", tier.rate)
		if tier.rate == math.Trunc(tier.rate) {
			rate = fmt.Sprintf("%.0f", tier.rate)
		}
		// The dollar estimate is the first thing to go: the sat/vB rate is what
		// the panel is actually for.
		full := rate
		if usdPrice > 0 {
			full += " · " + fmt.Sprintf("~$%.2f", tier.rate*typicalVSize/100_000_000*usdPrice)
		}
		stats = append(stats, stat{label: tier.long, short: tier.short, value: full, terse: rate})
	}
	title := headerText.Render("FEE MARKET")
	if l.subtitles {
		title += "  " + labelText.Render("sat/vB / USD for 140 vB")
	}
	return renderStatColumns(title, stats, width)
}

// networkMetrics derives the chain-wide figures that sit in the metric strip
// beside the mempool ones. The live flow rate is deliberately absent: the flow
// chart already prints it in its own title.
func networkMetrics(blocks []mempool.Block) []metric {
	lastBlock, interval := "waiting", "waiting"
	if len(blocks) > 0 {
		lastBlock = age(blocks[0].Timestamp)
	}
	if len(blocks) > 1 {
		if span := blocks[0].Timestamp - blocks[len(blocks)-1].Timestamp; span > 0 {
			average := time.Duration(span/int64(len(blocks)-1)) * time.Second
			interval = fmt.Sprintf("%dm %02ds", int(average.Minutes()), int(average.Seconds())%60)
		}
	}
	confirmed := int64(0)
	for _, block := range blocks {
		confirmed += int64(block.TxCount)
	}
	return []metric{
		{"LAST BLOCK", "LAST BLOCK", lastBlock},
		{"AVG INTERVAL", "INTERVAL", interval},
		{fmt.Sprintf("TX / %d BLOCKS", len(blocks)), fmt.Sprintf("TX / %d BLK", len(blocks)), formatInt(confirmed)},
	}
}

// stat is one label/value pair in a columned panel. The narrower forms are used
// automatically when the panel cannot fit the full ones on a single row.
type stat struct {
	label, short string
	value, terse string
}

func (s stat) cell(shortLabel, terseValue bool) (string, string) {
	label, value := s.label, s.value
	if shortLabel && s.short != "" {
		label = s.short
	}
	if terseValue && s.terse != "" {
		value = s.terse
	}
	return label, value
}

// renderStatColumns lays label/value pairs out in evenly spaced columns. It
// abbreviates before it wraps, wraps onto a second row before it drops, and
// drops the tail of the list rather than growing past two rows.
func renderStatColumns(title string, stats []stat, width int) string {
	inner := max(1, width-4)
	if len(stats) == 0 {
		return panelStyle.Width(width).Render(truncate(title, inner))
	}

	shortLabel, terseValue := false, false
	for _, form := range [][2]bool{{false, false}, {false, true}, {true, true}} {
		shortLabel, terseValue = form[0], form[1]
		if inner/len(stats) >= widestStat(stats, shortLabel, terseValue)+2 {
			break
		}
	}

	columns := len(stats)
	for columns > 1 && inner/columns < widestStat(stats, shortLabel, terseValue)+2 {
		columns--
	}
	if len(stats) > columns*2 {
		stats = stats[:columns*2]
	}

	rows := make([]string, 0, 4)
	for start := 0; start < len(stats); start += columns {
		var labels, values strings.Builder
		for i, s := range stats[start:min(len(stats), start+columns)] {
			cellWidth := inner / columns
			if i == columns-1 {
				cellWidth = inner - (inner/columns)*(columns-1)
			}
			label, value := s.cell(shortLabel, terseValue)
			labels.WriteString(pad(label, cellWidth))
			values.WriteString(pad(value, cellWidth))
		}
		rows = append(rows, labelText.Render(labels.String()), valueText.Render(values.String()))
	}
	return panelStyle.Width(width).Render(truncate(title, inner) + "\n" + strings.Join(rows, "\n"))
}

func widestStat(stats []stat, shortLabel, terseValue bool) int {
	widest := 1
	for _, s := range stats {
		label, value := s.cell(shortLabel, terseValue)
		widest = max(widest, max(lipgloss.Width(label), lipgloss.Width(value)))
	}
	return widest
}

// transactionColumn is one column of the latest-transactions table. Columns are
// added left to right for as long as the panel has room for them.
type transactionColumn struct {
	title  string
	share  int // percentage of the available width
	needs  int // panel inner width required before this column appears
	render func(tx mempool.Transaction, usdPrice float64) string
}

var transactionColumns = []transactionColumn{
	{title: "TXID", share: 30, needs: 0},
	{title: "USD", share: 22, needs: 26, render: func(tx mempool.Transaction, usdPrice float64) string {
		return formatUSD(float64(tx.Value) / 100_000_000 * usdPrice)
	}},
	{title: "BTC", share: 24, needs: 62, render: func(tx mempool.Transaction, _ float64) string {
		return fmt.Sprintf("%.8f", float64(tx.Value)/100_000_000)
	}},
	{title: "FEE", share: 24, needs: 40, render: func(tx mempool.Transaction, _ float64) string {
		feeRate := 0.0
		if tx.VSize > 0 {
			feeRate = float64(tx.Fee) / tx.VSize
		}
		return fmt.Sprintf("%d (%.1f/vB)", tx.Fee, feeRate)
	}},
}

func renderRecentTransactions(transactions []mempool.Transaction, usdPrice float64, l layout, width, height, txPulse int, newTXIDs map[string]struct{}) string {
	inner := max(1, width-4)
	title := headerText.Render(pickLabel(l, "LATEST TRANSACTIONS", "LATEST TX"))
	if l.subtitles {
		title += "  " + labelText.Render("live mempool arrivals")
	}
	// border + title + column header
	rows := max(1, height-4)

	if len(transactions) == 0 {
		return panelStyle.Width(width).Height(max(1, height-2)).Render(truncate(title, inner) + "\n\n" + labelText.Render("Waiting for transactions…"))
	}

	columns := make([]transactionColumn, 0, len(transactionColumns))
	for _, column := range transactionColumns {
		if inner >= column.needs {
			columns = append(columns, column)
		}
	}

	available := inner - (len(columns) - 1)
	widths := make([]int, len(columns))
	assigned := 0
	for i, column := range columns[1:] {
		widths[i+1] = available * column.share / 100
		assigned += widths[i+1]
	}
	widths[0] = available - assigned

	header := make([]string, 0, len(columns))
	for i, column := range columns {
		if i == 0 {
			header = append(header, pad(column.title, widths[i]))
			continue
		}
		header = append(header, padLeft(column.title, widths[i]))
	}

	lines := []string{truncate(title, inner), labelText.Render(strings.Join(header, " "))}
	for _, tx := range transactions[:min(rows, len(transactions))] {
		cells := make([]string, 0, len(columns))
		for i, column := range columns {
			if i == 0 {
				cells = append(cells, pad(shortenTXIDTo(tx.TxID, widths[i]), widths[i]))
				continue
			}
			cells = append(cells, padLeft(column.render(tx, usdPrice), widths[i]))
		}
		lines = append(lines, styleTransactionRow(strings.Join(cells, " "), tx.TxID, txPulse, newTXIDs, inner))
	}
	return panelStyle.Width(width).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
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

func shortenTXIDTo(txid string, width int) string {
	if width <= 0 {
		return ""
	}
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

// activityCeiling is the flow rate the chart's top row represents. Chart
// heights are chosen so this divides evenly by height-1, which puts every row
// of the y axis on a round number.
const activityCeiling = 8000.0

func renderActivityChart(samples []activitySample, width, height int) string {
	// Every row is labelled, and the gutter is sized to the widest of those
	// labels so the plot never loses a column it did not have to.
	compact := width < 52
	labels := make([]string, height)
	widest := 1
	for row := range height {
		labels[row] = formatAxisValue(axisValueAt(row, height), compact)
		widest = max(widest, len(labels[row]))
	}
	gutter := widest + 2
	plotColumns := max(4, width-gutter)

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
		var line strings.Builder
		fmt.Fprintf(&line, "%*s │", widest, labels[row])
		for x := range plotColumns {
			value := values[x]
			barDots := int(math.Ceil(math.Max(0, value) / activityCeiling * float64(logicalHeight)))
			dotsBelow := (height - 1 - row) * 4
			cellDots := min(4, max(0, barDots-dotsBelow))
			if value >= 0 && cellDots > 0 {
				level := activityCeiling * float64(dotsBelow+cellDots) / float64(logicalHeight)
				line.WriteString(activityColor(level).Render(brailleFill[cellDots]))
			} else {
				line.WriteString(" ")
			}
		}
		rows[row] = line.String()
	}
	rows = append(rows, strings.Repeat(" ", gutter-1)+"└"+strings.Repeat("─", plotColumns))
	if axis := renderTimeAxis(start, end, plotColumns, gutter); axis != "" {
		rows = append(rows, axis)
	}
	return strings.Join(rows, "\n")
}

func axisValueAt(row, height int) float64 {
	return activityCeiling * float64(height-1-row) / float64(max(1, height-1))
}

// formatAxisValue keeps the y axis narrow enough to leave the plot its columns.
func formatAxisValue(value float64, compact bool) string {
	if !compact || value < 1000 {
		return fmt.Sprintf("%.0f", value)
	}
	if math.Mod(value, 1000) == 0 {
		return fmt.Sprintf("%.0fk", value/1000)
	}
	return fmt.Sprintf("%.1fk", value/1000)
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

// renderTimeAxis spaces its ticks so the labels never collide, and returns an
// empty string once even the sparsest spacing would not fit.
func renderTimeAxis(start, end time.Time, width, gutter int) string {
	const labelWidth = 3
	for _, step := range []time.Duration{15 * time.Second, 30 * time.Second, time.Minute} {
		ticks := int(end.Sub(start) / step)
		if ticks == 0 {
			continue
		}
		if width/ticks < labelWidth+2 {
			continue
		}
		line := make([]rune, width+gutter)
		for i := range line {
			line[i] = ' '
		}
		for tick := start.Truncate(step).Add(step); !tick.After(end); tick = tick.Add(step) {
			x := gutter + int(tick.Sub(start).Seconds()/end.Sub(start).Seconds()*float64(width-1))
			for i, char := range tick.Format(":05") {
				if x+i < len(line) {
					line[x+i] = char
				}
			}
		}
		return labelText.Render(string(line))
	}
	return ""
}

// pad and padLeft size a cell by display width, so styled or multi-byte
// content lines up the same way plain ASCII does.
func pad(value string, width int) string {
	value = truncate(value, width)
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func padLeft(value string, width int) string {
	value = truncate(value, width)
	return strings.Repeat(" ", max(0, width-lipgloss.Width(value))) + value
}

// ellipsize trims to width the way a reader expects, marking the cut.
func ellipsize(value string, width int) string {
	if width <= 1 || lipgloss.Width(value) <= width {
		return truncate(value, width)
	}
	return truncate(value, width-1) + "…"
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(value)
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
