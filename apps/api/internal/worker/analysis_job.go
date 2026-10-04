package worker

import (
	"context"
	"fmt"

	"github.com/gofrs/uuid"
)

// AnalysisJobPayload is the data the analysis worker needs to run.
// It invokes the service's BuildAnalysis(analysisID) — the worker
// itself is generic.
type AnalysisJobPayload struct {
	AnalysisID uuid.UUID
	Handler    func(ctx context.Context) error
}

// Kind satisfies Job.Kind so the pool can log/dedupe.
func (p *AnalysisJobPayload) Kind() string {
	return "analysis:" + p.AnalysisID.String()
}

// Execute satisfies Job.Execute.
func (p *AnalysisJobPayload) Execute(ctx context.Context) error {
	if p.Handler == nil {
		return fmt.Errorf("analysis job handler is nil")
	}
	return p.Handler(ctx)
}