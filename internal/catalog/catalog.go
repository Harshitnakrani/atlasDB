package catalog

import (
	"encoding/json"
	"fmt"
	"sync"

	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
)

// ColumnDef defines schema metadata for a table column.
type ColumnDef struct {
	Name       string          `json:"name"`
	Type       common.DataType `json:"type"`
	PrimaryKey bool            `json:"primary_key"`
	Nullable   bool            `json:"nullable"`
}

// TableDef defines schema metadata for a table.
type TableDef struct {
	Name       string          `json:"name"`
	RootPageID common.PageID   `json:"root_page_id"`
	NextRowID  int64           `json:"next_row_id"`
	Columns    []ColumnDef     `json:"columns"`
	ColIndex   map[string]int  `json:"-"`
}

func (td *TableDef) FindColumn(name string) (int, *ColumnDef, bool) {
	for i, col := range td.Columns {
		if col.Name == name {
			return i, &td.Columns[i], true
		}
	}
	return -1, nil, false
}

// IndexDef defines schema metadata for a secondary index.
type IndexDef struct {
	Name       string        `json:"name"`
	TableName  string        `json:"table_name"`
	ColumnName string        `json:"column_name"`
	RootPageID common.PageID `json:"root_page_id"`
	Unique     bool          `json:"unique"`
}

// CatalogData is the persistent representation stored on disk.
type CatalogData struct {
	Tables  []*TableDef `json:"tables"`
	Indexes []*IndexDef `json:"indexes"`
}

// Catalog manages database schema definitions and root page bindings.
type Catalog struct {
	mu          sync.RWMutex
	bp          *buffer.BufferPool
	catalogPgID common.PageID
	tables      map[string]*TableDef
	indexes     map[string]*IndexDef
}

// LoadCatalog initializes or restores the catalog from its designated page.
func LoadCatalog(bp *buffer.BufferPool, catalogPgID common.PageID) (*Catalog, error) {
	c := &Catalog{
		bp:          bp,
		catalogPgID: catalogPgID,
		tables:      make(map[string]*TableDef),
		indexes:     make(map[string]*IndexDef),
	}

	frame, err := bp.FetchPage(catalogPgID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch catalog page %d: %w", catalogPgID, err)
	}
	defer bp.Unpin(catalogPgID, false)

	// Check if page has catalog data (magic check or json start)
	// We check if first byte is non-zero
	if frame.Data[0] == '{' {
		// Find null terminator or json length
		var dataLen int
		for i, b := range frame.Data {
			if b == 0 {
				dataLen = i
				break
			}
		}
		if dataLen == 0 {
			dataLen = len(frame.Data)
		}

		var cd CatalogData
		if err := json.Unmarshal(frame.Data[:dataLen], &cd); err == nil {
			for _, tbl := range cd.Tables {
				tbl.ColIndex = make(map[string]int)
				for idx, col := range tbl.Columns {
					tbl.ColIndex[col.Name] = idx
				}
				c.tables[tbl.Name] = tbl
			}
			for _, idx := range cd.Indexes {
				c.indexes[idx.Name] = idx
			}
		}
	}

	return c, nil
}

// Save persists the current catalog schema to disk.
func (c *Catalog) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cd := CatalogData{
		Tables:  make([]*TableDef, 0, len(c.tables)),
		Indexes: make([]*IndexDef, 0, len(c.indexes)),
	}
	for _, tbl := range c.tables {
		cd.Tables = append(cd.Tables, tbl)
	}
	for _, idx := range c.indexes {
		cd.Indexes = append(cd.Indexes, idx)
	}

	bytesData, err := json.Marshal(cd)
	if err != nil {
		return err
	}

	frame, err := c.bp.FetchPage(c.catalogPgID)
	if err != nil {
		return err
	}
	defer c.bp.Unpin(c.catalogPgID, true)

	if len(bytesData) > len(frame.Data) {
		return fmt.Errorf("catalog data size exceeds single page capacity")
	}

	// Zero frame and copy new catalog json
	for i := range frame.Data {
		frame.Data[i] = 0
	}
	copy(frame.Data, bytesData)

	return nil
}

// CreateTable adds a new table schema to the catalog.
func (c *Catalog) CreateTable(name string, cols []ColumnDef, rootPgID common.PageID) (*TableDef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.tables[name]; exists {
		return nil, common.ErrTableExists
	}

	colIdx := make(map[string]int, len(cols))
	for i, col := range cols {
		if _, exists := colIdx[col.Name]; exists {
			return nil, fmt.Errorf("duplicate column name: %s", col.Name)
		}
		colIdx[col.Name] = i
	}

	tbl := &TableDef{
		Name:       name,
		RootPageID: rootPgID,
		NextRowID:  1,
		Columns:    cols,
		ColIndex:   colIdx,
	}

	c.tables[name] = tbl
	return tbl, nil
}

// GetTable retrieves a table definition by name.
func (c *Catalog) GetTable(name string) (*TableDef, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tbl, exists := c.tables[name]
	return tbl, exists
}

// NextRowID generates and increments the next available RowID for a table.
func (c *Catalog) NextRowID(tableName string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tbl, exists := c.tables[tableName]
	if !exists {
		return 0, common.ErrTableNotFound
	}

	id := tbl.NextRowID
	tbl.NextRowID++
	return id, nil
}

// CreateIndex adds a new index metadata entry.
func (c *Catalog) CreateIndex(name, tableName, colName string, rootPgID common.PageID, unique bool) (*IndexDef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.indexes[name]; exists {
		return nil, common.ErrIndexExists
	}

	tbl, exists := c.tables[tableName]
	if !exists {
		return nil, common.ErrTableNotFound
	}

	if _, _, ok := tbl.FindColumn(colName); !ok {
		return nil, common.ErrColumnNotFound
	}

	idx := &IndexDef{
		Name:       name,
		TableName:  tableName,
		ColumnName: colName,
		RootPageID: rootPgID,
		Unique:     unique,
	}

	c.indexes[name] = idx
	return idx, nil
}

// GetIndex returns the index definition by name.
func (c *Catalog) GetIndex(name string) (*IndexDef, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	idx, exists := c.indexes[name]
	return idx, exists
}

// GetTableIndexes returns all indexes created for a table.
func (c *Catalog) GetTableIndexes(tableName string) []*IndexDef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var res []*IndexDef
	for _, idx := range c.indexes {
		if idx.TableName == tableName {
			res = append(res, idx)
		}
	}
	return res
}

// DropTable removes a table schema and its associated indexes from the catalog.
func (c *Catalog) DropTable(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.tables[name]; !exists {
		return common.ErrTableNotFound
	}

	delete(c.tables, name)
	// Remove associated indexes
	for idxName, idx := range c.indexes {
		if idx.TableName == name {
			delete(c.indexes, idxName)
		}
	}
	return nil
}

// DropIndex removes an index schema from the catalog.
func (c *Catalog) DropIndex(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.indexes[name]; !exists {
		return common.ErrIndexNotFound
	}
	delete(c.indexes, name)
	return nil
}

// AddColumn appends a new column to a table schema.
func (c *Catalog) AddColumn(tableName string, col ColumnDef) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tbl, exists := c.tables[tableName]
	if !exists {
		return common.ErrTableNotFound
	}

	if _, exists := tbl.ColIndex[col.Name]; exists {
		return fmt.Errorf("column %s already exists in %s", col.Name, tableName)
	}

	tbl.ColIndex[col.Name] = len(tbl.Columns)
	tbl.Columns = append(tbl.Columns, col)
	return nil
}

// TruncateTable resets the table's NextRowID counter.
func (c *Catalog) TruncateTable(name string) (*TableDef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tbl, exists := c.tables[name]
	if !exists {
		return nil, common.ErrTableNotFound
	}

	tbl.NextRowID = 1
	return tbl, nil
}

// ListTables returns all table definitions in the catalog.
func (c *Catalog) ListTables() []*TableDef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make([]*TableDef, 0, len(c.tables))
	for _, tbl := range c.tables {
		res = append(res, tbl)
	}
	return res
}
