-- EAN/UPC printed on the box of physical copies; used to detect duplicates when scanning.
ALTER TABLE copies ADD COLUMN barcode TEXT NOT NULL DEFAULT '';
CREATE INDEX copies_barcode ON copies(barcode) WHERE barcode <> '';
