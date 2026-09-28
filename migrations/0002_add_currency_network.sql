-- Add currency and network columns to deposits
ALTER TABLE "deposits" ADD COLUMN "currency" character varying(10);
ALTER TABLE "deposits" ADD COLUMN "network" character varying(20);

-- Add currency and network columns to withdrawals
ALTER TABLE "withdrawals" ADD COLUMN "currency" character varying(10);
ALTER TABLE "withdrawals" ADD COLUMN "network" character varying(20);

-- Add currency and network columns to addresses
ALTER TABLE "addresses" ADD COLUMN "currency" character varying(10);
ALTER TABLE "addresses" ADD COLUMN "network" character varying(20);

-- Add currency and network columns to hot_wallets
ALTER TABLE "hot_wallets" ADD COLUMN "currency" character varying(10);
ALTER TABLE "hot_wallets" ADD COLUMN "network" character varying(20);

-- Backfill currency and network from asset column
-- BTC -> (BTC, bitcoin), ETH -> (ETH, ethereum), USD -> (USD, fiat), EUR -> (EUR, fiat)
UPDATE "deposits" SET "currency" = "asset", "network" = CASE
  WHEN "asset" = 'BTC' THEN 'bitcoin'
  WHEN "asset" = 'ETH' THEN 'ethereum'
  WHEN "asset" = 'USD' THEN 'fiat'
  WHEN "asset" = 'EUR' THEN 'fiat'
  ELSE 'unknown'
END WHERE "currency" IS NULL;

UPDATE "withdrawals" SET "currency" = "asset", "network" = CASE
  WHEN "asset" = 'BTC' THEN 'bitcoin'
  WHEN "asset" = 'ETH' THEN 'ethereum'
  WHEN "asset" = 'USD' THEN 'fiat'
  WHEN "asset" = 'EUR' THEN 'fiat'
  ELSE 'unknown'
END WHERE "currency" IS NULL;

UPDATE "addresses" SET "currency" = "asset", "network" = CASE
  WHEN "asset" = 'BTC' THEN 'bitcoin'
  WHEN "asset" = 'ETH' THEN 'ethereum'
  WHEN "asset" = 'USD' THEN 'fiat'
  WHEN "asset" = 'EUR' THEN 'fiat'
  ELSE 'unknown'
END WHERE "currency" IS NULL;

UPDATE "hot_wallets" SET "currency" = "asset", "network" = CASE
  WHEN "asset" = 'BTC' THEN 'bitcoin'
  WHEN "asset" = 'ETH' THEN 'ethereum'
  WHEN "asset" = 'USD' THEN 'fiat'
  WHEN "asset" = 'EUR' THEN 'fiat'
  ELSE 'unknown'
END WHERE "currency" IS NULL;

-- Make currency and network NOT NULL after backfill
ALTER TABLE "deposits" ALTER COLUMN "currency" SET NOT NULL;
ALTER TABLE "deposits" ALTER COLUMN "network" SET NOT NULL;
ALTER TABLE "withdrawals" ALTER COLUMN "currency" SET NOT NULL;
ALTER TABLE "withdrawals" ALTER COLUMN "network" SET NOT NULL;
ALTER TABLE "addresses" ALTER COLUMN "currency" SET NOT NULL;
ALTER TABLE "addresses" ALTER COLUMN "network" SET NOT NULL;
ALTER TABLE "hot_wallets" ALTER COLUMN "currency" SET NOT NULL;
ALTER TABLE "hot_wallets" ALTER COLUMN "network" SET NOT NULL;

-- Update unique index on addresses to use (currency, network, address)
DROP INDEX "address_asset_address";
CREATE UNIQUE INDEX "address_currency_network_address" ON "addresses" ("currency", "network", "address");

-- Update unique index on hot_wallets to use (currency, network)
DROP INDEX "hot_wallets_asset_key";
CREATE UNIQUE INDEX "hot_wallets_currency_network_key" ON "hot_wallets" ("currency", "network");

-- Make asset column nullable (deprecated, kept for backward compatibility)
ALTER TABLE "deposits" ALTER COLUMN "asset" DROP NOT NULL;
ALTER TABLE "withdrawals" ALTER COLUMN "asset" DROP NOT NULL;
ALTER TABLE "addresses" ALTER COLUMN "asset" DROP NOT NULL;
ALTER TABLE "hot_wallets" ALTER COLUMN "asset" DROP NOT NULL;
