package pdfreport

import (
	"context"
	"fmt"
	"time"
)

// StubService returns a minimal valid PDF — used in tests and when the
// signintech implementation is gated off. The byte slice is a hand-crafted
// one-page PDF that says "Trading Journal — PDF generation not configured".
type StubService struct{}

// NewStubService returns a Service that emits a placeholder PDF.
func NewStubService() Service { return &StubService{} }

// minimalPDF is a valid one-page PDF — enough to pass "starts with %PDF-,
// ends with %%EOF" checks while telling the caller to configure the feature.
const minimalPDF = `%PDF-1.4
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 12 0 R >> >> >> endobj
4 0 obj << /Length 95 >> stream
BT /F1 18 Tf 72 720 Td (Trading Journal — PDF generation not configured) Tj 0 -24 Td /F1 10 Tf (See internal/integration/pdfreport) Tj ET
endstream endobj
12 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj
xref
0 13
0000000000 65535 f
0000000010 00000 n
0000000053 00000 n
0000000100 00000 n
0000000203 00000 n
0000000350 00000 n
trailer << /Size 13 /Root 1 0 R >>
startxref
420
%%EOF`

// Generate returns the placeholder PDF with a small audit-trail prefix
// (timestamp + tenant id, embedded as a comment-equivalent so it's visible
// in hex dumps without polluting the rendered text).
func (s *StubService) Generate(_ context.Context, req ReportRequest) ([]byte, error) {
	header := fmt.Sprintf("%% Trading Journal stub PDF — tenant=%s account=%s period=%s..%s generated_at=%s\n",
		req.TenantID, req.Account.ID,
		req.Period.From.Format("2006-01-02"), req.Period.To.Format("2006-01-02"),
		time.Now().UTC().Format(time.RFC3339))
	return append([]byte(header), []byte(minimalPDF)...), nil
}