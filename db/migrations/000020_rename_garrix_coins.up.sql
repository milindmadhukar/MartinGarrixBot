-- Rebrand: the economy currency is "STMPD Coins", so the column that stores it
-- should say so too. This is a pure rename -- no type change, no data movement,
-- and Postgres rewrites nothing, so it is safe on a live table.
--
-- Migrations run at bot boot, which means the schema flips a moment before the
-- new binary serves traffic. A RENAME is not backward compatible: the previous
-- image's queries still say garrix_coins and would error. Deploy the new image
-- and let it migrate on start; do not roll the old image forward over this.
ALTER TABLE users RENAME COLUMN garrix_coins TO stmpd_coins;
