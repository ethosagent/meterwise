package store

import (
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// addMemory adds memory's likely credit (memory_hits by ts, injected hints only) to the hero and the days. It touches
// the likely classes only: memory never enters the certain number (ADR 0027, non-negotiable 5). days is the zero-filled
// range from daysAndHero; a row for a day outside it is ignored.
func addMemory(days []contract.SavingsDay, hero *contract.SavingsHero, mem []sqlc.SavingsMemoryByDayRow) {
	idx := make(map[string]int, len(days))
	for i, d := range days {
		idx[d.Day] = i
	}
	for _, m := range mem {
		likely := contract.MicroUSD(m.LikelyMicro)
		if !m.Day.Valid {
			hero.LikelySavedUsd += likely
			hero.LikelySavedTokens += m.LikelyTokens
			continue
		}
		if i, ok := idx[m.Day.Time.UTC().Format(time.DateOnly)]; ok {
			days[i].LikelySavedUsd += likely
		}
	}
}

// memoryCard is the memory cause row from the totals row of SavingsMemoryByDay: injected hints as fires, the credited
// likely money and tokens. Observe-only hits carry no money, so would-save stays zero.
func memoryCard(mem []sqlc.SavingsMemoryByDayRow) contract.CauseCard {
	var tot sqlc.SavingsMemoryByDayRow
	for _, m := range mem {
		if !m.Day.Valid {
			tot = m
		}
	}
	return contract.CauseCard{CauseView: contract.CauseView{
		Cause: contract.CauseMemory, Kind: contract.KindMemory, Fires: tot.Fires,
		LikelySavedUsd: contract.MicroUSD(tot.LikelyMicro), LikelySavedTokens: tot.LikelyTokens,
	}}
}
