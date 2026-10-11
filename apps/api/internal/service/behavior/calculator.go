package behavior

import (
	"fmt"
	"sort"
	"time"

	"github.com/gofrs/uuid"

	domainbehavior "github.com/trading-journal/api/internal/domain/behavior"
)

const algorithmVersion = "r2-v1"

type Detector interface {
	Code() domainbehavior.FindingCode
	Detect([]domainbehavior.InputTrade) domainbehavior.Finding
}

type Calculator struct {
	detectors []Detector
}

func NewCalculator() *Calculator {
	return &Calculator{detectors: []Detector{
		overtradingDetector{},
		revengeDetector{},
		lossStreakDetector{},
		riskEscalationDetector{},
		symbolConcentrationDetector{},
	}}
}

func (c *Calculator) Calculate(accountID uuid.UUID, from, to time.Time, input []domainbehavior.InputTrade) domainbehavior.Profile {
	trades := make([]domainbehavior.InputTrade, 0, len(input))
	for _, tr := range input {
		if !tr.OpenTime.Before(from) && tr.OpenTime.Before(to) {
			trades = append(trades, tr)
		}
	}
	sort.SliceStable(trades, func(i, j int) bool {
		if trades[i].OpenTime.Equal(trades[j].OpenTime) {
			return trades[i].ID.String() < trades[j].ID.String()
		}
		return trades[i].OpenTime.Before(trades[j].OpenTime)
	})

	profile := domainbehavior.Profile{
		TradingAccountID: accountID,
		WindowStart:      from.UTC(),
		WindowEnd:        to.UTC(),
		AlgorithmVersion: algorithmVersion,
		Status:           domainbehavior.ProfileStatusInsufficientData,
		SampleSize:       len(trades),
		Metrics: domainbehavior.Metrics{
			SymbolShare:    map[string]float64{},
			DirectionShare: map[string]float64{},
			SessionShare:   map[string]float64{},
			MagicShare:     map[string]float64{},
		},
		Findings: []domainbehavior.Finding{},
	}
	if len(trades) < 10 {
		return profile
	}

	profile.Status = domainbehavior.ProfileStatusReady
	profile.Metrics = computeMetrics(from, to, trades)
	for _, detector := range c.detectors {
		finding := detector.Detect(trades)
		if finding.Count > 0 {
			profile.Findings = append(profile.Findings, finding)
		}
	}
	sort.Slice(profile.Findings, func(i, j int) bool { return profile.Findings[i].Code < profile.Findings[j].Code })
	return profile
}

func computeMetrics(from, to time.Time, trades []domainbehavior.InputTrade) domainbehavior.Metrics {
	metrics := domainbehavior.Metrics{
		SymbolShare:    map[string]float64{},
		DirectionShare: map[string]float64{},
		SessionShare:   map[string]float64{},
		MagicShare:     map[string]float64{},
	}
	var wins, losses int
	var grossWin, grossLoss, totalPL, totalHold float64
	var held int
	for _, tr := range trades {
		if tr.NetPL > 0 {
			wins++
			grossWin += tr.NetPL
		} else if tr.NetPL < 0 {
			losses++
			grossLoss += -tr.NetPL
		}
		totalPL += tr.NetPL
		if tr.CloseTime != nil {
			totalHold += tr.CloseTime.Sub(tr.OpenTime).Seconds()
			held++
		}
		metrics.SymbolShare[tr.Symbol]++
		metrics.DirectionShare[tr.Direction]++
		metrics.SessionShare[sessionFor(tr.OpenTime)]++
		magic := "none"
		if tr.Magic != nil {
			magic = fmt.Sprintf("%d", *tr.Magic)
		}
		metrics.MagicShare[magic]++
	}
	count := float64(len(trades))
	for _, shares := range []map[string]float64{metrics.SymbolShare, metrics.DirectionShare, metrics.SessionShare, metrics.MagicShare} {
		for key := range shares {
			shares[key] /= count
		}
	}
	days := to.Sub(from).Hours() / 24
	if days <= 0 {
		days = 1
	}
	metrics.WinRate = float64(wins) / count
	metrics.NetPL = totalPL
	metrics.Expectancy = totalPL / count
	if wins > 0 {
		metrics.AverageWin = grossWin / float64(wins)
	}
	if losses > 0 {
		metrics.AverageLoss = grossLoss / float64(losses)
		metrics.ProfitFactor = grossWin / grossLoss
	}
	if held > 0 {
		metrics.AverageHoldSecs = totalHold / float64(held)
	}
	metrics.TradesPerDay = count / days
	return metrics
}

func sessionFor(t time.Time) string {
	hour := t.UTC().Hour()
	switch {
	case hour < 8:
		return "asia"
	case hour < 13:
		return "london"
	case hour < 21:
		return "new_york"
	default:
		return "off_hours"
	}
}
