CREATE TABLE IF NOT EXISTS drives (
    id BIGINT PRIMARY KEY,
    device TEXT NOT NULL,
    identity_key TEXT NOT NULL CHECK (identity_key <> ''),
    model TEXT,
    serial TEXT,
    wwn TEXT,
    first_seen_at TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP NOT NULL
);

CREATE SEQUENCE IF NOT EXISTS seq_drives START 1;

CREATE TABLE IF NOT EXISTS smart_samples (
    id BIGINT PRIMARY KEY,
    drive_id BIGINT NOT NULL,
    collected_at TIMESTAMP NOT NULL,
    temperature INTEGER,
    power_on_hours BIGINT,
    reallocated_sectors BIGINT,
    pending_sectors BIGINT,
    uncorrectable_sectors BIGINT,
    wear_level BIGINT,
    raw_json JSON,
    FOREIGN KEY (drive_id) REFERENCES drives(id)
);

CREATE SEQUENCE IF NOT EXISTS seq_samples START 1;

CREATE TABLE IF NOT EXISTS smart_hourly_rollups (
    drive_id BIGINT NOT NULL, bucket_start TIMESTAMP NOT NULL, bucket_end TIMESTAMP NOT NULL,
    sample_count BIGINT NOT NULL, temp_count BIGINT NOT NULL, temp_min DOUBLE, temp_max DOUBLE, temp_avg DOUBLE, temp_last INTEGER,
    power_on_hours_last BIGINT, reallocated_max BIGINT, reallocated_last BIGINT,
    pending_max BIGINT, pending_last BIGINT, uncorrectable_max BIGINT, uncorrectable_last BIGINT,
    wear_min BIGINT, wear_last BIGINT, health_status TEXT, health_score INTEGER,
    last_sample_at TIMESTAMP NOT NULL, last_sample_id BIGINT NOT NULL,
    PRIMARY KEY (drive_id, bucket_start)
);
CREATE TABLE IF NOT EXISTS smart_daily_rollups (
    drive_id BIGINT NOT NULL, bucket_start TIMESTAMP NOT NULL, bucket_end TIMESTAMP NOT NULL,
    sample_count BIGINT NOT NULL, temp_count BIGINT NOT NULL, temp_min DOUBLE, temp_max DOUBLE, temp_avg DOUBLE, temp_last INTEGER,
    power_on_hours_last BIGINT, reallocated_max BIGINT, reallocated_last BIGINT,
    pending_max BIGINT, pending_last BIGINT, uncorrectable_max BIGINT, uncorrectable_last BIGINT,
    wear_min BIGINT, wear_last BIGINT, health_status TEXT, health_score INTEGER,
    last_sample_at TIMESTAMP NOT NULL, last_sample_id BIGINT NOT NULL,
    PRIMARY KEY (drive_id, bucket_start)
);
CREATE TABLE IF NOT EXISTS smart_rollup_pending_samples (sample_id BIGINT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS smart_prune_pending_samples (sample_id BIGINT PRIMARY KEY);

CREATE TABLE IF NOT EXISTS smart_attributes (
    sample_id BIGINT NOT NULL,
    attribute_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    value INTEGER,
    worst INTEGER,
    threshold INTEGER,
    raw TEXT,
    PRIMARY KEY (sample_id, attribute_id),
    FOREIGN KEY (sample_id) REFERENCES smart_samples(id)
);

CREATE TABLE IF NOT EXISTS drive_health (
    drive_id BIGINT NOT NULL,
    sample_id BIGINT NOT NULL,
    status TEXT NOT NULL,
    score INTEGER NOT NULL,
    reasons TEXT,
    PRIMARY KEY (drive_id, sample_id),
    FOREIGN KEY (drive_id) REFERENCES drives(id),
    FOREIGN KEY (sample_id) REFERENCES smart_samples(id)
);

CREATE TABLE IF NOT EXISTS smart_test_runs (
    id BIGINT PRIMARY KEY,
    drive_id BIGINT NOT NULL,
    test_type TEXT NOT NULL,
    scheduled_at TIMESTAMP NOT NULL,
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    status TEXT NOT NULL,
    message TEXT,
    FOREIGN KEY (drive_id) REFERENCES drives(id)
);

CREATE SEQUENCE IF NOT EXISTS seq_smart_test_runs START 1;

CREATE TABLE IF NOT EXISTS notification_state (
    drive_id BIGINT NOT NULL,
    notification_name TEXT NOT NULL,
    state TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (drive_id, notification_name),
    FOREIGN KEY (drive_id) REFERENCES drives(id)
);
