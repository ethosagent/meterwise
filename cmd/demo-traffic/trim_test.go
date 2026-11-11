package main

import (
	"bytes"
	"context"
	"testing"
)

// The trim scenarios run against the fake upstream through the real chain: with -trim lossless the OpenAI session's
// calls are acted (certain: openai.chat with measured input), the Anthropic session's are acted but likely, and the
// stages that fire are the ones the scenario was written for. With -trim observe nothing is rewritten, and with trim
// off the scenarios do not play at all.
func TestTrimScenarioLosslessRewritesAndPricesTheCalls(t *testing.T) {
	ctx := context.Background()
	dsn, st, _ := demoDB(t)
	var out bytes.Buffer
	res, err := run(ctx, config{dsn: dsn, rounds: 1, trim: "lossless"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.sessions) != 6 || res.clientCalls != res.upstreamHits {
		t.Fatalf("result = %+v (4 base sessions + 2 trim sessions; nothing is blocked)", res)
	}
	q := func(sql string) int { return scalar(t, st, sql) }
	if n := q(`SELECT count(*) FROM calls WHERE session_id LIKE '%-trim-openai' AND trim_action = 'acted' AND trim_confidence = 'certain' AND saved_usd_trim > 0`); n < 3 {
		t.Errorf("certain acted openai calls = %d, want at least 3 of 5", n)
	}
	if n := q(`SELECT count(*) FROM calls WHERE session_id LIKE '%-trim-anthropic' AND trim_action = 'acted' AND trim_confidence = 'likely'`); n < 1 {
		t.Errorf("likely acted anthropic calls = %d, want at least 1", n)
	}
	if n := q(`SELECT count(*) FROM calls WHERE trim_confidence = 'certain' AND wire <> 'openai.chat'`); n != 0 {
		t.Errorf("%d certain trim calls are not on openai.chat", n)
	}
	for _, stage := range []string{"log_window", "repeat_collapse"} {
		if n := q(`SELECT count(*) FROM trim_events WHERE stage = '` + stage + `' AND action = 'acted'`); n == 0 {
			t.Errorf("stage %s never fired", stage)
		}
	}
	// the OpenAI session's saved number is the sum of its calls' certain trim savings: nothing is lost between the tables
	a := q(`SELECT (COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_confidence = 'certain'),0)*1000000)::bigint FROM calls WHERE session_id LIKE '%-trim-openai'`)
	b := q(`SELECT (COALESCE(SUM(saved_usd),0)*1000000)::bigint FROM sessions WHERE id LIKE '%-trim-openai'`)
	if a == 0 || a != b {
		t.Errorf("calls saved %d != session saved %d", a, b)
	}
}

func TestTrimScenarioObserveNeverRewritesAndOffDoesNotPlay(t *testing.T) {
	ctx := context.Background()
	dsn, st, _ := demoDB(t)
	var out bytes.Buffer
	if _, err := run(ctx, config{dsn: dsn, rounds: 1, trim: "observe"}, &out); err != nil {
		t.Fatal(err)
	}
	if n := scalar(t, st, `SELECT count(*) FROM calls WHERE trim_action = 'acted'`); n != 0 {
		t.Errorf("observe acted on %d calls", n)
	}
	if n := scalar(t, st, `SELECT count(*) FROM calls WHERE trim_action = 'observe'`); n == 0 {
		t.Error("observe recorded no would-save")
	}
	dsn2, st2, _ := demoDB(t)
	res, err := run(ctx, config{dsn: dsn2, rounds: 1}, &out)
	if err != nil || len(res.sessions) != 4 {
		t.Fatalf("off: %+v %v", res, err)
	}
	if n := scalar(t, st2, `SELECT count(*) FROM trim_events`); n != 0 {
		t.Errorf("trim off wrote %d trim events", n)
	}
}
