package parser

import (
	"strings"
	"testing"
	"time"

	"github.com/gofrs/uuid"
)

// keep references not used by other tests pinned
var _ = uuid.Nil

func TestDetect_HTML(t *testing.T) {
	content := []byte(`<!DOCTYPE html><html><body><table id="deals"><tr><th>Ticket</th></tr></table></body></html>`)
	if got := Detect(content); got != FormatMT5HTML {
		t.Errorf("Detect = %v, want mt5_html", got)
	}
}

func TestDetect_CSV(t *testing.T) {
	content := []byte(`Ticket,Symbol,OpenTime,Volume,Profit
12345,XAUUSD,2024.01.15 10:30:00,0.10,84.20`)
	if got := Detect(content); got != FormatMT5CSV {
		t.Errorf("Detect = %v, want mt5_csv", got)
	}
}

func TestDetect_Unknown(t *testing.T) {
	content := []byte(`random binary blob with no headers here at all`)
	if got := Detect(content); got != FormatUnknown {
		t.Errorf("Detect = %v, want unknown", got)
	}
}

func TestMT5HTMLParser_SimpleTable(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
<h1>Deals</h1>
<table id="deals">
  <thead><tr><th>Ticket</th><th>Open Time</th><th>Type</th><th>Volume</th><th>Symbol</th><th>Price</th><th>Commission</th><th>Swap</th><th>Profit</th></tr></thead>
  <tbody>
    <tr><td>1</td><td>2024.01.15 10:30:00</td><td>buy</td><td>0.10</td><td>XAUUSD</td><td>2651.20</td><td>0</td><td>0</td><td>0</td></tr>
    <tr><td>2</td><td>2024.01.15 11:45:00</td><td>sell</td><td>0.10</td><td>XAUUSD</td><td>2659.62</td><td>-0.50</td><td>0</td><td>84.20</td></tr>
  </tbody>
</table>
</body></html>`
	res, err := MT5HTMLParser{}.Parse(strings.NewReader(html))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Deals) != 2 {
		t.Fatalf("expected 2 deals, got %d", len(res.Deals))
	}
	if res.Deals[0].Symbol != "XAUUSD" {
		t.Errorf("deal 0 symbol = %q", res.Deals[0].Symbol)
	}
	if res.Deals[0].Type != "buy" {
		t.Errorf("deal 0 type = %q", res.Deals[0].Type)
	}
	if res.Deals[1].Profit != 84.20 {
		t.Errorf("deal 1 profit = %v", res.Deals[1].Profit)
	}
}

func TestCSVParser_Simple(t *testing.T) {
	csv := `Ticket,Symbol,OpenTime,Type,Volume,Price,Commission,Swap,Profit
1,XAUUSD,2024.01.15 10:30:00,buy,0.10,2651.20,0,0,0
2,XAUUSD,2024.01.15 11:45:00,sell,0.10,2659.62,-0.50,0,84.20`
	res, err := CSVParser{}.Parse(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Deals) != 2 {
		t.Fatalf("expected 2 deals, got %d", len(res.Deals))
	}
	if res.Deals[0].EntryType != "" {
		t.Errorf("default entry_type = %q", res.Deals[0].EntryType)
	}
}

func TestAggregate_PairEntryExit(t *testing.T) {
	deals := []Deal{
		{ProviderDealID: "1", Symbol: "XAUUSD", Type: "buy", EntryType: "entry",
			Volume: 0.10, Price: 2651.20, ExecutedAt: mustTime("2024-01-15T10:30:00Z")},
		{ProviderDealID: "2", Symbol: "XAUUSD", Type: "sell", EntryType: "exit",
			Volume: 0.10, Price: 2659.62, Commission: -0.50, Profit: 84.20,
			ExecutedAt: mustTime("2024-01-15T11:45:00Z")},
	}
	trades := Aggregate(deals)
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].NetPL != 83.70 {
		t.Errorf("net p/l = %v (expected 84.20 - 0.50 = 83.70)", trades[0].NetPL)
	}
	if trades[0].Outcome != "win" {
		t.Errorf("outcome = %q", trades[0].Outcome)
	}
}

func TestAggregate_PartialClose(t *testing.T) {
	deals := []Deal{
		{ProviderDealID: "1", Symbol: "XAUUSD", Type: "buy", EntryType: "entry",
			Volume: 1.0, Price: 100, ExecutedAt: mustTime("2024-01-15T10:00:00Z")},
		{ProviderDealID: "2", Symbol: "XAUUSD", Type: "sell", EntryType: "exit",
			Volume: 0.5, Price: 110, Profit: 5.0,
			ExecutedAt: mustTime("2024-01-15T11:00:00Z")},
	}
	trades := Aggregate(deals)
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].NetPL != 5.0 {
		t.Errorf("net p/l = %v", trades[0].NetPL)
	}
}

func TestAggregate_OrphanExit(t *testing.T) {
	deals := []Deal{
		{ProviderDealID: "1", Symbol: "XAUUSD", Type: "sell", EntryType: "exit",
			Volume: 0.1, Price: 110, Profit: 5.0,
			ExecutedAt: mustTime("2024-01-15T11:00:00Z")},
	}
	trades := Aggregate(deals)
	if len(trades) != 0 {
		t.Errorf("orphan exit should be ignored, got %d trades", len(trades))
	}
}

func mustTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}