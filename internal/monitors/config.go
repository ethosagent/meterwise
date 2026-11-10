package monitors

import (
	"sync"

	"github.com/ethosagent/meterwise/internal/contract"
)

// AttrMonitors is the Tenant.Attrs key wsauth fills with the workspace's canonical monitor config
// (ruling R5): contract.MonitorConfig.Attr().
const AttrMonitors = "monitors"

const memoCap = 1024

// configMemo decodes the tenant attr once per distinct string, so Before does no per-call parse.
type configMemo struct {
	mu       sync.RWMutex
	m        map[string]contract.MonitorConfig
	defaults contract.MonitorConfig
}

func newConfigMemo() *configMemo {
	return &configMemo{m: map[string]contract.MonitorConfig{}, defaults: contract.DefaultMonitorConfig()}
}

// get returns the config for attr; a missing or bad attr is the defaults.
func (c *configMemo) get(attr string) contract.MonitorConfig {
	if attr == "" {
		return c.defaults
	}
	c.mu.RLock()
	cfg, ok := c.m[attr]
	c.mu.RUnlock()
	if ok {
		return cfg
	}
	cfg = contract.ParseMonitorAttr(attr)
	c.mu.Lock()
	if len(c.m) >= memoCap {
		clear(c.m)
	}
	c.m[attr] = cfg
	c.mu.Unlock()
	return cfg
}
