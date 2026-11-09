package dataplane_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
)

type fakeSource struct{ name string }

func (f fakeSource) Status() map[string]any { return map[string]any{"queue_depth": 3, "dropped": 0} }
func (f fakeSource) Name() string           { return f.name }

type anonSource struct{}

func (anonSource) Status() map[string]any { return map[string]any{"ok": true} }

func TestStatuszRendersEveryRegisteredSourceBesideTollgatesOwnCounters(t *testing.T) {
	p, err := dataplane.New(cfg(t, map[string]string{}), dataplane.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	adm, _ := dataplane.NewAdmission(4, dataplane.QueueTimeout)
	srv := httptest.NewServer(dataplane.AdminHandler(p.AdminHandler(), []contract.StatusSource{fakeSource{"ledger"}, adm, anonSource{}}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/statusz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil || resp.StatusCode != 200 {
		t.Fatalf("status %d, %v, %s", resp.StatusCode, err, b)
	}
	src, _ := doc["sources"].(map[string]any)
	led, _ := src["ledger"].(map[string]any)
	adminSrc, _ := src["admission"].(map[string]any)
	anon, _ := src["source2"].(map[string]any)
	if led["queue_depth"] != float64(3) || adminSrc["default_max"] != float64(4) || anon["ok"] != true {
		t.Fatalf("sources = %s", b)
	}
	if _, ok := doc["in_flight"]; !ok {
		t.Fatalf("tollgate's own counters are gone: %s", b)
	}
	if m, err := http.Get(srv.URL + "/metrics"); err != nil || m.StatusCode != 200 {
		t.Fatalf("/metrics must pass through: %v", err)
	} else {
		_ = m.Body.Close()
	}
}

func TestOptionsUseTheMeterwiseSessionHeaderAndMeasurePrediction(t *testing.T) {
	o := dataplane.Options(dataplane.Deps{})
	if o.Headers.Session != "x-meterwise-session" || !o.MeasureSessionPrediction || o.Sessions != nil {
		t.Fatalf("session options: %+v measure=%v strategy=%v", o.Headers, o.MeasureSessionPrediction, o.Sessions)
	}
}
