-- Seed test data for local development and testing.
-- Not part of the Atlas-tracked schema migrations directory: this is data,
-- not schema, so it's applied as a separate unversioned step after ApplyAll's
-- schema migrations.

-- Insert test customers
INSERT INTO customers (id, external_id, email, created_at, updated_at) VALUES
    ('00000000-0000-0000-0000-000000000001', 'test-customer-001', 'alice@example.com', now(), now()),
    ('00000000-0000-0000-0000-000000000002', 'test-customer-002', 'bob@example.com', now(), now())
ON CONFLICT (external_id) DO NOTHING;

-- Insert test addresses matching sample payloads
INSERT INTO addresses (id, customer_id, asset, address, created_at) VALUES
    -- Sample BTC deposit address
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000001', 'BTC', 'bc1qfndhdgmxk8lv4xrh5j3zsk2p6y4djk2ym3h7zn', now()),
    -- Sample ETH deposit address
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000001', 'ETH', '0x440402e8DEE73c17132167D9b57BEc84b06e8ECA', now()),
    -- Additional test addresses
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000002', 'BTC', 'bc1q5x5aydqgjw5d3j4u2t8d6v9w7e8r9t0y1u2i3', now()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000002', 'ETH', '0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb1', now())
ON CONFLICT (asset, address) DO NOTHING;
