package telemetry

import (
	"encoding/json"
	"fmt"
	"sort"
)

// usageAccumulator folds snapshot rows into one delivery's totals, its
// per-model breakdown and its per-session breakdown.
//
// It exists because two separate places used to walk the same join and
// add up the same columns - TotalsByDelivery for a delivery's detail and
// the projection's batch query for the list - so a fix to one silently
// left the other reporting different numbers for the same delivery. Both
// now fold through this.
type usageAccumulator struct {
	counters        UsageTotals
	telemetryStatus string

	costKnownAny   bool
	costFullyKnown bool
	costTotal      float64
	currency       string

	unpricedSeen   map[string]bool
	unpricedModels []string

	models     map[string]*ModelUsageTotals
	modelOrder []string

	sessions     map[string]*SessionUsageTotals
	sessionOrder []string
}

func newUsageAccumulator() *usageAccumulator {
	return &usageAccumulator{
		telemetryStatus: "complete",
		costFullyKnown:  true,
		unpricedSeen:    map[string]bool{},
		models:          map[string]*ModelUsageTotals{},
		sessions:        map[string]*SessionUsageTotals{},
	}
}

// snapshotRow is one row of the sessions-to-snapshots join. A session with
// no snapshot yet still produces a row, with zeroed counters and no JSON -
// its telemetry status still counts.
type snapshotRow struct {
	SessionID         string
	ExternalSessionID string
	ClientKind        string
	Participant       string
	TelemetryStatus   string
	Status            string
	StartedAt         string
	StoppedAt         string

	InputTokens      int64
	OutputTokens     int64
	CacheWriteTokens int64
	CacheReadTokens  int64
	ToolCalls        int64
	ElapsedMS        int64

	ModelUsageJSON string
	PricingJSON    string
	CostJSON       string
}

func (a *usageAccumulator) add(row snapshotRow) error {
	if row.TelemetryStatus != "complete" {
		a.telemetryStatus = "incomplete"
	}
	a.counters.InputTokens += row.InputTokens
	a.counters.OutputTokens += row.OutputTokens
	a.counters.CacheWriteTokens += row.CacheWriteTokens
	a.counters.CacheReadTokens += row.CacheReadTokens
	a.counters.ToolCalls += row.ToolCalls
	a.counters.ElapsedMS += row.ElapsedMS

	rates, err := a.foldPricing(row.PricingJSON)
	if err != nil {
		return err
	}
	modelCost, modelCurrency, err := a.foldModels(row.ModelUsageJSON, rates)
	if err != nil {
		return err
	}
	sessionCost, sessionCurrency, sessionPriced, err := a.foldCost(row.CostJSON)
	if err != nil {
		return err
	}
	// A session's own share is the cost its snapshots were priced at when
	// they were captured; the per-model figures are recomputed from the
	// rates stored alongside them, and fall back to that same recorded
	// total's currency when a rate named none.
	if sessionCurrency == "" {
		sessionCurrency = modelCurrency
	}
	if sessionCost == 0 && !sessionPriced {
		sessionCost = modelCost
	}
	a.foldSession(row, sessionCost, sessionCurrency, sessionPriced)
	return nil
}

// foldPricing records every model this row could not price and returns the
// rates it could, keyed by model.
func (a *usageAccumulator) foldPricing(pricingJSON string) (map[string]ModelRate, error) {
	if pricingJSON == "" {
		return nil, nil
	}
	var entries []pricingEntry
	if err := json.Unmarshal([]byte(pricingJSON), &entries); err != nil {
		return nil, fmt.Errorf("telemetry: decode pricing: %w", err)
	}
	rates := make(map[string]ModelRate, len(entries))
	for _, entry := range entries {
		if entry.Rate != nil {
			rates[entry.Model] = *entry.Rate
		}
		// "Cost unknown" on its own gives a reader nothing to act on; the
		// model name is the whole lead - it says which catalog entry is
		// missing.
		if entry.Known || a.unpricedSeen[entry.Model] {
			continue
		}
		a.unpricedSeen[entry.Model] = true
		a.unpricedModels = append(a.unpricedModels, entry.Model)
	}
	return rates, nil
}

func (a *usageAccumulator) foldModels(modelUsageJSON string, rates map[string]ModelRate) (float64, string, error) {
	if modelUsageJSON == "" {
		return 0, "", nil
	}
	var usage []modelUsageEntry
	if err := json.Unmarshal([]byte(modelUsageJSON), &usage); err != nil {
		return 0, "", fmt.Errorf("telemetry: decode model usage: %w", err)
	}
	var total float64
	currency := ""
	for _, mu := range usage {
		entry, ok := a.models[mu.Model]
		if !ok {
			entry = &ModelUsageTotals{Model: mu.Model, Priced: true}
			a.models[mu.Model] = entry
			a.modelOrder = append(a.modelOrder, mu.Model)
		}
		entry.InputTokens += mu.InputTokens
		entry.OutputTokens += mu.OutputTokens
		entry.CacheWriteTokens += mu.CacheWriteTokens
		entry.CacheReadTokens += mu.CacheReadTokens

		// A pseudo-model no provider bills for is priced at nothing rather
		// than unpriced; treating it as unknown would take the whole
		// delivery's cost down with it.
		if NonBillableModel(mu.Model) {
			continue
		}
		rate, ok := rates[mu.Model]
		if !ok {
			entry.Priced = false
			continue
		}
		cost := costForModelUsage(mu, rate)
		entry.EstimatedCost += cost
		entry.Currency = rate.Currency
		total += cost
		if currency == "" {
			currency = rate.Currency
		}
	}
	return total, currency, nil
}

func (a *usageAccumulator) foldCost(costJSON string) (float64, string, bool, error) {
	if costJSON == "" {
		return 0, "", false, nil
	}
	var cost estimatedCost
	if err := json.Unmarshal([]byte(costJSON), &cost); err != nil {
		return 0, "", false, fmt.Errorf("telemetry: decode estimated cost: %w", err)
	}
	if !cost.Known {
		a.costFullyKnown = false
		return 0, "", false, nil
	}
	a.costKnownAny = true
	// A snapshot whose only usage was non-billable is known to have cost
	// nothing and names no currency. It must not claim the delivery's
	// currency slot, or the first real priced snapshot after it reads as a
	// currency mismatch.
	if cost.Currency == "" && cost.Amount == 0 {
		return 0, "", true, nil
	}
	if a.currency == "" {
		a.currency = cost.Currency
	} else if a.currency != cost.Currency {
		a.costFullyKnown = false
	}
	a.costTotal += cost.Amount
	return cost.Amount, cost.Currency, true, nil
}

func (a *usageAccumulator) foldSession(row snapshotRow, cost float64, currency string, priced bool) {
	if row.SessionID == "" {
		return
	}
	entry, ok := a.sessions[row.SessionID]
	if !ok {
		entry = &SessionUsageTotals{
			SessionID:         row.SessionID,
			ExternalSessionID: row.ExternalSessionID,
			ClientKind:        row.ClientKind,
			Participant:       row.Participant,
			Status:            row.Status,
			StartedAt:         row.StartedAt,
			StoppedAt:         row.StoppedAt,
			Priced:            true,
		}
		a.sessions[row.SessionID] = entry
		a.sessionOrder = append(a.sessionOrder, row.SessionID)
	}
	entry.InputTokens += row.InputTokens
	entry.OutputTokens += row.OutputTokens
	entry.CacheWriteTokens += row.CacheWriteTokens
	entry.CacheReadTokens += row.CacheReadTokens
	entry.ToolCalls += row.ToolCalls
	entry.ElapsedMS += row.ElapsedMS
	entry.EstimatedCost += cost
	if currency != "" {
		entry.Currency = currency
	}
	// A session with no snapshot at all has nothing to price yet, which is
	// not the same as having failed to price something.
	if !priced && row.CostJSON != "" {
		entry.Priced = false
	}
}

func (a *usageAccumulator) projection(orchestrationID string) UsageProjection {
	sort.Strings(a.unpricedModels)
	out := UsageProjection{
		OrchestrationID: orchestrationID,
		Counters:        a.counters,
		TelemetryStatus: a.telemetryStatus,
		UnpricedModels:  a.unpricedModels,
	}
	out.TotalTokens = a.counters.InputTokens + a.counters.OutputTokens + a.counters.CacheWriteTokens + a.counters.CacheReadTokens
	if a.costKnownAny {
		out.EstimatedCost = &CostTotal{Amount: a.costTotal, Currency: a.currency, FullyKnown: a.costFullyKnown}
	}
	// Ordered by spend, so the model or sitting that drove the bill reads
	// first rather than whichever the join happened to return first.
	sort.SliceStable(a.modelOrder, func(i, j int) bool {
		return a.models[a.modelOrder[i]].EstimatedCost > a.models[a.modelOrder[j]].EstimatedCost
	})
	for _, model := range a.modelOrder {
		out.ByModel = append(out.ByModel, *a.models[model])
	}
	sort.SliceStable(a.sessionOrder, func(i, j int) bool {
		return a.sessions[a.sessionOrder[i]].EstimatedCost > a.sessions[a.sessionOrder[j]].EstimatedCost
	})
	for _, session := range a.sessionOrder {
		out.BySession = append(out.BySession, *a.sessions[session])
	}
	return out
}
