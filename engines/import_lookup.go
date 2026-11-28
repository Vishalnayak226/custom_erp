package engines

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

// 313.2: every import lookup of stored records reads them by primary key
// alone and checks doctype, status and deletion on the rows it gets back.
//
// The lookups used to ask `doctype = $1 AND id = ANY($2)`. When the table's
// figures were taken before a big import, the planner thinks the doctype
// holds almost no rows and reads the doctype index instead of the key: on a
// 10M-row import a 200-id lookup took 49 s (297.2.5). With only `id = ANY`
// in the WHERE clause the key is the one index the query can use, so the
// plan cannot move with the statistics. The same read also sees an id held
// by another record type or by a deleted record, which the old query could
// not.

// importStoredDoc is what an import needs to know about one stored record.
type importStoredDoc struct {
	doctype   string
	status    string
	hasStatus bool   // status is not NULL
	deleted   bool   // deleted_at is set
	data      []byte // only when the lookup asked for data
}

// live is a record of this doctype that has not been deleted.
func (d importStoredDoc) live(doctype string) bool {
	return d.doctype == doctype && !d.deleted
}

// importQueryer is a *sql.DB or a *sql.Tx.
type importQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

// importDocsByIDSQL is the one lookup statement. table is "documents" in a
// transaction whose search_path is set, or "<schema>.documents".
func importDocsByIDSQL(table string, withData, forUpdate bool) string {
	cols := "id, doctype, status, deleted_at IS NOT NULL, NULL::jsonb"
	if withData {
		cols = "id, doctype, status, deleted_at IS NOT NULL, data"
	}
	s := "SELECT " + cols + " FROM " + table + " WHERE id = ANY($1::text[])"
	if forUpdate {
		s += " FOR UPDATE"
	}
	return s
}

// importDocsByID reads the records with these ids, whatever their doctype,
// deleted ones included. forUpdate locks them until the transaction ends.
func importDocsByID(ctx context.Context, q importQueryer, table string, ids []string, withData, forUpdate bool) (map[string]importStoredDoc, error) {
	out := make(map[string]importStoredDoc, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, importDocsByIDSQL(table, withData, forUpdate), pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var d importStoredDoc
		var status sql.NullString
		if err := rows.Scan(&id, &d.doctype, &status, &d.deleted, &d.data); err != nil {
			return nil, err
		}
		d.status, d.hasStatus = status.String, status.Valid
		out[id] = d
	}
	return out, rows.Err()
}
