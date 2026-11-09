package contextbuilder

import "encoding/json"

// ApplyBudgetWithMemoryLimit reserves authoritative/recent/summary priority,
// then caps retrieved evidence independently, including serialized provenance.
// Counts are deterministic rune-based estimates, not model tokenizer counts.
func ApplyBudgetWithMemoryLimit(in Result, budget, memoryBudget int) Result {
	base := in
	base.Retrieved = nil
	out := ApplyBudget(base, budget)
	used := estimate(string(out.Authoritative))
	for _, text := range out.RecentBeats {
		used += estimate(text)
	}
	for _, text := range out.Summaries {
		used += estimate(text)
	}
	remaining := min(budget-used, memoryBudget)
	for _, item := range in.Retrieved {
		raw, err := json.Marshal(item)
		if err != nil {
			continue
		}
		cost := estimate(string(raw))
		if cost > remaining {
			break
		}
		out.Retrieved = append(out.Retrieved, item)
		remaining -= cost
	}
	return out
}

// ApplyBudget uses a deterministic priority order. Authoritative state is never
// removed by semantic retrieval; if it alone exceeds the requested soft budget,
// it is returned intact and lower-priority material is omitted.
func ApplyBudget(in Result, budget int) Result {
	if budget <= 0 {
		return Result{Authoritative: append([]byte(nil), in.Authoritative...), RetrievalError: in.RetrievalError}
	}
	out := Result{Authoritative: append([]byte(nil), in.Authoritative...), RetrievalError: in.RetrievalError}
	used := estimate(string(out.Authoritative))
	if used >= budget {
		return out
	}
	for _, v := range in.RecentBeats {
		n := estimate(v)
		if used+n > budget {
			break
		}
		out.RecentBeats = append(out.RecentBeats, v)
		used += n
	}
	for _, v := range in.Summaries {
		n := estimate(v)
		if used+n > budget {
			break
		}
		out.Summaries = append(out.Summaries, v)
		used += n
	}
	for _, v := range in.Retrieved {
		n := estimate(v.Content)
		if used+n > budget {
			break
		}
		out.Retrieved = append(out.Retrieved, v)
		used += n
	}
	return out
}
func estimate(s string) int {
	if s == "" {
		return 0
	}
	return (len([]rune(s)) + 3) / 4
}
