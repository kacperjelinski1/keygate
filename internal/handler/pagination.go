package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/tabloy/keygate/internal/store"
	"github.com/tabloy/keygate/pkg/response"
)

// Every list endpoint answers the same two query parameters and the
// same three numbers, so a client that can page one of them can page
// all of them.
//
// The limit is clamped rather than refused: a caller asking for more
// than the cap gets the cap, and the limit that was actually applied
// comes back in the response, which is what a page count has to be
// worked out from. Without that echo a dashboard asking for 1000 and
// silently served 200 would draw a pager five times too short.
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// listPage reads the window a list request asks for. Anything
// nonsensical — zero, negative, unparseable — reads as "not asked
// for" and takes the default; in particular a limit of 0 does not
// mean "everything", which is a request no HTTP caller can make.
func listPage(c *gin.Context) store.Page {
	limit := queryInt(c, "limit", defaultListLimit)
	if limit < 1 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	offset := queryInt(c, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	return store.Page{Limit: limit, Offset: offset}
}

// listOK writes the one shape every list endpoint answers with: the
// rows under their own name, and total/limit/offset beside them.
//
// An empty list is an empty array, never null: a client that iterates
// the field should not have to special-case "no rows" differently
// from "one row".
func listOK[T any](c *gin.Context, key string, items []T, total int, p store.Page, extra ...gin.H) {
	if items == nil {
		items = []T{}
	}
	body := gin.H{key: items, "total": total, "limit": p.Limit, "offset": p.Offset}
	for _, e := range extra {
		for k, v := range e {
			body[k] = v
		}
	}
	response.OK(c, body)
}
