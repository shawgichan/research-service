package db

import (
	"github.com/shawgichan/research-service/internal/db/sqlc" // Ensure this path is correct

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store defines all functions to execute db queries and transactions
type Store interface {
	sqlc.Querier // Embeds all query methods from sqlc
	// Add transaction methods here if needed, e.g., ExecTx(ctx context.Context, fn func(*sqlc.Queries) error) error
}

// SQLStore provides all functions to execute SQL queries and transactions
type SQLStore struct {
	*sqlc.Queries // Embeds all query methods from generated sqlc code
	db            *pgxpool.Pool
}

// NewStore creates a new Store
func NewStore(db *pgxpool.Pool) Store {
	return &SQLStore{
		Queries: sqlc.New(db), // sqlc.New expects a DBTX, which *pgxpool.Pool implements
		db:      db,
	}
}

// Example of a transaction method (add to Store interface as well)
/*
func (store *SQLStore) ExecTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // Rollback is a no-op if Commit has been called

	q := sqlc.New(tx) // Create new Querier with the transaction
	err = fn(q)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}
*/
