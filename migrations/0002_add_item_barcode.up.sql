-- บาร์โค้ดสินค้า (สแกนรับเข้า/ค้นหา) — unique เฉพาะแถวที่มีค่า
ALTER TABLE inventory_items ADD COLUMN barcode VARCHAR(64);
CREATE UNIQUE INDEX idx_inventory_items_barcode ON inventory_items (barcode) WHERE barcode IS NOT NULL;
