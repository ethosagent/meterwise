-- name: ListPricing :many
-- Every price row; the ledger's loader hands them to the pure savings.TablePricer snapshot.
SELECT provider, model, input_per_m, output_per_m, cached_per_m, reasoning_per_m, effective_from
FROM pricing
ORDER BY provider, model, effective_from;
