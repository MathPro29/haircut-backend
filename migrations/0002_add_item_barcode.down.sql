DROP INDEX IF EXISTS idx_inventory_items_barcode;
ALTER TABLE inventory_items DROP COLUMN IF EXISTS barcode;
