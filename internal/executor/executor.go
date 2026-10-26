package executor

import (
	"atlasDB/internal/common"
)

// Row represents a single retrieved or processed database tuple.
type Row struct {
	RowID  int64
	Values []common.Value
}

// Executor defines Volcano-style iterator execution.
type Executor interface {
	Init() error
	Next() (*Row, error)
	Close() error
}

// Result holds the final output of an executed SQL query or command.
type Result struct {
	Columns      []string
	Rows         [][]common.Value
	RowsAffected int64
}
