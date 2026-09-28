-- Create "customers" table
CREATE TABLE "customers" (
  "id" character varying NOT NULL,
  "external_id" character varying NOT NULL,
  "email" character varying NOT NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "customers_external_id_key" ON "customers" ("external_id");
CREATE INDEX "customer_external_id" ON "customers" ("external_id");

-- Create "addresses" table
CREATE TABLE "addresses" (
  "id" character varying NOT NULL,
  "asset" character varying NOT NULL,
  "address" character varying NOT NULL,
  "created_at" timestamptz NOT NULL,
  "customer_id" character varying NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "addresses_customers_addresses" FOREIGN KEY ("customer_id") REFERENCES "customers" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
CREATE UNIQUE INDEX "address_asset_address" ON "addresses" ("asset", "address");
CREATE INDEX "address_customer_id" ON "addresses" ("customer_id");

-- Create "deposits" table
CREATE TABLE "deposits" (
  "id" character varying NOT NULL,
  "external_tx_id" character varying NOT NULL,
  "tx_hash" character varying NULL,
  "asset" character varying NOT NULL,
  "amount_atomic" character varying NOT NULL,
  "status" character varying NOT NULL DEFAULT 'PENDING',
  "confirmations" bigint NOT NULL DEFAULT 0,
  "required_confirmations" bigint NOT NULL,
  "transaction_metadata" jsonb NULL,
  "version" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "customer_id" character varying NOT NULL,
  "address_id" character varying NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "deposits_customers_deposits" FOREIGN KEY ("customer_id") REFERENCES "customers" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "deposits_addresses_deposits" FOREIGN KEY ("address_id") REFERENCES "addresses" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
CREATE UNIQUE INDEX "deposits_external_tx_id_key" ON "deposits" ("external_tx_id");
CREATE INDEX "deposit_customer_id" ON "deposits" ("customer_id");
CREATE INDEX "deposit_address_id" ON "deposits" ("address_id");
CREATE INDEX "deposit_status" ON "deposits" ("status");
CREATE INDEX "deposit_tx_hash" ON "deposits" ("tx_hash");

-- Create "withdrawals" table
CREATE TABLE "withdrawals" (
  "id" character varying NOT NULL,
  "idempotency_key" character varying NOT NULL,
  "destination_address" character varying NOT NULL,
  "tx_hash" character varying NULL,
  "asset" character varying NOT NULL,
  "amount_atomic" character varying NOT NULL,
  "status" character varying NOT NULL DEFAULT 'PENDING',
  "confirmations" bigint NOT NULL DEFAULT 0,
  "required_confirmations" bigint NOT NULL,
  "retry_count" bigint NOT NULL DEFAULT 0,
  "next_retry_at" timestamptz NULL,
  "failure_reason" character varying NULL,
  "transaction_metadata" jsonb NULL,
  "version" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "customer_id" character varying NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "withdrawals_customers_withdrawals" FOREIGN KEY ("customer_id") REFERENCES "customers" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
CREATE UNIQUE INDEX "withdrawals_idempotency_key_key" ON "withdrawals" ("idempotency_key");
CREATE INDEX "withdrawal_customer_id" ON "withdrawals" ("customer_id");
CREATE INDEX "withdrawal_status" ON "withdrawals" ("status");
CREATE INDEX "withdrawal_tx_hash" ON "withdrawals" ("tx_hash");

-- Create "deposit_events" table
CREATE TABLE "deposit_events" (
  "id" character varying NOT NULL,
  "event_type" character varying NOT NULL,
  "from_status" character varying NULL,
  "to_status" character varying NULL,
  "metadata" jsonb NULL,
  "created_at" timestamptz NOT NULL,
  "deposit_id" character varying NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "deposit_events_deposits_events" FOREIGN KEY ("deposit_id") REFERENCES "deposits" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
CREATE INDEX "depositevent_deposit_id" ON "deposit_events" ("deposit_id");
CREATE INDEX "depositevent_event_type" ON "deposit_events" ("event_type");
CREATE INDEX "depositevent_created_at" ON "deposit_events" ("created_at");

-- Create "withdrawal_events" table
CREATE TABLE "withdrawal_events" (
  "id" character varying NOT NULL,
  "event_type" character varying NOT NULL,
  "from_status" character varying NULL,
  "to_status" character varying NULL,
  "metadata" jsonb NULL,
  "created_at" timestamptz NOT NULL,
  "withdrawal_id" character varying NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "withdrawal_events_withdrawals_events" FOREIGN KEY ("withdrawal_id") REFERENCES "withdrawals" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
CREATE INDEX "withdrawalevent_withdrawal_id" ON "withdrawal_events" ("withdrawal_id");
CREATE INDEX "withdrawalevent_event_type" ON "withdrawal_events" ("event_type");
CREATE INDEX "withdrawalevent_created_at" ON "withdrawal_events" ("created_at");

-- Create "hot_wallets" table
CREATE TABLE "hot_wallets" (
  "id" character varying NOT NULL,
  "asset" character varying NOT NULL,
  "address" character varying NOT NULL,
  "next_nonce" bigint NOT NULL DEFAULT 0,
  "version" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "hot_wallets_asset_key" ON "hot_wallets" ("asset");
