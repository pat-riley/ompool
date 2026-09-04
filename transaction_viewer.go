package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ompool/internal/mempool"
)

const (
	lenSassamanTXID = "930a2114cdaa86e1fac46d15c74e81c09eee1d4150ff9d48e76cb0697d8e1d72"
	genesisTXID     = "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"
)

var transactionIDPattern = regexp.MustCompile(`(?i)[0-9a-f]{64}`)
var viewerANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")

type transactionViewerState struct {
	query        string
	loaded       string
	loading      bool
	err          error
	inspection   *mempool.TransactionInspection
	requestID    int
	pane         int // wide: 0 inputs, 1 outputs, 2 data; compact: 0 UTXOs, 1 data
	utxoScroll   int
	inputScroll  int
	outputScroll int
	dataScroll   int
}

type transactionInspectionMsg struct {
	requestID  int
	inspection mempool.TransactionInspection
	err        error
}

func normalizeTransactionID(value string) (string, error) {
	match := transactionIDPattern.FindString(strings.TrimSpace(value))
	if match == "" {
		return "", fmt.Errorf("enter a 64-character transaction ID or paste a transaction URL")
	}
	return strings.ToLower(match), nil
}

func (m *model) inspectTransaction(value string) tea.Cmd {
	txid, err := normalizeTransactionID(value)
	if err != nil {
		m.viewer.err = err
		m.viewer.loading = false
		return nil
	}
	m.viewer.query = txid
	m.viewer.loaded = txid
	m.viewer.loading = true
	m.viewer.err = nil
	m.viewer.inspection = nil
	m.viewer.utxoScroll = 0
	m.viewer.inputScroll = 0
	m.viewer.outputScroll = 0
	m.viewer.dataScroll = 0
	m.viewer.requestID++
	requestID := m.viewer.requestID
	return func() tea.Msg {
		inspection, err := m.client.FetchTransaction(context.Background(), txid)
		return transactionInspectionMsg{requestID: requestID, inspection: inspection, err: err}
	}
}

func (m *model) loadTransactionPreset(txid string) tea.Cmd {
	m.viewer.query = txid
	return m.inspectTransaction(txid)
}

func appendViewerInput(current, addition string) string {
	addition = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return -1
		}
		return r
	}, addition)
	if len(current)+len(addition) > 512 {
		addition = addition[:max(0, 512-len(current))]
	}
	return current + addition
}

func removeLastRune(value string) string {
	if value == "" {
		return value
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
}

func (m *model) moveViewerScroll(delta int) {
	width, _ := m.terminalSize()
	l := newLayout(width, 1)
	if viewerUsesThreeColumns(l) {
		switch m.viewer.pane {
		case 0:
			m.viewer.inputScroll = min(m.viewerScrollLimit(0), max(0, m.viewer.inputScroll+delta))
		case 1:
			m.viewer.outputScroll = min(m.viewerScrollLimit(1), max(0, m.viewer.outputScroll+delta))
		default:
			m.viewer.dataScroll = min(m.viewerScrollLimit(2), max(0, m.viewer.dataScroll+delta))
		}
		return
	}
	if m.viewer.pane == 0 {
		m.viewer.utxoScroll = min(m.viewerScrollLimit(0), max(0, m.viewer.utxoScroll+delta))
		return
	}
	m.viewer.dataScroll = min(m.viewerScrollLimit(1), max(0, m.viewer.dataScroll+delta))
}

func (m model) viewerScrollLimit(pane int) int {
	if m.viewer.inspection == nil {
		return 0
	}
	width, height := m.terminalSize()
	l := newLayout(width, height)
	inputHeight := lipgloss.Height(renderViewerInput(m.viewer, l.content))
	paneHeight := max(0, height-l.padY-1-2-inputHeight-1-1)
	if paneHeight >= 12 {
		metrics := viewerMetrics(*m.viewer.inspection)
		paneHeight -= lipgloss.Height(renderMetricCards(metrics, l.content, min(4, metricCardColumns(metrics, l.content)), true))
	}
	if paneHeight < minPanelHeight {
		return 0
	}
	paneWidth := l.content
	if viewerUsesThreeColumns(l) {
		inputWidth, outputWidth, dataWidth := viewerColumnWidths(l.content, l.columnGap)
		switch pane {
		case 0:
			paneWidth = inputWidth
		case 1:
			paneWidth = outputWidth
		default:
			paneWidth = dataWidth
		}
	} else if l.twoColumn {
		leftWidth := max(38, l.content*43/100)
		paneWidth = leftWidth
		if pane == 1 {
			paneWidth = l.content - l.columnGap - leftWidth
		}
	}
	var lines []string
	if viewerUsesThreeColumns(l) && pane == 0 {
		lines = viewerInputLines(*m.viewer.inspection)
	} else if viewerUsesThreeColumns(l) && pane == 1 {
		lines = viewerOutputLines(*m.viewer.inspection)
	} else if pane == 0 {
		lines = viewerUTXOLines(*m.viewer.inspection)
	} else {
		lines = viewerDataLines(*m.viewer.inspection, max(20, paneWidth-4))
	}
	wrapped := 0
	for _, line := range lines {
		wrapped += len(wrapStyledLine(line, max(1, paneWidth-4)))
	}
	return max(0, wrapped-max(1, paneHeight-3))
}

func viewerUsesThreeColumns(l layout) bool { return l.content >= 112 }

func viewerColumnWidths(content, gap int) (int, int, int) {
	available := max(3, content-2*gap)
	columnWidth := max(1, available/3)
	return columnWidth, columnWidth, columnWidth
}

func renderTransactionViewer(state transactionViewerState, width, height int) string {
	if width < floorWidth || height < floorHeight {
		return renderTooSmall(width, height)
	}
	l := newLayout(width, height)
	header := renderViewerHeader(state, l.content)
	footerText := "Esc modules  ·  Enter inspect  ·  Shift+L Len Sassaman  ·  Shift+G Genesis  ·  Tab pane  ·  ↑/↓ scroll"
	if l.content < 84 {
		footerText = "esc back · enter inspect · ⇧L/⇧G presets · tab pane"
	}
	footer := labelText.Render(truncate(footerText, l.content))
	input := renderViewerInput(state, l.content)
	bodyHeight := max(0, height-l.padY-lipgloss.Height(header)-2-lipgloss.Height(input)-1-1)
	body := renderViewerBody(state, l, bodyHeight)
	content := header + "\n\n" + input
	if body != "" {
		content += "\n" + body
	}
	return renderWithAnchoredFooter(content, footer, l)
}

func renderViewerHeader(state transactionViewerState, width int) string {
	left := headerText.Render("OMPOOL") + labelText.Render("  / ") + headerText.Render("TRANSACTION VIEWER")
	status := labelText.Render("AWAITING TXID")
	switch {
	case state.loading:
		status = valueText.Render("◌ FETCHING")
	case state.err != nil:
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff3b30")).Render("● LOOKUP ERROR")
	case state.inspection != nil && state.inspection.Transaction.Status.Confirmed:
		status = valueText.Render("● CONFIRMED")
	case state.inspection != nil:
		status = valueText.Render("● MEMPOOL")
	}
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(status))
	return truncate(left+strings.Repeat(" ", gap)+status, width)
}

func renderViewerInput(state transactionViewerState, width int) string {
	inner := max(1, width-4)
	query := state.query
	if query == "" {
		query = labelText.Render("paste a transaction ID or mempool.space URL")
	} else {
		query = headerText.Render(ellipsize(query, max(1, inner-2)))
	}
	cursor := valueText.Render("█")
	line := truncate(query+cursor, inner)
	presets := valueText.Render("SHIFT+L") + labelText.Render(" LEN SASSAMAN PORTRAIT") + "    " +
		valueText.Render("SHIFT+G") + labelText.Render(" GENESIS / THE TIMES")
	return panelStyle.BorderForeground(orange).Width(width).Render(
		headerText.Render("TRANSACTION ID") + "\n" + line + "\n" + truncate(presets, inner),
	)
}

func renderViewerBody(state transactionViewerState, l layout, height int) string {
	if height < minPanelHeight {
		return ""
	}
	if state.loading {
		message := valueText.Render("◌ RESOLVING TRANSACTION") + "\n" + labelText.Render("decoded data · raw hex · output spend status")
		return panelStyle.Width(l.content).Height(max(1, height-2)).Render(placeCentered(message, max(1, l.content-4), max(1, height-2)))
	}
	if state.err != nil {
		message := headerText.Render("LOOKUP FAILED") + "\n" + labelText.Render(ellipsize(state.err.Error(), max(1, l.content-4)))
		return panelStyle.BorderForeground(lipgloss.Color("#ff3b30")).Width(l.content).Height(max(1, height-2)).Render(message)
	}
	if state.inspection == nil {
		message := headerText.Render("INSPECT ANY BITCOIN TRANSACTION") + "\n\n" +
			labelText.Render("Paste a txid above, or open one of the two on-chain artifacts with Shift+L / Shift+G.")
		return panelStyle.Width(l.content).Height(max(1, height-2)).Render(message)
	}

	metrics := viewerMetrics(*state.inspection)
	metricCards := ""
	if height >= 12 {
		metricCards = renderMetricCards(metrics, l.content, min(4, metricCardColumns(metrics, l.content)), true)
		height -= lipgloss.Height(metricCards)
	}
	if height < minPanelHeight {
		return metricCards
	}

	utxos := viewerUTXOLines(*state.inspection)
	panes := ""
	if viewerUsesThreeColumns(l) {
		inputWidth, outputWidth, dataWidth := viewerColumnWidths(l.content, l.columnGap)
		inputs := renderViewerPane(fmt.Sprintf("INPUT UTXOs  ·  %d", len(state.inspection.Transaction.Vin)), viewerInputLines(*state.inspection), inputWidth, height, state.inputScroll, state.pane == 0)
		outputs := renderViewerPane(fmt.Sprintf("OUTPUT UTXOs  ·  %d", len(state.inspection.Transaction.Vout)), viewerOutputLines(*state.inspection), outputWidth, height, state.outputScroll, state.pane == 1)
		data := viewerDataLines(*state.inspection, max(20, dataWidth-4))
		onChain := renderViewerPane("ON-CHAIN DATA / ASCII + HEX", data, dataWidth, height, state.dataScroll, state.pane == 2)
		panes = lipgloss.JoinHorizontal(lipgloss.Top, inputs, strings.Repeat(" ", l.columnGap), outputs, strings.Repeat(" ", l.columnGap), onChain)
	} else if l.twoColumn {
		leftWidth := max(38, l.content*43/100)
		rightWidth := l.content - l.columnGap - leftWidth
		data := viewerDataLines(*state.inspection, max(20, rightWidth-4))
		left := renderViewerPane("INPUTS / OUTPUT UTXOs", utxos, leftWidth, height, state.utxoScroll, state.pane == 0)
		right := renderViewerPane("ON-CHAIN DATA / ASCII + HEX", data, rightWidth, height, state.dataScroll, state.pane == 1)
		panes = lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", l.columnGap), right)
	} else if state.pane == 0 {
		panes = renderViewerPane("INPUTS / OUTPUT UTXOs  ·  TAB → DATA", utxos, l.content, height, state.utxoScroll, true)
	} else {
		data := viewerDataLines(*state.inspection, max(20, l.content-4))
		panes = renderViewerPane("ON-CHAIN DATA / ASCII + HEX  ·  TAB → UTXOs", data, l.content, height, state.dataScroll, true)
	}
	if metricCards == "" {
		return panes
	}
	return lipgloss.JoinVertical(lipgloss.Left, metricCards, panes)
}

func viewerMetrics(inspection mempool.TransactionInspection) []metric {
	tx := inspection.Transaction
	totalOut := int64(0)
	for _, output := range tx.Vout {
		totalOut += output.Value
	}
	virtualSize := float64(tx.Weight) / 4
	status := "MEMPOOL"
	if tx.Status.Confirmed {
		status = "BLOCK #" + formatInt(tx.Status.BlockHeight)
	}
	return []metric{
		{"TOTAL OUTPUT", "OUTPUT", formatBTC(float64(totalOut))},
		{"NETWORK FEE", "FEE", formatBTC(float64(tx.Fee))},
		{"FEE RATE", "RATE", fmt.Sprintf("%.2f sat/vB", float64(tx.Fee)/max(1, virtualSize))},
		{"STATUS", "STATUS", status},
	}
}

func viewerUTXOLines(inspection mempool.TransactionInspection) []string {
	inputs := viewerInputLines(inspection)
	outputs := viewerOutputLines(inspection)
	lines := []string{headerText.Render(fmt.Sprintf("INPUT UTXOs  %d", len(inspection.Transaction.Vin)))}
	lines = append(lines, inputs...)
	lines = append(lines, "", headerText.Render(fmt.Sprintf("OUTPUT UTXOs  %d", len(inspection.Transaction.Vout))))
	return append(lines, outputs...)
}

func viewerInputLines(inspection mempool.TransactionInspection) []string {
	tx := inspection.Transaction
	lines := make([]string, 0, len(tx.Vin)*3)
	for index, input := range tx.Vin {
		if index > 0 {
			lines = append(lines, "")
		}
		if input.IsCoinbase {
			lines = append(lines, headerText.Render("[coinbase]"), valueText.Render("  -> creates BTC"))
			continue
		}
		value, address := int64(0), "unknown address"
		if input.Prevout != nil {
			value, address = input.Prevout.Value, input.Prevout.ScriptPubKeyAddress
			if address == "" {
				address = input.Prevout.ScriptPubKeyType
			}
		}
		if address == "" {
			address = "[unknown input]"
		}
		lines = append(lines,
			headerText.Render(address),
			valueText.Render("  -> "+formatBTC(float64(value))),
		)
	}
	return lines
}

func viewerOutputLines(inspection mempool.TransactionInspection) []string {
	tx := inspection.Transaction
	lines := make([]string, 0, len(tx.Vout)*3+2)
	for index, output := range tx.Vout {
		if index > 0 {
			lines = append(lines, "")
		}
		address := output.ScriptPubKeyAddress
		if address == "" {
			address = output.ScriptPubKeyType
			if address == "" {
				address = "unknown output"
			}
			address = "[" + address + "]"
		}
		lines = append(lines,
			valueText.Render(formatBTC(float64(output.Value))+" ->"),
			headerText.Render("  "+address),
		)
	}
	if tx.TxID == genesisTXID {
		lines = append(lines, "", labelText.Render("Note: the genesis reward is historically unspendable and is not part of Bitcoin Core's spendable UTXO set."))
	}
	return lines
}

func renderViewerPane(title string, lines []string, width, height, scroll int, active bool) string {
	innerWidth := max(1, width-4)
	viewport := max(1, height-3)
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			wrapped = append(wrapped, "")
			continue
		}
		wrapped = append(wrapped, wrapStyledLine(line, innerWidth)...)
	}
	maxScroll := max(0, len(wrapped)-viewport)
	scroll = min(max(0, scroll), maxScroll)
	visible := wrapped[scroll:min(len(wrapped), scroll+viewport)]
	caption := headerText.Render(title)
	if maxScroll > 0 {
		caption = appendIfItFits(caption, labelText.Render(fmt.Sprintf(" %d/%d", scroll+1, maxScroll+1)), innerWidth)
	}
	content := caption
	if len(visible) > 0 {
		content += "\n" + strings.Join(visible, "\n")
	}
	style := panelStyle
	if active {
		style = style.BorderForeground(orange)
	}
	return style.Width(width).Height(max(1, height-2)).Render(content)
}

func wrapStyledLine(value string, width int) []string {
	if lipgloss.Width(value) <= width {
		return []string{value}
	}
	plainValue := viewerANSI.ReplaceAllString(value, "")
	var lines []string
	for len(plainValue) > 0 {
		chunk := truncate(plainValue, width)
		lines = append(lines, chunk)
		plainValue = plainValue[len(chunk):]
	}
	return lines
}

type embeddedPayload struct {
	source string
	data   []byte
}

func viewerDataLines(inspection mempool.TransactionInspection, width int) []string {
	payloads := transactionPayloads(inspection)
	lines := []string{}
	if len(payloads) == 0 {
		lines = append(lines, labelText.Render("No printable pushed payload detected."), "", headerText.Render("RAW TRANSACTION HEX"))
		raw, _ := hex.DecodeString(inspection.RawHex)
		return append(lines, hexDumpLines(raw, width)...)
	}
	total := 0
	for _, payload := range payloads {
		total += len(payload.data)
	}
	lines = append(lines, headerText.Render(fmt.Sprintf("DECODED ASCII  ·  %s", formatBytes(int64(total), "B"))))
	for _, payload := range payloads {
		text := string(payload.data)
		if len(text) <= width {
			lines = append(lines, valueText.Render(text))
			continue
		}
		for len(text) > 0 {
			cut := min(len(text), width)
			lines = append(lines, valueText.Render(text[:cut]))
			text = text[cut:]
		}
	}
	lines = append(lines, "", headerText.Render("EMBEDDED PAYLOAD HEX"))
	combined := make([]byte, 0, total)
	for _, payload := range payloads {
		combined = append(combined, payload.data...)
	}
	return append(lines, hexDumpLines(combined, width)...)
}

func transactionPayloads(inspection mempool.TransactionInspection) []embeddedPayload {
	var payloads []embeddedPayload
	for index, input := range inspection.Transaction.Vin {
		payloads = appendPrintablePushes(payloads, fmt.Sprintf("in:%d", index), input.ScriptSig)
		for witnessIndex, witness := range input.Witness {
			payloads = appendPrintableHex(payloads, fmt.Sprintf("witness:%d:%d", index, witnessIndex), witness)
		}
	}
	for index, output := range inspection.Transaction.Vout {
		payloads = appendPrintablePushes(payloads, fmt.Sprintf("out:%d", index), output.ScriptPubKey)
	}
	if len(payloads) > 0 {
		return payloads
	}
	raw, err := hex.DecodeString(inspection.RawHex)
	if err != nil {
		return nil
	}
	start := -1
	for index, b := range raw {
		if isPrintableASCII(b) {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 && index-start >= 4 {
			payloads = append(payloads, embeddedPayload{"raw", append([]byte(nil), raw[start:index]...)})
		}
		start = -1
	}
	if start >= 0 && len(raw)-start >= 4 {
		payloads = append(payloads, embeddedPayload{"raw", append([]byte(nil), raw[start:]...)})
	}
	return payloads
}

func appendPrintablePushes(payloads []embeddedPayload, source, scriptHex string) []embeddedPayload {
	script, err := hex.DecodeString(scriptHex)
	if err != nil {
		return payloads
	}
	for _, pushed := range scriptPushes(script) {
		if printablePayload(pushed) {
			payloads = append(payloads, embeddedPayload{source, append([]byte(nil), pushed...)})
		}
	}
	return payloads
}

func appendPrintableHex(payloads []embeddedPayload, source, value string) []embeddedPayload {
	decoded, err := hex.DecodeString(value)
	if err == nil && printablePayload(decoded) {
		return append(payloads, embeddedPayload{source, decoded})
	}
	return payloads
}

func scriptPushes(script []byte) [][]byte {
	var pushes [][]byte
	for cursor := 0; cursor < len(script); {
		opcode := int(script[cursor])
		cursor++
		length := 0
		switch {
		case opcode >= 1 && opcode <= 75:
			length = opcode
		case opcode == 76 && cursor < len(script):
			length = int(script[cursor])
			cursor++
		case opcode == 77 && cursor+1 < len(script):
			length = int(script[cursor]) | int(script[cursor+1])<<8
			cursor += 2
		case opcode == 78 && cursor+3 < len(script):
			length = int(script[cursor]) | int(script[cursor+1])<<8 | int(script[cursor+2])<<16 | int(script[cursor+3])<<24
			cursor += 4
		default:
			continue
		}
		if length < 0 || cursor+length > len(script) {
			break
		}
		pushes = append(pushes, script[cursor:cursor+length])
		cursor += length
	}
	return pushes
}

func printablePayload(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	printable := 0
	for _, b := range data {
		if isPrintableASCII(b) {
			printable++
		}
	}
	return float64(printable)/float64(len(data)) >= 0.9
}

func isPrintableASCII(b byte) bool { return b >= 0x20 && b <= 0x7e }

func hexDumpLines(data []byte, width int) []string {
	if len(data) == 0 {
		return []string{labelText.Render("none")}
	}
	bytesPerLine := 16
	if width < 58 {
		bytesPerLine = 8
	}
	lines := make([]string, 0, (len(data)+bytesPerLine-1)/bytesPerLine)
	for offset := 0; offset < len(data); offset += bytesPerLine {
		end := min(len(data), offset+bytesPerLine)
		line := fmt.Sprintf("%06x  %s", offset, strings.ToUpper(hex.EncodeToString(data[offset:end])))
		lines = append(lines, labelText.Render(line))
	}
	return lines
}
