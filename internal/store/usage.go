package store

import (
	"context"
	"time"
)

type UsageRow struct {
	Participant      string  `json:"participant"`
	Model            string  `json:"model,omitempty"`
	ModelSource      string  `json:"model_source,omitempty"`
	Messages         int     `json:"messages"`
	Failed           int     `json:"failed"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	CostReported     int     `json:"cost_reported"`
	InputTokens      int     `json:"input_tokens,omitempty"`
	OutputTokens     int     `json:"output_tokens,omitempty"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	TokensReported   int     `json:"tokens_reported"`
}

func (s *Store) Usage(ctx context.Context, since time.Time) ([]UsageRow, error) {
	where := "kind IN ('message','error')"
	var args []any
	if !since.IsZero() {
		where += " AND julianday(created_at) >= julianday(?)"
		args = append(args, ts(since))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT author, model, model_source,
SUM(CASE WHEN kind='message' THEN 1 ELSE 0 END),
SUM(CASE WHEN kind='error' THEN 1 ELSE 0 END),
SUM(cost_usd),
SUM(CASE WHEN cost_usd>0 THEN 1 ELSE 0 END),
SUM(input_tokens), SUM(output_tokens), SUM(cache_read_tokens), SUM(cache_write_tokens),
SUM(CASE WHEN input_tokens>0 OR output_tokens>0 OR cache_read_tokens>0 OR cache_write_tokens>0 THEN 1 ELSE 0 END)
FROM messages WHERE `+where+`
GROUP BY author, model, model_source
ORDER BY SUM(cost_usd) DESC, SUM(CASE WHEN kind='message' THEN 1 ELSE 0 END) DESC, author`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []UsageRow
	for rows.Next() {
		var r UsageRow
		if err := rows.Scan(&r.Participant, &r.Model, &r.ModelSource, &r.Messages, &r.Failed, &r.CostUSD,
			&r.CostReported, &r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens, &r.TokensReported); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
