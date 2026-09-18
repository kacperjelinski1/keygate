package store

import (
	"context"

	"github.com/uptrace/bun"
)

// Page is the slice of a list a caller wants. It is the store's half
// of the contract the admin API answers with: a bounded window plus
// the number of rows the filter matched, so a client can tell how far
// the list goes without asking for all of it.
//
// A Limit of zero or less means "every row". Only callers inside this
// process build one of those — the analytics sweep that walks every
// product, the public plan list of a single product. A request never
// can: the handler clamps what it reads from the query string before
// it gets here, so an unbounded page cannot be asked for over HTTP.
type Page struct {
	Limit  int
	Offset int
}

// All is that unbounded page, named so the call sites that mean it say so.
var All = Page{}

// scanPage runs the query for one page and returns how many rows the
// filter matched in total.
//
// The count is a second statement, so it is only run when a limit is
// in force — for an unbounded page the rows in hand are the total, and
// asking the database to count them again would be a query for
// nothing. The count ignores limit and offset, which is what makes it
// a total rather than a page size.
func scanPage(ctx context.Context, q *bun.SelectQuery, p Page) (int, error) {
	if p.Limit <= 0 {
		return 0, q.Scan(ctx)
	}
	if p.Offset > 0 {
		q = q.Offset(p.Offset)
	}
	return q.Limit(p.Limit).ScanAndCount(ctx)
}
