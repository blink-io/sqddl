ALTER TABLE movie ADD metadata NVARCHAR(MAX);

ALTER TABLE movie ALTER COLUMN movie_id INT NOT NULL;
