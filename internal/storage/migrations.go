//go:build cgo

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"diskmon/internal/smart"
)

func migrateDuckDB(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	hasIdentityKey, err := drivesHasColumn(ctx, tx, "identity_key")
	if err != nil {
		return err
	}
	if !hasIdentityKey {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE drives ADD COLUMN identity_key TEXT`); err != nil {
			return fmt.Errorf("add drives.identity_key: %w", err)
		}
	}

	plan, err := buildDriveMigrationPlan(ctx, tx, hasIdentityKey)
	if err != nil {
		return err
	}
	if err := applyDriveMigrationPlan(ctx, tx, plan); err != nil {
		return err
	}

	if err := ensureDuckDBQueryIndexes(ctx, tx); err != nil {
		return err
	}

	if plan.hasDuplicateIdentityKeys {
		// DuckDB does not support dropping the legacy device UNIQUE constraint in-place.
		// Preserve additive backfill/consolidation and skip only the new unique
		// identity index when unresolved duplicate identity keys remain.
		return nil
	}

	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_drives_identity_key ON drives(identity_key)`); err != nil {
		return fmt.Errorf("create drives identity index: %w", err)
	}

	return nil
}

func ensureDuckDBQueryIndexes(ctx context.Context, tx *sql.Tx) error {
	for _, index := range []struct {
		name string
		stmt string
	}{
		{name: "idx_smart_samples_drive_id_id", stmt: `CREATE INDEX IF NOT EXISTS idx_smart_samples_drive_id_id ON smart_samples(drive_id, id)`},
		{name: "idx_drive_health_drive_id_sample_id", stmt: `CREATE INDEX IF NOT EXISTS idx_drive_health_drive_id_sample_id ON drive_health(drive_id, sample_id)`},
		{name: "idx_smart_samples_drive_id_collected_at", stmt: `CREATE INDEX IF NOT EXISTS idx_smart_samples_drive_id_collected_at ON smart_samples(drive_id, collected_at)`},
		{name: "idx_smart_attributes_sample_id_attribute_id", stmt: `CREATE INDEX IF NOT EXISTS idx_smart_attributes_sample_id_attribute_id ON smart_attributes(sample_id, attribute_id)`},
		{name: "idx_smart_test_runs_drive_id_started_at", stmt: `CREATE INDEX IF NOT EXISTS idx_smart_test_runs_drive_id_started_at ON smart_test_runs(drive_id, started_at)`},
	} {
		if _, err := tx.ExecContext(ctx, index.stmt); err != nil {
			return fmt.Errorf("create query index %s: %w", index.name, err)
		}
	}
	return nil
}

func drivesHasColumn(ctx context.Context, tx *sql.Tx, column string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'drives'
		  AND column_name = ?
	`, column).Scan(&count); err != nil {
		return false, fmt.Errorf("query drives column %q: %w", column, err)
	}
	return count > 0, nil
}

type driveRow struct {
	ID          int64
	Device      string
	IdentityKey string
	Model       string
	Serial      string
	WWN         string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

type notificationStateRow struct {
	DriveID          int64
	NotificationName string
	State            string
	UpdatedAt        time.Time
}

type smartSampleRow struct {
	ID                   int64
	DriveID              int64
	CollectedAt          time.Time
	Temperature          sql.NullInt64
	PowerOnHours         sql.NullInt64
	ReallocatedSectors   sql.NullInt64
	PendingSectors       sql.NullInt64
	UncorrectableSectors sql.NullInt64
	WearLevel            sql.NullInt64
	RawJSON              sql.NullString
}

type smartAttributeRow struct {
	SampleID    int64
	AttributeID int64
	Name        string
	Value       sql.NullInt64
	Worst       sql.NullInt64
	Threshold   sql.NullInt64
	Raw         sql.NullString
}

type driveHealthRow struct {
	DriveID  int64
	SampleID int64
	Status   string
	Score    int64
	Reasons  sql.NullString
}

type smartTestRunRow struct {
	ID          int64
	DriveID     int64
	TestType    string
	ScheduledAt time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
	Status      string
	Message     sql.NullString
}

type driveMigrationPlan struct {
	drives                   []driveRow
	groups                   []driveDuplicateGroup
	duplicates               []driveDuplicateGroup
	rebuildDrives            bool
	hasDuplicateIdentityKeys bool
	stateRows                []notificationStateRow
	sampleRows               []smartSampleRow
	attributeRows            []smartAttributeRow
	healthRows               []driveHealthRow
	testRunRows              []smartTestRunRow
}

type driveDuplicateGroup struct {
	canonicalID int64
	memberIDs   []int64
	current     driveRow
	merged      driveRow
	duplicates  []int64
}

func buildDriveMigrationPlan(ctx context.Context, tx *sql.Tx, hasIdentityKey bool) (driveMigrationPlan, error) {
	drives, err := loadDriveRows(ctx, tx, hasIdentityKey)
	if err != nil {
		return driveMigrationPlan{}, err
	}
	groups, err := groupDriveRows(drives)
	if err != nil {
		return driveMigrationPlan{}, err
	}
	stateRows, err := loadNotificationStateRows(ctx, tx)
	if err != nil {
		return driveMigrationPlan{}, err
	}

	plan := driveMigrationPlan{drives: drives, groups: groups, stateRows: stateRows, rebuildDrives: !hasIdentityKey}
	identityCounts := make(map[string]int)
	for _, group := range groups {
		identityCounts[group.merged.IdentityKey]++
		if len(group.duplicates) > 0 {
			plan.duplicates = append(plan.duplicates, group)
		}
	}
	for _, count := range identityCounts {
		if count > 1 {
			plan.hasDuplicateIdentityKeys = true
			break
		}
	}

	if plan.rebuildDrives {
		if plan.sampleRows, err = loadSmartSampleRows(ctx, tx); err != nil {
			return driveMigrationPlan{}, err
		}
		if plan.attributeRows, err = loadSmartAttributeRows(ctx, tx); err != nil {
			return driveMigrationPlan{}, err
		}
		if plan.healthRows, err = loadDriveHealthRows(ctx, tx); err != nil {
			return driveMigrationPlan{}, err
		}
		if plan.testRunRows, err = loadSmartTestRunRows(ctx, tx); err != nil {
			return driveMigrationPlan{}, err
		}
	}

	return plan, nil
}

func loadDriveRows(ctx context.Context, tx *sql.Tx, hasIdentityKey bool) ([]driveRow, error) {
	query := `
		SELECT id, device, '', model, serial, wwn, first_seen_at, last_seen_at
		FROM drives
		ORDER BY id
	`
	if hasIdentityKey {
		query = `
			SELECT id, device, COALESCE(identity_key, ''), model, serial, wwn, first_seen_at, last_seen_at
			FROM drives
			ORDER BY id
		`
	}

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query drives: %w", err)
	}
	defer rows.Close()

	var drives []driveRow
	for rows.Next() {
		var row driveRow
		var model sql.NullString
		var serial sql.NullString
		var wwn sql.NullString
		if err := rows.Scan(&row.ID, &row.Device, &row.IdentityKey, &model, &serial, &wwn, &row.FirstSeenAt, &row.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan drive row: %w", err)
		}
		row.Model = nullStringValue(model)
		row.Serial = nullStringValue(serial)
		row.WWN = nullStringValue(wwn)
		drives = append(drives, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate drive rows: %w", err)
	}
	return drives, nil
}

func loadNotificationStateRows(ctx context.Context, tx *sql.Tx) ([]notificationStateRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT drive_id, notification_name, state, updated_at
		FROM notification_state
		ORDER BY drive_id, notification_name
	`)
	if err != nil {
		return nil, fmt.Errorf("query notification_state: %w", err)
	}
	defer rows.Close()

	var states []notificationStateRow
	for rows.Next() {
		var row notificationStateRow
		if err := rows.Scan(&row.DriveID, &row.NotificationName, &row.State, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan notification_state row: %w", err)
		}
		states = append(states, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification_state rows: %w", err)
	}
	return states, nil
}

func loadSmartSampleRows(ctx context.Context, tx *sql.Tx) ([]smartSampleRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, drive_id, collected_at, temperature, power_on_hours,
		       reallocated_sectors, pending_sectors, uncorrectable_sectors, wear_level, CAST(raw_json AS VARCHAR)
		FROM smart_samples
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query smart_samples: %w", err)
	}
	defer rows.Close()

	var samples []smartSampleRow
	for rows.Next() {
		var row smartSampleRow
		if err := rows.Scan(&row.ID, &row.DriveID, &row.CollectedAt, &row.Temperature, &row.PowerOnHours, &row.ReallocatedSectors, &row.PendingSectors, &row.UncorrectableSectors, &row.WearLevel, &row.RawJSON); err != nil {
			return nil, fmt.Errorf("scan smart_samples row: %w", err)
		}
		samples = append(samples, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate smart_samples rows: %w", err)
	}
	return samples, nil
}

func loadSmartAttributeRows(ctx context.Context, tx *sql.Tx) ([]smartAttributeRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT sample_id, attribute_id, name, value, worst, threshold, raw
		FROM smart_attributes
		ORDER BY sample_id, attribute_id, name
	`)
	if err != nil {
		return nil, fmt.Errorf("query smart_attributes: %w", err)
	}
	defer rows.Close()

	var attrs []smartAttributeRow
	for rows.Next() {
		var row smartAttributeRow
		if err := rows.Scan(&row.SampleID, &row.AttributeID, &row.Name, &row.Value, &row.Worst, &row.Threshold, &row.Raw); err != nil {
			return nil, fmt.Errorf("scan smart_attributes row: %w", err)
		}
		attrs = append(attrs, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate smart_attributes rows: %w", err)
	}
	return attrs, nil
}

func loadDriveHealthRows(ctx context.Context, tx *sql.Tx) ([]driveHealthRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT drive_id, sample_id, status, score, reasons
		FROM drive_health
		ORDER BY sample_id, drive_id
	`)
	if err != nil {
		return nil, fmt.Errorf("query drive_health: %w", err)
	}
	defer rows.Close()

	var healthRows []driveHealthRow
	for rows.Next() {
		var row driveHealthRow
		if err := rows.Scan(&row.DriveID, &row.SampleID, &row.Status, &row.Score, &row.Reasons); err != nil {
			return nil, fmt.Errorf("scan drive_health row: %w", err)
		}
		healthRows = append(healthRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate drive_health rows: %w", err)
	}
	return healthRows, nil
}

func loadSmartTestRunRows(ctx context.Context, tx *sql.Tx) ([]smartTestRunRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, drive_id, test_type, scheduled_at, started_at, finished_at, status, message
		FROM smart_test_runs
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query smart_test_runs: %w", err)
	}
	defer rows.Close()

	var runs []smartTestRunRow
	for rows.Next() {
		var row smartTestRunRow
		if err := rows.Scan(&row.ID, &row.DriveID, &row.TestType, &row.ScheduledAt, &row.StartedAt, &row.FinishedAt, &row.Status, &row.Message); err != nil {
			return nil, fmt.Errorf("scan smart_test_runs row: %w", err)
		}
		runs = append(runs, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate smart_test_runs rows: %w", err)
	}
	return runs, nil
}

func nullStringValue(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func groupDriveRows(drives []driveRow) ([]driveDuplicateGroup, error) {
	uf := newUnionFind(len(drives))
	aliasOwner := make(map[string]int)

	for i, row := range drives {
		for _, alias := range driveStrongAliases(row) {
			if other, ok := aliasOwner[alias]; ok {
				uf.union(i, other)
			} else {
				aliasOwner[alias] = i
			}
		}
	}

	grouped := make(map[int][]driveRow)
	for i, row := range drives {
		grouped[uf.find(i)] = append(grouped[uf.find(i)], row)
	}

	roots := make([]int, 0, len(grouped))
	for root := range grouped {
		roots = append(roots, root)
	}
	sort.Ints(roots)

	groups := make([]driveDuplicateGroup, 0, len(roots))
	for _, root := range roots {
		group, err := buildDriveDuplicateGroup(grouped[root])
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func driveStrongAliases(row driveRow) []string {
	aliases := make([]string, 0, 2)
	if wwn := normalizeIdentityPart(row.WWN); wwn != "" {
		aliases = append(aliases, "wwn:"+wwn)
	}
	serial := normalizeIdentityPart(row.Serial)
	model := normalizeIdentityPart(row.Model)
	if serial != "" && model != "" {
		aliases = append(aliases, "serial-model:"+serial+"|"+model)
	}
	return aliases
}

func buildDriveDuplicateGroup(rows []driveRow) (driveDuplicateGroup, error) {
	canonical := rows[0]
	for _, row := range rows[1:] {
		if shouldReplaceCanonicalDrive(canonical, row) {
			canonical = row
		}
	}

	merged := canonical
	merged.FirstSeenAt = canonical.FirstSeenAt
	merged.LastSeenAt = canonical.LastSeenAt

	deviceChoice := latestNonEmptyChoice(canonical.Device, canonical.LastSeenAt, canonical.ID)
	modelChoice := latestNonEmptyChoice(canonical.Model, canonical.LastSeenAt, canonical.ID)
	serialChoice := latestNonEmptyChoice(canonical.Serial, canonical.LastSeenAt, canonical.ID)
	wwnChoice := latestNonEmptyChoice(canonical.WWN, canonical.LastSeenAt, canonical.ID)

	memberIDs := make([]int64, 0, len(rows))
	duplicates := make([]int64, 0, len(rows)-1)
	for _, row := range rows {
		memberIDs = append(memberIDs, row.ID)
		if row.FirstSeenAt.Before(merged.FirstSeenAt) {
			merged.FirstSeenAt = row.FirstSeenAt
		}
		if row.LastSeenAt.After(merged.LastSeenAt) {
			merged.LastSeenAt = row.LastSeenAt
		}
		deviceChoice = chooseLatestNonEmpty(deviceChoice, row.Device, row.LastSeenAt, row.ID)
		modelChoice = chooseLatestNonEmpty(modelChoice, row.Model, row.LastSeenAt, row.ID)
		serialChoice = chooseLatestNonEmpty(serialChoice, row.Serial, row.LastSeenAt, row.ID)
		wwnChoice = chooseLatestNonEmpty(wwnChoice, row.WWN, row.LastSeenAt, row.ID)
		if row.ID != canonical.ID {
			duplicates = append(duplicates, row.ID)
		}
	}

	sort.Slice(memberIDs, func(i, j int) bool { return memberIDs[i] < memberIDs[j] })
	sort.Slice(duplicates, func(i, j int) bool { return duplicates[i] < duplicates[j] })

	merged.Device = deviceChoice.value
	merged.Model = modelChoice.value
	merged.Serial = serialChoice.value
	merged.WWN = wwnChoice.value
	merged.IdentityKey = driveIdentityKey(smart.DriveInfo{Device: merged.Device, Model: merged.Model, Serial: merged.Serial, WWN: merged.WWN})
	if merged.IdentityKey == "" {
		return driveDuplicateGroup{}, fmt.Errorf("drive %d has empty derived identity key", canonical.ID)
	}

	return driveDuplicateGroup{
		canonicalID: canonical.ID,
		memberIDs:   memberIDs,
		current:     canonical,
		merged:      merged,
		duplicates:  duplicates,
	}, nil
}

func shouldReplaceCanonicalDrive(current, candidate driveRow) bool {
	if candidate.LastSeenAt.After(current.LastSeenAt) {
		return true
	}
	if candidate.LastSeenAt.Equal(current.LastSeenAt) && candidate.ID < current.ID {
		return true
	}
	return false
}

type fieldChoice struct {
	value  string
	seenAt time.Time
	id     int64
	set    bool
}

func latestNonEmptyChoice(value string, seenAt time.Time, id int64) fieldChoice {
	return chooseLatestNonEmpty(fieldChoice{}, value, seenAt, id)
}

func chooseLatestNonEmpty(current fieldChoice, value string, seenAt time.Time, id int64) fieldChoice {
	if strings.TrimSpace(value) == "" {
		return current
	}
	if !current.set || seenAt.After(current.seenAt) || (seenAt.Equal(current.seenAt) && id < current.id) {
		return fieldChoice{value: value, seenAt: seenAt, id: id, set: true}
	}
	return current
}

func applyDriveMigrationPlan(ctx context.Context, tx *sql.Tx, plan driveMigrationPlan) error {
	if plan.rebuildDrives {
		var err error
		plan, err = rewriteDuplicateDriveChildren(ctx, tx, plan)
		if err != nil {
			return err
		}
	} else {
		for _, group := range plan.groups {
			if driveRowsEqual(group.current, group.merged) {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE drives
				SET device = ?, identity_key = ?, model = ?, serial = ?, wwn = ?, first_seen_at = ?, last_seen_at = ?
				WHERE id = ?
			`, group.merged.Device, group.merged.IdentityKey, group.merged.Model, group.merged.Serial, group.merged.WWN, group.merged.FirstSeenAt, group.merged.LastSeenAt, group.canonicalID); err != nil {
				return fmt.Errorf("update canonical drive %d: %w", group.canonicalID, err)
			}
		}
	}

	if plan.rebuildDrives {
		if err := restoreDuplicateDriveChildren(ctx, tx, plan); err != nil {
			return err
		}
	}

	return nil
}

func driveRowsEqual(current, merged driveRow) bool {
	return current.ID == merged.ID &&
		current.Device == merged.Device &&
		current.IdentityKey == merged.IdentityKey &&
		current.Model == merged.Model &&
		current.Serial == merged.Serial &&
		current.WWN == merged.WWN &&
		current.FirstSeenAt.Equal(merged.FirstSeenAt) &&
		current.LastSeenAt.Equal(merged.LastSeenAt)
}

func rewriteDuplicateDriveChildren(ctx context.Context, tx *sql.Tx, plan driveMigrationPlan) (driveMigrationPlan, error) {
	duplicateToCanonical := make(map[int64]int64)
	for _, group := range plan.duplicates {
		for _, duplicateID := range group.duplicates {
			duplicateToCanonical[duplicateID] = group.canonicalID
		}
	}

	for i := range plan.sampleRows {
		if canonicalID, ok := duplicateToCanonical[plan.sampleRows[i].DriveID]; ok {
			plan.sampleRows[i].DriveID = canonicalID
		}
	}
	for i := range plan.healthRows {
		if canonicalID, ok := duplicateToCanonical[plan.healthRows[i].DriveID]; ok {
			plan.healthRows[i].DriveID = canonicalID
		}
	}
	for i := range plan.testRunRows {
		if canonicalID, ok := duplicateToCanonical[plan.testRunRows[i].DriveID]; ok {
			plan.testRunRows[i].DriveID = canonicalID
		}
	}

	stateRows := mergeNotificationStates(plan.duplicates, plan.stateRows)
	plan.stateRows = stateRows

	for _, stmt := range []string{
		`DROP TABLE smart_attributes`,
		`DROP TABLE drive_health`,
		`DROP TABLE smart_test_runs`,
		`DROP TABLE notification_state`,
		`DROP TABLE smart_samples`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return driveMigrationPlan{}, fmt.Errorf("rebuild child tables (%s): %w", stmt, err)
		}
	}

	for _, group := range plan.duplicates {
		for _, duplicateID := range group.duplicates {
			_ = duplicateID
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE drives`); err != nil {
		return driveMigrationPlan{}, fmt.Errorf("drop legacy drives table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE drives (
			id BIGINT PRIMARY KEY,
			device TEXT NOT NULL,
			identity_key TEXT NOT NULL CHECK (identity_key <> ''),
			model TEXT,
			serial TEXT,
			wwn TEXT,
			first_seen_at TIMESTAMP NOT NULL,
			last_seen_at TIMESTAMP NOT NULL
		)
	`); err != nil {
		return driveMigrationPlan{}, fmt.Errorf("create migrated drives table: %w", err)
	}
	for _, group := range plan.groups {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO drives (id, device, identity_key, model, serial, wwn, first_seen_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, group.canonicalID, group.merged.Device, group.merged.IdentityKey, group.merged.Model, group.merged.Serial, group.merged.WWN, group.merged.FirstSeenAt, group.merged.LastSeenAt); err != nil {
			return driveMigrationPlan{}, fmt.Errorf("insert migrated drive %d: %w", group.canonicalID, err)
		}
	}

	return plan, nil
}
func restoreDuplicateDriveChildren(ctx context.Context, tx *sql.Tx, plan driveMigrationPlan) error {
	if err := recreateDriveChildTables(ctx, tx); err != nil {
		return err
	}

	for _, row := range plan.sampleRows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO smart_samples (
				id, drive_id, collected_at, temperature, power_on_hours,
				reallocated_sectors, pending_sectors, uncorrectable_sectors, wear_level, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, row.ID, row.DriveID, row.CollectedAt, row.Temperature, row.PowerOnHours, row.ReallocatedSectors, row.PendingSectors, row.UncorrectableSectors, row.WearLevel, row.RawJSON); err != nil {
			return fmt.Errorf("reinsert smart_sample %d: %w", row.ID, err)
		}
	}
	for _, row := range plan.attributeRows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO smart_attributes (sample_id, attribute_id, name, value, worst, threshold, raw)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, row.SampleID, row.AttributeID, row.Name, row.Value, row.Worst, row.Threshold, row.Raw); err != nil {
			return fmt.Errorf("reinsert smart_attribute sample=%d attr=%d: %w", row.SampleID, row.AttributeID, err)
		}
	}
	for _, row := range plan.healthRows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO drive_health (drive_id, sample_id, status, score, reasons)
			VALUES (?, ?, ?, ?, ?)
		`, row.DriveID, row.SampleID, row.Status, row.Score, row.Reasons); err != nil {
			return fmt.Errorf("reinsert drive_health sample=%d drive=%d: %w", row.SampleID, row.DriveID, err)
		}
	}
	for _, row := range plan.testRunRows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO smart_test_runs (id, drive_id, test_type, scheduled_at, started_at, finished_at, status, message)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, row.ID, row.DriveID, row.TestType, row.ScheduledAt, row.StartedAt, row.FinishedAt, row.Status, row.Message); err != nil {
			return fmt.Errorf("reinsert smart_test_run %d: %w", row.ID, err)
		}
	}
	for _, row := range plan.stateRows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_state (drive_id, notification_name, state, updated_at)
			VALUES (?, ?, ?, ?)
		`, row.DriveID, row.NotificationName, row.State, row.UpdatedAt); err != nil {
			return fmt.Errorf("reinsert notification_state %q drive=%d: %w", row.NotificationName, row.DriveID, err)
		}
	}

	return nil
}

func recreateDriveChildTables(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`CREATE TABLE smart_samples (
			id BIGINT PRIMARY KEY,
			drive_id BIGINT NOT NULL,
			collected_at TIMESTAMP NOT NULL,
			temperature INTEGER,
			power_on_hours BIGINT,
			reallocated_sectors BIGINT,
			pending_sectors BIGINT,
			uncorrectable_sectors BIGINT,
			wear_level BIGINT,
			raw_json JSON
		)`,
		`CREATE TABLE smart_attributes (
			sample_id BIGINT NOT NULL,
			attribute_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			value INTEGER,
			worst INTEGER,
			threshold INTEGER,
			raw TEXT,
			FOREIGN KEY (sample_id) REFERENCES smart_samples(id)
		)`,
		`CREATE TABLE drive_health (
			drive_id BIGINT NOT NULL,
			sample_id BIGINT NOT NULL,
			status TEXT NOT NULL,
			score INTEGER NOT NULL,
			reasons TEXT,
			FOREIGN KEY (sample_id) REFERENCES smart_samples(id)
		)`,
		`CREATE TABLE smart_test_runs (
			id BIGINT PRIMARY KEY,
			drive_id BIGINT NOT NULL,
			test_type TEXT NOT NULL,
			scheduled_at TIMESTAMP NOT NULL,
			started_at TIMESTAMP NOT NULL,
			finished_at TIMESTAMP NOT NULL,
			status TEXT NOT NULL,
			message TEXT
		)`,
		`CREATE TABLE notification_state (
			drive_id BIGINT NOT NULL,
			notification_name TEXT NOT NULL,
			state TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (drive_id, notification_name)
		)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("recreate child table: %w", err)
		}
	}
	return nil
}

func mergeNotificationStates(groups []driveDuplicateGroup, states []notificationStateRow) []notificationStateRow {
	stateByDrive := make(map[int64][]notificationStateRow)
	for _, row := range states {
		stateByDrive[row.DriveID] = append(stateByDrive[row.DriveID], row)
	}

	consumed := make(map[int64]bool)
	resolved := make([]notificationStateRow, 0, len(states))
	for _, group := range groups {
		consumed[group.canonicalID] = true
		for _, duplicateID := range group.duplicates {
			consumed[duplicateID] = true
		}
		resolved = append(resolved, mergeNotificationStateGroup(group, stateByDrive)...)
	}
	for _, row := range states {
		if !consumed[row.DriveID] {
			resolved = append(resolved, row)
		}
	}

	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].DriveID != resolved[j].DriveID {
			return resolved[i].DriveID < resolved[j].DriveID
		}
		return resolved[i].NotificationName < resolved[j].NotificationName
	})
	return resolved
}

func mergeNotificationStateGroup(group driveDuplicateGroup, stateByDrive map[int64][]notificationStateRow) []notificationStateRow {
	resolved := make(map[string]notificationStateRow)
	for _, driveID := range group.memberIDs {
		for _, row := range stateByDrive[driveID] {
			winner, ok := resolved[row.NotificationName]
			if !ok || row.UpdatedAt.After(winner.UpdatedAt) || (row.UpdatedAt.Equal(winner.UpdatedAt) && row.DriveID < winner.DriveID) {
				row.DriveID = group.canonicalID
				resolved[row.NotificationName] = row
			}
		}
	}

	names := make([]string, 0, len(resolved))
	for name := range resolved {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]notificationStateRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, resolved[name])
	}
	return rows
}

type unionFind struct {
	parent []int
	rank   []int
}

func newUnionFind(size int) *unionFind {
	parent := make([]int, size)
	for i := range parent {
		parent[i] = i
	}
	return &unionFind{parent: parent, rank: make([]int, size)}
}

func (u *unionFind) find(x int) int {
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x])
	}
	return u.parent[x]
}

func (u *unionFind) union(a, b int) {
	ra := u.find(a)
	rb := u.find(b)
	if ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
}
