package common

import (
	"errors"
	"fmt"
)

// PageID is a logical identifier for a 4KB disk page.
type PageID uint64

// InvalidPageID represents an unassigned or null page pointer.
const InvalidPageID PageID = 0xFFFFFFFFFFFFFFFF

// TxID is a logical identifier for a transaction.
type TxID uint64

// InvalidTxID represents no transaction or an invalid transaction.
const InvalidTxID TxID = 0

// Standard error definitions for AtlasDB subsystems.
var (
	ErrNotFound         = errors.New("atlas: record not found")
	ErrKeyExists        = errors.New("atlas: duplicate key error")
	ErrPageFull         = errors.New("atlas: page is full")
	ErrTableNotFound    = errors.New("atlas: table not found")
	ErrTableExists      = errors.New("atlas: table already exists")
	ErrIndexNotFound    = errors.New("atlas: index not found")
	ErrIndexExists      = errors.New("atlas: index already exists")
	ErrColumnNotFound   = errors.New("atlas: column not found")
	ErrInvalidSchema    = errors.New("atlas: invalid schema definition")
	ErrInvalidSQL       = errors.New("atlas: invalid SQL syntax")
	ErrTypeMismatch     = errors.New("atlas: data type mismatch")
	ErrConstraintFailed = errors.New("atlas: constraint violation")
	ErrTxActive         = errors.New("atlas: transaction already active")
	ErrNoActiveTx       = errors.New("atlas: no active transaction")
	ErrTxRolledBack     = errors.New("atlas: transaction rolled back")
	ErrCorruptData      = errors.New("atlas: corrupted data detected")
	ErrBufferPoolFull   = errors.New("atlas: buffer pool has no unpinned frames")
)

// DataType represents the logical data type stored in AtlasDB.
type DataType uint8

const (
	TypeNull DataType = iota
	TypeInt           // int64
	TypeFloat         // float64
	TypeText          // string
	TypeBool          // bool
	TypeBlob          // []byte
)

func (t DataType) String() string {
	switch t {
	case TypeNull:
		return "NULL"
	case TypeInt:
		return "INTEGER"
	case TypeFloat:
		return "REAL"
	case TypeText:
		return "TEXT"
	case TypeBool:
		return "BOOLEAN"
	case TypeBlob:
		return "BLOB"
	default:
		return "UNKNOWN"
	}
}

// Value is a dynamic SQL value stored in tuples and indexes.
type Value struct {
	Type  DataType
	Int   int64
	Float float64
	Text  string
	Bool  bool
	Blob  []byte
}

func NullValue() Value {
	return Value{Type: TypeNull}
}

func IntValue(v int64) Value {
	return Value{Type: TypeInt, Int: v}
}

func FloatValue(v float64) Value {
	return Value{Type: TypeFloat, Float: v}
}

func TextValue(v string) Value {
	return Value{Type: TypeText, Text: v}
}

func BoolValue(v bool) Value {
	return Value{Type: TypeBool, Bool: v}
}

func BlobValue(v []byte) Value {
	return Value{Type: TypeBlob, Blob: v}
}

func (v Value) IsNull() bool {
	return v.Type == TypeNull
}

func (v Value) String() string {
	switch v.Type {
	case TypeNull:
		return "NULL"
	case TypeInt:
		return fmt.Sprintf("%d", v.Int)
	case TypeFloat:
		return fmt.Sprintf("%f", v.Float)
	case TypeText:
		return v.Text
	case TypeBool:
		if v.Bool {
			return "TRUE"
		}
		return "FALSE"
	case TypeBlob:
		return fmt.Sprintf("<BLOB %d bytes>", len(v.Blob))
	default:
		return "<UNKNOWN>"
	}
}

// Compare compares two Values. Returns -1 if v < other, 0 if v == other, 1 if v > other.
func (v Value) Compare(other Value) int {
	if v.Type == TypeNull && other.Type == TypeNull {
		return 0
	}
	if v.Type == TypeNull {
		return -1
	}
	if other.Type == TypeNull {
		return 1
	}

	// Compare based on type
	if v.Type == other.Type {
		switch v.Type {
		case TypeInt:
			if v.Int < other.Int {
				return -1
			} else if v.Int > other.Int {
				return 1
			}
			return 0
		case TypeFloat:
			if v.Float < other.Float {
				return -1
			} else if v.Float > other.Float {
				return 1
			}
			return 0
		case TypeText:
			if v.Text < other.Text {
				return -1
			} else if v.Text > other.Text {
				return 1
			}
			return 0
		case TypeBool:
			if !v.Bool && other.Bool {
				return -1
			} else if v.Bool && !other.Bool {
				return 1
			}
			return 0
		case TypeBlob:
			l1, l2 := len(v.Blob), len(other.Blob)
			minLen := l1
			if l2 < minLen {
				minLen = l2
			}
			for i := 0; i < minLen; i++ {
				if v.Blob[i] < other.Blob[i] {
					return -1
				} else if v.Blob[i] > other.Blob[i] {
					return 1
				}
			}
			if l1 < l2 {
				return -1
			} else if l1 > l2 {
				return 1
			}
			return 0
		}
	}

	// Type comparison if types differ (numeric coercion if int vs float)
	if v.Type == TypeInt && other.Type == TypeFloat {
		vf := float64(v.Int)
		if vf < other.Float {
			return -1
		} else if vf > other.Float {
			return 1
		}
		return 0
	}
	if v.Type == TypeFloat && other.Type == TypeInt {
		of := float64(other.Int)
		if v.Float < of {
			return -1
		} else if v.Float > of {
			return 1
		}
		return 0
	}

	// Default ordering by type tag
	if v.Type < other.Type {
		return -1
	}
	return 1
}

// TraceEvent represents an event recorded during query execution for observability.
type TraceEvent struct {
	Subsystem string // e.g. "Parser", "Planner", "BTree", "Buffer", "Executor"
	Message   string
}

// TraceContext tracks observability information across subsystems.
type TraceContext struct {
	Events []TraceEvent
}

func NewTraceContext() *TraceContext {
	return &TraceContext{
		Events: make([]TraceEvent, 0),
	}
}

func (tc *TraceContext) Add(subsystem, format string, args ...any) {
	if tc == nil {
		return
	}
	tc.Events = append(tc.Events, TraceEvent{
		Subsystem: subsystem,
		Message:   fmt.Sprintf(format, args...),
	})
}
