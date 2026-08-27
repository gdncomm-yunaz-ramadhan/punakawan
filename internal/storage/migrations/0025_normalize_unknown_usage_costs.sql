-- A zero-valued price with this provenance meant "rate unavailable", not a
-- known free rate. Preserve measured duration while restoring unknown cost.
UPDATE delivery_usage_ledger
SET unit_price = NULL,
    cost_amount = NULL,
    cost_currency = '',
    price_source = ''
WHERE category = 'engineering_worklog'
  AND unit_price = 0
  AND cost_amount = 0
  AND price_source LIKE '%billing rates, which are not available%';
