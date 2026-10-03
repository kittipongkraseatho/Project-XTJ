package parser

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"golang.org/x/net/html"

	"github.com/trading-journal/api/internal/observability"
)

// MT5HTMLParser parses the MetaQuotes MT5 history export.
//
// Document shape (simplified):
//
//   <html>
//     <body>
//       <table id="deals">     ← OR the first table under <h1>Deals</h1>
//         <thead><tr><th>Ticket</th><th>Open Time</th>...</tr></thead>
//         <tbody>
//           <tr><td>12345</td><td>2024.01.15 10:30:00</td>...</tr>
//           ...
//         </tbody>
//       </table>
//     </body>
//   </html>
//
// We use header-driven column mapping so any column order works.
// Date formats supported: "2006.01.02 15:04:05" and "2006-01-02 15:04:05".
type MT5HTMLParser struct{}

func (MT5HTMLParser) Format() Format { return FormatMT5HTML }

var mt5DateLayouts = []string{
	"2006.01.02 15:04:05",
	"2006-01-02 15:04:05",
	"01/02/2006 15:04:05",
	"2006.01.02",
	"2006-01-02",
}

func parseMT5Date(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range mt5DateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date %q", s)
}

func (p MT5HTMLParser) Parse(r io.Reader) (*ParseResult, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	table := findDealsTable(doc)
	if table == nil {
		return nil, observability.ErrValidation.WithDetails(map[string]any{"reason": "no deals table found"})
	}

	headerRow := findFirstChild(table, "thead")
	var headerCells []string
	if headerRow != nil {
		tr := findFirstChild(headerRow, "tr")
		if tr != nil {
			headerCells = extractCells(tr)
		}
	}
	if len(headerCells) == 0 {
		// Fallback: use first tbody row as header
		tbody := findFirstChild(table, "tbody")
		if tbody != nil {
			tr := findFirstChild(tbody, "tr")
			if tr != nil {
				headerCells = extractCells(tr)
			}
		}
	}
	if len(headerCells) == 0 {
		return nil, observability.ErrValidation.WithDetails(map[string]any{"reason": "no header row"})
	}
	colIdx := map[string]int{}
	for i, h := range headerCells {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}

	rows := collectDataRows(table)
	out := &ParseResult{}
	for rowNum, row := range rows {
		cells := extractCells(row)
		deal, err := rowToDeal(cells, colIdx, rowNum)
		if err != nil {
			out.Errors = append(out.Errors, ParseError{Row: rowNum, Message: err.Error()})
			continue
		}
		out.Deals = append(out.Deals, deal)
	}
	return out, nil
}

func rowToDeal(cells []string, cols map[string]int, rowNum int) (Deal, error) {
	get := func(name string) string {
		if i, ok := cols[name]; ok && i < len(cells) {
			return strings.TrimSpace(cells[i])
		}
		return ""
	}

	deal := Deal{
		ID:             uuid.Must(uuid.NewV7()),
		ProviderDealID: get("ticket"),
		Symbol:         strings.ToUpper(get("symbol")),
		Type:           strings.ToLower(get("type")),
		EntryType:      strings.ToLower(get("entry")), // entry | exit | inout
		Comment:        get("comment"),
	}

	if deal.ProviderDealID == "" {
		return deal, fmt.Errorf("row %d: missing ticket", rowNum)
	}
	if deal.Symbol == "" {
		return deal, fmt.Errorf("row %d: missing symbol", rowNum)
	}

	openTime := get("open time")
	if openTime == "" {
		openTime = get("opentime")
	}
	if openTime == "" {
		openTime = get("time")
	}
	t, err := parseMT5Date(openTime)
	if err != nil {
		return deal, fmt.Errorf("row %d: open time: %w", rowNum, err)
	}
	deal.ExecutedAt = t

	if v := get("volume"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return deal, fmt.Errorf("row %d: volume: %w", rowNum, err)
		}
		deal.Volume = f
	}
	if v := get("price"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		deal.Price = f
	}
	if v := get("commission"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		deal.Commission = f
	}
	if v := get("swap"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		deal.Swap = f
	}
	if v := get("profit"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		deal.Profit = f
	}
	if v := get("magic"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			deal.Magic = &n
		}
	}

	return deal, nil
}

// ── HTML tree helpers ─────────────────────────────────────────────────

func findDealsTable(n *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "table" {
			// Look for id="deals" or caption containing "Deals"
			for _, a := range n.Attr {
				if a.Key == "id" && strings.Contains(strings.ToLower(a.Val), "deal") {
					found = n
					return
				}
			}
			if found == nil {
				caption := findFirstChild(n, "caption")
				if caption != nil && strings.Contains(strings.ToLower(textOf(caption)), "deal") {
					found = n
					return
				}
			}
			if found == nil {
				found = n // first table as fallback
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if found != nil && (n.Data == "table") {
				return
			}
		}
	}
	walk(n)
	return found
}

func findFirstChild(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return c
		}
	}
	return nil
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

func extractCells(tr *html.Node) []string {
	var out []string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			out = append(out, textOf(c))
		}
	}
	return out
}

func collectDataRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	tbody := findFirstChild(table, "tbody")
	if tbody != nil {
		for c := tbody.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "tr" {
				rows = append(rows, c)
			}
		}
		return rows
	}
	for c := table.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "tr" {
			rows = append(rows, c)
		}
	}
	return rows
}

// SanitizeUTF8 trims BOM and invalid sequences that MetaQuotes sometimes
// appends to HTML files.
func SanitizeUTF8(b []byte) []byte {
	if len(b) >= 3 && bytes.Equal(b[:3], []byte{0xEF, 0xBB, 0xBF}) {
		b = b[3:]
	}
	return b
}