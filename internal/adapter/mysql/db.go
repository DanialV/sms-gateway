package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
)

const (
	codeDuplicateEntry  = 1062
	codeLockWaitTimeout = 1205
	codeDeadlock        = 1213
	maxTxAttempts       = 3
	maxErrorLength      = 255
)

func Open(ctx context.Context, dsn string, maxConns int) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	var err error
	for range maxTxAttempts {
		if err = runTx(ctx, db, fn); !hasCode(err, codeDeadlock, codeLockWaitTimeout) {
			return err
		}
	}
	return err
}

func runTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func hasCode(err error, codes ...uint16) bool {
	var mysqlErr *gomysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	for _, code := range codes {
		if mysqlErr.Number == code {
			return true
		}
	}
	return false
}

func requireRow(res sql.Result, whenNone error) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return whenNone
	}
	return nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s[:min(len(s), maxErrorLength)]

}
