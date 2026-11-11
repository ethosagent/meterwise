package router

import "github.com/ethosagent/meterwise/internal/contract"

// Allows reports whether a model with capabilities c can serve a request with features f. A request that needs a
// capability the model is not recorded to have is refused (unknown means no), and a request with any tool
// definition is refused for a model whose MaxTools is 0 or smaller than the number of tools.
func Allows(c contract.ModelCaps, f contract.RequestFeatures) bool {
	switch {
	case f.Tools > c.MaxTools:
		return false
	case f.JSONSchema && !c.JSONSchema:
		return false
	case f.Vision && !c.Vision:
		return false
	case f.Reasoning && !c.Reasoning:
		return false
	}
	return true
}
