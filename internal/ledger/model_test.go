package ledger_test

import (
	"context"
	"testing"
)

// Cost and the row use the model tollgate reports on the Metric when it reports one (the response
// model, once tollgate carries it); otherwise the model of the request.
func TestCostUsesTheReportedModelElseTheRequestModel(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, nil)

	reported := directCall("reported", "hi", nil, 1) // request says gpt-4o
	if _, err := h.Before(context.Background(), reported); err != nil {
		t.Fatal(err)
	}
	m := metricFor(reported)
	m.Model = "gpt-4o-mini" // what the provider answered with
	h.After(context.Background(), reported, m)

	requested := directCall("requested", "hi", nil, 2)
	if _, err := h.Before(context.Background(), requested); err != nil {
		t.Fatal(err)
	}
	m = metricFor(requested)
	m.Model = "" // tollgate reported none
	h.After(context.Background(), requested, m)

	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	by := byIndex(w.snapshot())
	// 10 input + 5 output: gpt-4o-mini 0.15/0.60 per M = 1.5 + 3.0 -> 4 (rounded), gpt-4o 2.5/10 = 25 + 50 = 75
	if r := by[1]; r.Model != "gpt-4o-mini" || r.Cost == nil || *r.Cost == 75 {
		t.Errorf("reported model: %+v cost %v", r.Model, r.Cost)
	}
	if r := by[2]; r.Model != "gpt-4o" || r.Cost == nil || *r.Cost != 75 {
		t.Errorf("request model fallback: %+v cost %v", r.Model, r.Cost)
	}
}
