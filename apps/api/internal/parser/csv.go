package parser

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gofrs/uuid"
)

// CSVParser parses MT5 CSV exports.
//
// Expected header (case-insensitive, flexible):
//
//   Ticket,Symbol,Type,OpenTime,Price,Volume,CloseTime,ClosePrice,Commission,Swap,Profit,Magic,Comment
type CSVParser struct{}

func (CSVParser) Format() Format { return FormatMT5CSV }

func (p CSVParser) Parse(r io.Reader) (*ParseResult, error) {
	rdr := csv.NewReader(r)
	rdr.FieldsPerRecord = -1
	rdr.TrimLeadingSpace = true
	rdr.LazyQuotes = true

	header, err := rdr.Read()
	if err != nil {
		return nil, fmt.Errorf("csv header: %w", err)
	}
	cols := map[string]int{}
	for i, h := range header {
		cols[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := cols["ticket"]; !ok {
		return nil, fmt.Errorf("missing required column: ticket")
	}
	if _, ok := cols["symbol"]; !ok {
		return nil, fmt.Errorf("missing required column: symbol")
	}

	out := &ParseResult{}
	rowNum := 1 // header is row 1
	for {
		rec, err := rdr.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			out.Errors = append(out.Errors, ParseError{Row: rowNum, Message: err.Error()})
			continue
		}
		deal, err := csvRowToDeal(rec, cols, rowNum)
		if err != nil {
			out.Errors = append(out.Errors, ParseError{Row: rowNum, Message: err.Error()})
			continue
		}
		out.Deals = append(out.Deals, deal)
	}
	return out, nil
}

func csvRowToDeal(rec []string, cols map[string]int, rowNum int) (Deal, error) {
	get := func(name string) string {
		if i, ok := cols[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}

	deal := Deal{
		ID:             uuid.Must(uuid.NewV7()),
		ProviderDealID: get("ticket"),
		Symbol:         strings.ToUpper(get("symbol")),
		Type:           strings.ToLower(get("type")),
		EntryType:      strings.ToLower(get("entry")),
		Comment:        get("comment"),
	}

	if deal.ProviderDealID == "" {
		return deal, fmt.Errorf("missing ticket")
	}
	if deal.Symbol == "" {
		return deal, fmt.Errorf("missing symbol")
	}

	// Date — try OpenTime or Time column
	openTime := get("opentime")
	if openTime == "" {
		openTime = get("time")
	}
	if openTime == "" {
		openTime = get("open time")
	}
	t, err := parseMT5Date(openTime)
	if err != nil {
		return deal, fmt.Errorf("open time: %w", err)
	}
	deal.ExecutedAt = t

	if v := get("volume"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return deal, fmt.Errorf("volume: %w", err)
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