package contract

// RouterCapRow is one router_model_capabilities row as the store reads it (the newest row effective now): a nil
// MaxTools is NULL in the table (the provider's page states no maximum). The router resolves it into ModelCaps.
type RouterCapRow struct {
	Provider, Model               string
	MaxTools                      *int
	JSONSchema, Vision, Reasoning bool
}
