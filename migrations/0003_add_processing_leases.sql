-- Add processing lease columns to deposits (ADR-0002)
ALTER TABLE "deposits" ADD COLUMN "locked_by" character varying;
ALTER TABLE "deposits" ADD COLUMN "locked_until" timestamptz;

-- Add processing lease columns to withdrawals (ADR-0002)
ALTER TABLE "withdrawals" ADD COLUMN "locked_by" character varying;
ALTER TABLE "withdrawals" ADD COLUMN "locked_until" timestamptz;

-- Composite indexes supporting the lease-claim query
-- (status = ? AND (locked_until IS NULL OR locked_until < now()))
CREATE INDEX "deposit_status_locked_until" ON "deposits" ("status", "locked_until");
CREATE INDEX "withdrawal_status_locked_until" ON "withdrawals" ("status", "locked_until");
