package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"

	"github.com/stolostron/multicluster-global-hub/pkg/conflator/db/postgres/batch"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
)

var (
	errBatchDoesNotMatchPostgreSQL = errors.New("given batch doesn't match postgresql library")
	errBatchFailed                 = errors.New("some of the batch statements failed to execute")
)

// NewStatusPostgreSQL creates a new instance of PostgreSQL object.
func NewStatusPostgreSQL(ctx context.Context, dataConfig *database.DatabaseConfig) (*PostgreSQL, error) {
	dbConnectionPool, err := database.PostgresConnPool(ctx, dataConfig.URL, dataConfig.CaCertPath,
		int32(dataConfig.PoolSize))
	if err != nil {
		return nil, fmt.Errorf("unable to connect to status db: %w", err)
	}

	return &PostgreSQL{conn: dbConnectionPool}, nil
}

// PostgreSQL abstracts management of PostgreSQL client.
type PostgreSQL struct {
	conn *pgxpool.Pool
}

// Stop function stops PostgreSQL client.
func (p *PostgreSQL) Stop() {
	p.conn.Close()
}

// GetPoolSize returns the max number of connections.
func (p *PostgreSQL) GetPoolSize() int32 {
	return p.conn.Config().MaxConns
}

// SendBatch sends a batch operation to the db and returns list of errors if there were any.
func (p *PostgreSQL) SendBatch(ctx context.Context, batch interface{}) error {
	postgreSQLBatch, ok := batch.(*pgx.Batch)
	if !ok {
		return fmt.Errorf("failed to send batch - %w", errBatchDoesNotMatchPostgreSQL)
	}

	if postgreSQLBatch.Len() == 0 { // no statements in the batch
		return nil // then, there is no error
	}

	batchResult := p.conn.SendBatch(ctx, postgreSQLBatch)
	defer batchResult.Close()

	errorStringBuilder := strings.Builder{}

	for i := 0; i < postgreSQLBatch.Len(); i++ {
		_, err := batchResult.Exec()
		if err != nil {
			errorStringBuilder.WriteString(fmt.Errorf(
				"failed to execute batch statement %w, ", err).Error())
		}
	}

	errorString := errorStringBuilder.String()
	if len(errorString) > 0 {
		return fmt.Errorf("%w - %s", errBatchFailed, errorString)
	}

	return nil
}

// NewManagedClustersBatchBuilder creates a new instance of ManagedClustersBatchBuilder.
func (p *PostgreSQL) NewManagedClustersBatchBuilder(schema string, tableName string,
	leafHubName string,
) ManagedClustersBatchBuilder {
	return batch.NewManagedClustersBatchBuilder(schema, tableName, leafHubName)
}

// GetManagedClustersByLeafHub returns list of managed clusters and for each managed cluster it's resourceVersion.
func (p *PostgreSQL) GetManagedClustersByLeafHub(ctx context.Context, schema string, tableName string,
	leafHubName string,
) (map[string]string, error) {
	rows, _ := p.conn.Query(ctx, fmt.Sprintf(`SELECT payload->'metadata'->>'name',
		payload->'metadata'->>'resourceVersion' FROM %s.%s WHERE leaf_hub_name=$1`, schema, tableName), leafHubName)

	result, err := buildKeyValueMapFromRows(rows)
	if err != nil {
		return nil, fmt.Errorf("failed reading from table %s.%s - %w", schema, tableName, err)
	}

	return result, nil
}

// NewPoliciesBatchBuilder creates a new instance of PoliciesBatchBuilder.
func (p *PostgreSQL) NewPoliciesBatchBuilder(schema string, tableName string,
	leafHubName string,
) PoliciesBatchBuilder {
	return batch.NewPoliciesBatchBuilder(schema, tableName, leafHubName)
}

// NewGenericBatchBuilder creates a new instance of GenericBatchBuilder.
func (p *PostgreSQL) NewGenericBatchBuilder(schema string, tableName string,
	leafHubName string,
) GenericBatchBuilder {
	return batch.NewGenericBatchBuilder(schema, tableName, leafHubName)
}

// GetResourceIDToVersionByLeafHub returns a map from resource id to its resourceVersion.
func (p *PostgreSQL) GetResourceIDToVersionByLeafHub(ctx context.Context, schema string, tableName string,
	leafHubName string,
) (map[string]string, error) {
	rows, _ := p.conn.Query(ctx, fmt.Sprintf(`SELECT id,
		payload->'metadata'->>'resourceVersion' FROM %s.%s WHERE leaf_hub_name=$1`, schema, tableName), leafHubName)

	result, err := buildKeyValueMapFromRows(rows)
	if err != nil {
		return nil, fmt.Errorf("failed reading from table %s.%s - %w", schema, tableName, err)
	}

	return result, nil
}

// NewGenericLocalBatchBuilder creates a new instance of GenericLocalBatchBuilder.
func (p *PostgreSQL) NewGenericLocalBatchBuilder(schema string, tableName string,
	leafHubName string,
) GenericLocalBatchBuilder {
	return batch.NewGenericLocalBatchBuilder(schema, tableName, leafHubName)
}

// GetLocalResourceIDToVersionByLeafHub returns a map from resource id to its resourceVersion.
func (p *PostgreSQL) GetLocalResourceIDToVersionByLeafHub(ctx context.Context, schema string, tableName string,
	leafHubName string,
) (map[string]string, error) {
	rows, _ := p.conn.Query(ctx, fmt.Sprintf(`SELECT payload->'metadata'->>'uid',
		payload->'metadata'->>'resourceVersion' FROM %s.%s WHERE leaf_hub_name=$1`, schema, tableName), leafHubName)

	result, err := buildKeyValueMapFromRows(rows)
	if err != nil {
		return nil, fmt.Errorf("failed generating map from db - %w", err)
	}

	return result, nil
}

// UpdateHeartbeat inserts or updates heartbeat for a leaf hub.
func (p *PostgreSQL) UpdateHeartbeat(ctx context.Context, schema string, tableName string, leafHubName string) error {
	if _, err := p.conn.Exec(ctx, fmt.Sprintf(`INSERT INTO %[1]s.%[2]s (leaf_hub_name, last_timestamp) 
		values($1, (now() at time zone 'utc')) ON CONFLICT (leaf_hub_name) DO UPDATE SET last_timestamp = (now() 
		at time zone 'utc') WHERE %[1]s.%[2]s.leaf_hub_name = $1`, schema, tableName),
		leafHubName); err != nil {
		return fmt.Errorf("failed upsert into database: %w", err)
	}

	return nil
}

func buildKeyValueMapFromRows(rows pgx.Rows) (map[string]string, error) {
	result := make(map[string]string)

	for rows.Next() {
		var key, val string

		if err := rows.Scan(&key, &val); err != nil {
			return nil, fmt.Errorf("error creating key value map from rows - %w", err)
		}

		result[key] = val
	}

	return result, nil
}
