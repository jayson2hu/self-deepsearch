-- +goose Up
CREATE TABLE platform.seo_report_imports (
    import_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    report_type text NOT NULL CHECK (report_type IN ('performance', 'indexing')),
    dataset_kind text NOT NULL CHECK (dataset_kind IN ('real', 'demo')),
    property_uri text NOT NULL CHECK (length(property_uri) BETWEEN 4 AND 500),
    period_start date NOT NULL,
    period_end date NOT NULL,
    exported_at timestamptz NOT NULL,
    file_sha256 text NOT NULL CHECK (file_sha256 ~ '^[a-f0-9]{64}$'),
    row_count integer NOT NULL CHECK (row_count BETWEEN 1 AND 10000),
    status text NOT NULL DEFAULT 'imported' CHECK (status IN ('imported', 'failed')),
    error_summary text CHECK (error_summary IS NULL OR length(error_summary) <= 1000),
    imported_by uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (period_end >= period_start),
    UNIQUE (report_type, dataset_kind, property_uri, file_sha256)
);

CREATE TABLE platform.seo_report_rows (
    import_id uuid NOT NULL REFERENCES platform.seo_report_imports(import_id) ON DELETE RESTRICT,
    row_number integer NOT NULL CHECK (row_number BETWEEN 1 AND 10000),
    dimensions jsonb NOT NULL CHECK (jsonb_typeof(dimensions) = 'object'),
    metrics jsonb NOT NULL CHECK (jsonb_typeof(metrics) = 'object'),
    PRIMARY KEY (import_id, row_number)
);

CREATE INDEX seo_report_imports_created_idx ON platform.seo_report_imports (created_at DESC, import_id);
GRANT SELECT, INSERT ON platform.seo_report_imports, platform.seo_report_rows TO platform_api;

UPDATE platform.system_metadata SET schema_version = 26 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 25 WHERE singleton;
DROP TABLE platform.seo_report_rows;
DROP TABLE platform.seo_report_imports;
