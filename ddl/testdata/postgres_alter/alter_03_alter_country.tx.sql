ALTER TABLE country ALTER COLUMN country_id DROP IDENTITY;

ALTER TABLE country ALTER COLUMN country TYPE TEXT COLLATE "C";
