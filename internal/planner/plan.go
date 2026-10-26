package planner

import (
	"fmt"

	"atlasDB/internal/catalog"
	"atlasDB/internal/common"
	"atlasDB/internal/parser"
)

type PlanType int

const (
	PlanTypeCreateTable PlanType = iota
	PlanTypeCreateIndex
	PlanTypeInsert
	PlanTypeTableScan
	PlanTypeIndexScan
	PlanTypePointLookup
	PlanTypeFilter
	PlanTypeProject
	PlanTypeOrderBy
	PlanTypeLimit
	PlanTypeUpdate
	PlanTypeDelete
	PlanTypeBegin
	PlanTypeCommit
	PlanTypeRollback

	// Extended Plan Types
	PlanTypeJoin
	PlanTypeAggregate
	PlanTypeDistinct
	PlanTypeDropTable
	PlanTypeDropIndex
	PlanTypeTruncateTable
	PlanTypeAlterTable
)

// Plan is the base interface for execution plan nodes.
type Plan interface {
	Type() PlanType
	String() string
}

type CreateTablePlan struct {
	Table       string
	Columns     []catalog.ColumnDef
	IfNotExists bool
}
func (p *CreateTablePlan) Type() PlanType { return PlanTypeCreateTable }
func (p *CreateTablePlan) String() string { return fmt.Sprintf("CreateTable(%s)", p.Table) }

type CreateIndexPlan struct {
	IndexName string
	Table     string
	Column    string
	Unique    bool
}
func (p *CreateIndexPlan) Type() PlanType { return PlanTypeCreateIndex }
func (p *CreateIndexPlan) String() string { return fmt.Sprintf("CreateIndex(%s on %s.%s)", p.IndexName, p.Table, p.Column) }

type DropTablePlan struct {
	Table    string
	IfExists bool
}
func (p *DropTablePlan) Type() PlanType { return PlanTypeDropTable }
func (p *DropTablePlan) String() string { return fmt.Sprintf("DropTable(%s)", p.Table) }

type DropIndexPlan struct {
	IndexName string
	IfExists  bool
}
func (p *DropIndexPlan) Type() PlanType { return PlanTypeDropIndex }
func (p *DropIndexPlan) String() string { return fmt.Sprintf("DropIndex(%s)", p.IndexName) }

type TruncateTablePlan struct {
	Table string
}
func (p *TruncateTablePlan) Type() PlanType { return PlanTypeTruncateTable }
func (p *TruncateTablePlan) String() string { return fmt.Sprintf("TruncateTable(%s)", p.Table) }

type AlterTablePlan struct {
	Table     string
	AddColumn catalog.ColumnDef
}
func (p *AlterTablePlan) Type() PlanType { return PlanTypeAlterTable }
func (p *AlterTablePlan) String() string { return fmt.Sprintf("AlterTable(%s ADD %s)", p.Table, p.AddColumn.Name) }

type InsertPlan struct {
	Table   *catalog.TableDef
	Columns []string
	Values  [][]common.Value
}
func (p *InsertPlan) Type() PlanType { return PlanTypeInsert }
func (p *InsertPlan) String() string { return fmt.Sprintf("Insert(%s, %d rows)", p.Table.Name, len(p.Values)) }

type TableScanPlan struct {
	Table *catalog.TableDef
}
func (p *TableScanPlan) Type() PlanType { return PlanTypeTableScan }
func (p *TableScanPlan) String() string { return fmt.Sprintf("TableScan(%s)", p.Table.Name) }

type IndexScanPlan struct {
	Table     *catalog.TableDef
	Index     *catalog.IndexDef
	ColName   string
	TargetVal common.Value
	IsPK      bool
}
func (p *IndexScanPlan) Type() PlanType { return PlanTypeIndexScan }
func (p *IndexScanPlan) String() string {
	return fmt.Sprintf("IndexScan(%s.%s)", p.Table.Name, p.ColName)
}

type JoinPlan struct {
	Left       Plan
	Right      Plan
	JoinType   parser.JoinType
	LeftTable  *catalog.TableDef
	RightTable *catalog.TableDef
	On         parser.Expr
}
func (p *JoinPlan) Type() PlanType { return PlanTypeJoin }
func (p *JoinPlan) String() string {
	return fmt.Sprintf("Join(%s, %s)", p.LeftTable.Name, p.RightTable.Name)
}

type FilterPlan struct {
	Child Plan
	Where parser.Expr
	Table *catalog.TableDef
}
func (p *FilterPlan) Type() PlanType { return PlanTypeFilter }
func (p *FilterPlan) String() string { return fmt.Sprintf("Filter(%v)", p.Child) }

type ProjectPlan struct {
	Child       Plan
	Table       *catalog.TableDef
	ColIndices  []int
	OutputNames []string
}
func (p *ProjectPlan) Type() PlanType { return PlanTypeProject }
func (p *ProjectPlan) String() string { return fmt.Sprintf("Project(%v)", p.OutputNames) }

type AggregateItem struct {
	Func       string // COUNT, SUM, AVG, MIN, MAX
	ColIdx     int    // -1 for COUNT(*)
	ColName    string
	OutputName string
}

type AggregatePlan struct {
	Child       Plan
	Table       *catalog.TableDef
	Aggs        []AggregateItem
	GroupBy     []int
	GroupCols   []string
	Having      parser.Expr
	OutputNames []string
}
func (p *AggregatePlan) Type() PlanType { return PlanTypeAggregate }
func (p *AggregatePlan) String() string { return fmt.Sprintf("Aggregate(%d aggs, %d group)", len(p.Aggs), len(p.GroupBy)) }

type DistinctPlan struct {
	Child Plan
}
func (p *DistinctPlan) Type() PlanType { return PlanTypeDistinct }
func (p *DistinctPlan) String() string { return fmt.Sprintf("Distinct(%v)", p.Child) }

type OrderByPlan struct {
	Child   Plan
	Table   *catalog.TableDef
	ColIdx  int
	ColName string
	Desc    bool
}
func (p *OrderByPlan) Type() PlanType { return PlanTypeOrderBy }
func (p *OrderByPlan) String() string {
	order := "ASC"
	if p.Desc {
		order = "DESC"
	}
	return fmt.Sprintf("OrderBy(%s %s)", p.ColName, order)
}

type LimitPlan struct {
	Child  Plan
	Limit  int
	Offset int
}
func (p *LimitPlan) Type() PlanType { return PlanTypeLimit }
func (p *LimitPlan) String() string { return fmt.Sprintf("Limit(%d, %d)", p.Offset, p.Limit) }

type UpdatePlan struct {
	Table       *catalog.TableDef
	Scan        Plan
	Assignments map[int]common.Value
}
func (p *UpdatePlan) Type() PlanType { return PlanTypeUpdate }
func (p *UpdatePlan) String() string { return fmt.Sprintf("Update(%s)", p.Table.Name) }

type DeletePlan struct {
	Table *catalog.TableDef
	Scan  Plan
}
func (p *DeletePlan) Type() PlanType { return PlanTypeDelete }
func (p *DeletePlan) String() string { return fmt.Sprintf("Delete(%s)", p.Table.Name) }

type BeginPlan struct{}
func (p *BeginPlan) Type() PlanType { return PlanTypeBegin }
func (p *BeginPlan) String() string { return "Begin" }

type CommitPlan struct{}
func (p *CommitPlan) Type() PlanType { return PlanTypeCommit }
func (p *CommitPlan) String() string { return "Commit" }

type RollbackPlan struct{}
func (p *RollbackPlan) Type() PlanType { return PlanTypeRollback }
func (p *RollbackPlan) String() string { return "Rollback" }
