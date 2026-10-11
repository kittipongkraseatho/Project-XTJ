package behavior

import (
	"time"

	"github.com/gofrs/uuid"

	domainbehavior "github.com/trading-journal/api/internal/domain/behavior"
)

type overtradingDetector struct{}

func (overtradingDetector) Code() domainbehavior.FindingCode {
	return domainbehavior.FindingOvertradingFrequency
}

func (overtradingDetector) Detect(trades []domainbehavior.InputTrade) domainbehavior.Finding {
	var evidence []domainbehavior.InputTrade
	left := 0
	for right := range trades {
		for trades[right].OpenTime.Sub(trades[left].OpenTime) >= 24*time.Hour {
			left++
		}
		if right-left+1 > 20 {
			evidence = append(evidence, trades[right])
		}
	}
	return finding(domainbehavior.FindingOvertradingFrequency, domainbehavior.SeverityWarning, evidence)
}

type revengeDetector struct{}

func (revengeDetector) Code() domainbehavior.FindingCode {
	return domainbehavior.FindingRevengeAfterLoss
}

func (revengeDetector) Detect(trades []domainbehavior.InputTrade) domainbehavior.Finding {
	var evidence []domainbehavior.InputTrade
	for i := 1; i < len(trades); i++ {
		previous, current := trades[i-1], trades[i]
		if previous.NetPL < 0 && current.OpenTime.Sub(previous.OpenTime) <= 30*time.Minute && current.Volume >= previous.Volume*1.5 {
			evidence = append(evidence, current)
		}
	}
	return finding(domainbehavior.FindingRevengeAfterLoss, domainbehavior.SeverityWarning, evidence)
}

type lossStreakDetector struct{}

func (lossStreakDetector) Code() domainbehavior.FindingCode {
	return domainbehavior.FindingLossStreak
}

func (lossStreakDetector) Detect(trades []domainbehavior.InputTrade) domainbehavior.Finding {
	var evidence []domainbehavior.InputTrade
	streak := 0
	for _, tr := range trades {
		if tr.NetPL < 0 {
			streak++
			if streak >= 3 {
				evidence = append(evidence, tr)
			}
		} else {
			streak = 0
		}
	}
	return finding(domainbehavior.FindingLossStreak, domainbehavior.SeverityWarning, evidence)
}

type riskEscalationDetector struct{}

func (riskEscalationDetector) Code() domainbehavior.FindingCode {
	return domainbehavior.FindingRiskEscalation
}

func (riskEscalationDetector) Detect(trades []domainbehavior.InputTrade) domainbehavior.Finding {
	var evidence []domainbehavior.InputTrade
	streak := 0
	for i, tr := range trades {
		if tr.NetPL < 0 {
			streak++
			continue
		}
		if streak >= 3 && i > 0 && tr.Volume >= trades[i-1].Volume*1.5 {
			evidence = append(evidence, tr)
		}
		streak = 0
	}
	return finding(domainbehavior.FindingRiskEscalation, domainbehavior.SeverityCritical, evidence)
}

type symbolConcentrationDetector struct{}

func (symbolConcentrationDetector) Code() domainbehavior.FindingCode {
	return domainbehavior.FindingSymbolConcentration
}

func (symbolConcentrationDetector) Detect(trades []domainbehavior.InputTrade) domainbehavior.Finding {
	counts := map[string]int{}
	for _, tr := range trades {
		counts[tr.Symbol]++
	}
	var evidence []domainbehavior.InputTrade
	for _, tr := range trades {
		if float64(counts[tr.Symbol])/float64(len(trades)) >= 0.7 {
			evidence = append(evidence, tr)
		}
	}
	return finding(domainbehavior.FindingSymbolConcentration, domainbehavior.SeverityInfo, evidence)
}

func finding(code domainbehavior.FindingCode, severity domainbehavior.FindingSeverity, trades []domainbehavior.InputTrade) domainbehavior.Finding {
	evidenceIDs := make([]uuid.UUID, 0, len(trades))
	evidenceAt := make([]time.Time, 0, len(trades))
	for _, tr := range trades {
		evidenceIDs = append(evidenceIDs, tr.ID)
		evidenceAt = append(evidenceAt, tr.OpenTime.UTC())
	}
	return domainbehavior.Finding{Code: code, Severity: severity, Count: len(trades), EvidenceIDs: evidenceIDs, EvidenceAt: evidenceAt}
}
