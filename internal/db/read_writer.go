package db

import (
	"context"
	"database/sql"
	stderrors "errors"
	"reflect"
	"time"

	"github.com/hashicorp/boundary/internal/errors"
	"github.com/hashicorp/boundary/internal/oplog"
	"github.com/hashicorp/boundary/internal/oplog/store"
	"github.com/hashicorp/go-dbw"
	wrapping "github.com/hashicorp/go-kms-wrapping"
)

const (
	NoRowsAffected = 0

	// DefaultLimit is the default for results for boundary
	DefaultLimit = 10000
)

// OrderBy defines an enum type for declaring a column's order by criteria.
type OrderBy int

const (
	// UnknownOrderBy would designate an unknown ordering of the column, which
	// is the standard ordering for any select without an order by clause.
	UnknownOrderBy = iota

	// AscendingOrderBy would designate ordering the column in ascending order.
	AscendingOrderBy

	// DescendingOrderBy would designate ordering the column in decending order.
	DescendingOrderBy
)

// Reader interface defines lookups/searching for resources
type Reader interface {
	// LookupById will lookup a resource by its primary key id, which must be
	// unique. If the resource implements either ResourcePublicIder or
	// ResourcePrivateIder interface, then they are used as the resource's
	// primary key for lookup.  Otherwise, the resource tags are used to
	// determine it's primary key(s) for lookup.
	LookupById(ctx context.Context, resource interface{}, opt ...Option) error

	// LookupByPublicId will lookup resource by its public_id which must be unique.
	LookupByPublicId(ctx context.Context, resource ResourcePublicIder, opt ...Option) error

	// LookupWhere will lookup and return the first resource using a where clause with parameters
	LookupWhere(ctx context.Context, resource interface{}, where string, args ...interface{}) error

	// SearchWhere will search for all the resources it can find using a where
	// clause with parameters. Supports the WithLimit option.  If
	// WithLimit < 0, then unlimited results are returned.  If WithLimit == 0, then
	// default limits are used for results.
	SearchWhere(ctx context.Context, resources interface{}, where string, args []interface{}, opt ...Option) error

	// Query will run the raw query and return the *sql.Rows results. Query will
	// operate within the context of any ongoing transaction for the db.Reader.  The
	// caller must close the returned *sql.Rows. Query can/should be used in
	// combination with ScanRows.
	Query(ctx context.Context, sql string, values []interface{}, opt ...Option) (*sql.Rows, error)

	// ScanRows will scan sql rows into the interface provided
	ScanRows(ctx context.Context, rows *sql.Rows, result interface{}) error
}

// Writer interface defines create, update and retryable transaction handlers
type Writer interface {
	// DoTx will wrap the TxHandler in a retryable transaction
	DoTx(ctx context.Context, retries uint, backOff Backoff, Handler TxHandler) (RetryInfo, error)

	// Update an object in the db, fieldMask is required and provides
	// field_mask.proto paths for fields that should be updated. The i interface
	// parameter is the type the caller wants to update in the db and its
	// fields are set to the update values. setToNullPaths is optional and
	// provides field_mask.proto paths for the fields that should be set to
	// null.  fieldMaskPaths and setToNullPaths must not intersect. The caller
	// is responsible for the transaction life cycle of the writer and if an
	// error is returned the caller must decide what to do with the transaction,
	// which almost always should be to rollback.  Update returns the number of
	// rows updated or an error. Supported options: WithOplog.
	Update(ctx context.Context, i interface{}, fieldMaskPaths []string, setToNullPaths []string, opt ...Option) (int, error)

	// Create an object in the db with options: WithDebug, WithOplog, NewOplogMsg,
	// WithLookup, WithReturnRowsAffected, OnConflict, WithVersion, and
	// WithWhere. The caller is responsible for the transaction life cycle of
	// the writer and if an error is returned the caller must decide what to do
	// with the transaction, which almost always should be to rollback.
	Create(ctx context.Context, i interface{}, opt ...Option) error

	// CreateItems will create multiple items of the same type.
	// Supported options: WithDebug, WithOplog, WithOplogMsgs,
	// WithReturnRowsAffected, OnConflict, WithVersion, and WithWhere.
	/// WithOplog and WithOplogMsgs may not be used together. WithLookup is not
	// a supported option. The caller is responsible for the transaction life
	// cycle of the writer and if an error is returned the caller must decide
	// what to do with the transaction, which almost always should be to
	// rollback.
	CreateItems(ctx context.Context, createItems []interface{}, opt ...Option) error

	// Delete an object in the db with options: WithOplog, WithDebug.
	// The caller is responsible for the transaction life cycle of the writer
	// and if an error is returned the caller must decide what to do with
	// the transaction, which almost always should be to rollback. Delete
	// returns the number of rows deleted or an error.
	Delete(ctx context.Context, i interface{}, opt ...Option) (int, error)

	// DeleteItems will delete multiple items of the same type.
	// Supported options: WithOplog and WithOplogMsgs. WithOplog and
	// WithOplogMsgs may not be used together. The caller is responsible for the
	// transaction life cycle of the writer and if an error is returned the
	// caller must decide what to do with the transaction, which almost always
	// should be to rollback. Delete returns the number of rows deleted or an error.
	DeleteItems(ctx context.Context, deleteItems []interface{}, opt ...Option) (int, error)

	// Exec will execute the sql with the values as parameters. The int returned
	// is the number of rows affected by the sql. No options are currently
	// supported.
	Exec(ctx context.Context, sql string, values []interface{}, opt ...Option) (int, error)

	// Query will run the raw query and return the *sql.Rows results. Query will
	// operate within the context of any ongoing transaction for the db.Writer.  The
	// caller must close the returned *sql.Rows. Query can/should be used in
	// combination with ScanRows.  Query is included in the Writer interface
	// so callers can execute updates and inserts with returning values.
	Query(ctx context.Context, sql string, values []interface{}, opt ...Option) (*sql.Rows, error)

	// GetTicket returns an oplog ticket for the aggregate root of "i" which can
	// be used to WriteOplogEntryWith for that aggregate root.
	GetTicket(ctx context.Context, i interface{}) (*store.Ticket, error)

	// WriteOplogEntryWith will write an oplog entry with the msgs provided for
	// the ticket's aggregateName. No options are currently supported.
	WriteOplogEntryWith(
		ctx context.Context,
		wrapper wrapping.Wrapper,
		ticket *store.Ticket,
		metadata oplog.Metadata,
		msgs []*oplog.Message,
		opt ...Option,
	) error

	// ScanRows will scan sql rows into the interface provided
	ScanRows(ctx context.Context, rows *sql.Rows, result interface{}) error
}

const (
	StdRetryCnt = 20
)

// RetryInfo provides information on the retries of a transaction
type RetryInfo struct {
	Retries int
	Backoff time.Duration
}

// TxHandler defines a handler for a func that writes a transaction for use with DoTx
type TxHandler func(Reader, Writer) error

// ResourcePublicIder defines an interface that LookupByPublicId() can use to
// get the resource's public id.
type ResourcePublicIder interface {
	GetPublicId() string
}

// ResourcePrivateIder defines an interface that LookupById() can use to get the
// resource's private id.
type ResourcePrivateIder interface {
	GetPrivateId() string
}

type OpType int

const (
	UnknownOp     OpType = 0
	CreateOp      OpType = 1
	UpdateOp      OpType = 2
	DeleteOp      OpType = 3
	CreateItemsOp OpType = 4
	DeleteItemsOp OpType = 5
	LookupOp      OpType = 6
	SearchOp      OpType = 7
)

// VetForWriter provides an interface that Create and Update can use to vet the
// resource before before writing it to the db.  For optType == UpdateOp,
// options WithFieldMaskPath and WithNullPaths are supported.  For optType ==
// CreateOp, no options are supported
type VetForWriter interface {
	VetForWrite(ctx context.Context, r Reader, opType OpType, opt ...Option) error
}

// Db uses a gorm DB connection for read/write
type Db struct {
	underlying *DB
}

// ensure that Db implements the interfaces of: Reader and Writer
var (
	_ Reader = (*Db)(nil)
	_ Writer = (*Db)(nil)
)

func New(underlying *DB) *Db {
	return &Db{underlying: underlying}
}

// Exec will execute the sql with the values as parameters. The int returned
// is the number of rows affected by the sql. No options are currently
// supported.
func (rw *Db) Exec(ctx context.Context, sql string, values []interface{}, _ ...Option) (int, error) {
	const op = "db.Exec"
	if sql == "" {
		return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing sql")
	}
	return dbw.New(rw.underlying.wrapped).Exec(ctx, sql, values)
}

// Query will run the raw query and return the *sql.Rows results. Query will
// operate within the context of any ongoing transaction for the db.Reader.  The
// caller must close the returned *sql.Rows. Query can/should be used in
// combination with ScanRows.
func (rw *Db) Query(ctx context.Context, sql string, values []interface{}, _ ...Option) (*sql.Rows, error) {
	const op = "db.Query"
	if sql == "" {
		return nil, errors.New(ctx, errors.InvalidParameter, op, "missing sql")
	}
	return dbw.New(rw.underlying.wrapped).Query(ctx, sql, values)
}

// Scan rows will scan the rows into the interface
func (rw *Db) ScanRows(ctx context.Context, rows *sql.Rows, result interface{}) error {
	const op = "db.ScanRows"
	if rw.underlying == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	}
	if isNil(result) {
		return errors.New(ctx, errors.InvalidParameter, op, "missing result")
	}
	return dbw.New(rw.underlying.wrapped).ScanRows(rows, result)
}

// Create an object in the db with options: WithDebug, WithOplog, NewOplogMsg,
// WithLookup, WithReturnRowsAffected, OnConflict, WithVersion, and WithWhere.
//
// WithOplog will write an oplog entry for the create. NewOplogMsg will return
// in-memory oplog message.  WithOplog and NewOplogMsg cannot be used together.
// WithLookup with to force a lookup after create.
//
// OnConflict specifies alternative actions to take when an insert results in a
// unique constraint or exclusion constraint error. If WithVersion is used, then
// the update for on conflict will include the version number, which basically
// makes the update use optimistic locking and the update will only succeed if
// the existing rows version matches the WithVersion option.  Zero is not a
// valid value for the WithVersion option and will return an error. WithWhere
// allows specifying an additional constraint on the on conflict operation in
// addition to the on conflict target policy (columns or constraint).
func (rw *Db) Create(ctx context.Context, i interface{}, opt ...Option) error {
	const op = "db.Create"
	if rw.underlying == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	}
	dbwOpts, err := getDbwOptions(ctx, rw, i, CreateOp, opt...)
	if err != nil {
		return errors.Wrap(ctx, err, op)
	}
	if err := dbw.New(rw.underlying.wrapped).Create(ctx, i, dbwOpts...); err != nil {
		return wrapError(ctx, err, op)
	}
	return nil
}

// CreateItems will create multiple items of the same type. Supported options:
// WithDebug, WithOplog, WithOplogMsgs, WithReturnRowsAffected, OnConflict,
// WithVersion, and WithWhere  WithOplog and WithOplogMsgs may not be used
// together.  WithLookup is not a supported option.
func (rw *Db) CreateItems(ctx context.Context, createItems []interface{}, opt ...Option) error {
	const op = "db.CreateItems"
	if rw.underlying == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	}
	dbwOpts, err := getDbwOptions(ctx, rw, createItems, CreateItemsOp, opt...)
	if err != nil {
		return errors.Wrap(ctx, err, op)
	}
	if err := dbw.New(rw.underlying.wrapped).CreateItems(ctx, createItems, dbwOpts...); err != nil {
		return wrapError(ctx, err, op)
	}
	return nil
}

// Update an object in the db, fieldMask is required and provides
// field_mask.proto paths for fields that should be updated. The i interface
// parameter is the type the caller wants to update in the db and its fields are
// set to the update values. setToNullPaths is optional and provides
// field_mask.proto paths for the fields that should be set to null.
// fieldMaskPaths and setToNullPaths must not intersect. The caller is
// responsible for the transaction life cycle of the writer and if an error is
// returned the caller must decide what to do with the transaction, which almost
// always should be to rollback.  Update returns the number of rows updated.
//
// Supported options: WithOplog, NewOplogMsg, WithWhere, WithDebug, and
// WithVersion. WithOplog will write an oplog entry for the update. NewOplogMsg
// will return in-memory oplog message. WithOplog and NewOplogMsg cannot be used
// together. If WithVersion is used, then the update will include the version
// number in the update where clause, which basically makes the update use
// optimistic locking and the update will only succeed if the existing rows
// version matches the WithVersion option. Zero is not a valid value for the
// WithVersion option and will return an error. WithWhere allows specifying an
// additional constraint on the operation in addition to the PKs. WithDebug will
// turn on debugging for the update call.
func (rw *Db) Update(ctx context.Context, i interface{}, fieldMaskPaths []string, setToNullPaths []string, opt ...Option) (int, error) {
	const op = "db.Update"
	if rw.underlying == nil {
		return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	}
	dbwOpts, err := getDbwOptions(ctx, rw, i, UpdateOp, opt...)
	if err != nil {
		return NoRowsAffected, errors.Wrap(ctx, err, op)
	}
	rowsUpdated, err := dbw.New(rw.underlying.wrapped).Update(ctx, i, fieldMaskPaths, setToNullPaths, dbwOpts...)
	if err != nil {
		return NoRowsAffected, wrapError(ctx, err, op)
	}
	return rowsUpdated, nil
}

// Delete an object in the db with options: WithOplog, NewOplogMsg, WithWhere.
// WithOplog will write an oplog entry for the delete. NewOplogMsg will return
// in-memory oplog message. WithOplog and NewOplogMsg cannot be used together.
// WithWhere allows specifying an additional constraint on the operation in
// addition to the PKs. Delete returns the number of rows deleted and any errors.
func (rw *Db) Delete(ctx context.Context, i interface{}, opt ...Option) (int, error) {
	// const op = "db.Delete"
	// if rw.underlying == nil {
	// 	return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	// }
	// if isNil(i) {
	// 	return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing interface")
	// }
	// opts := GetOpts(opt...)
	// withOplog := opts.withOplog
	// if withOplog && opts.newOplogMsg != nil {
	// 	return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "both WithOplog and NewOplogMsg options have been specified")
	// }

	// mDb := rw.underlying.Model(i)
	// err := mDb.Statement.Parse(i)
	// if err == nil && mDb.Statement.Schema == nil {
	// 	return NoRowsAffected, errors.New(ctx, errors.Unknown, op, "internal error: unable to parse stmt", errors.WithWrap(err))
	// }
	// reflectValue := reflect.Indirect(reflect.ValueOf(i))
	// for _, pf := range mDb.Statement.Schema.PrimaryFields {
	// 	if _, isZero := pf.ValueOf(reflectValue); isZero {
	// 		return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, fmt.Sprintf("primary key %s is not set", pf.Name))
	// 	}
	// }

	// if withOplog {
	// 	_, err := validateOplogArgs(ctx, i, opts)
	// 	if err != nil {
	// 		return NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("oplog validation failed"))
	// 	}
	// }
	// var ticket *store.Ticket
	// if withOplog {
	// 	var err error
	// 	ticket, err = rw.GetTicket(i)
	// 	if err != nil {
	// 		return NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get ticket"))
	// 	}
	// }
	// db := rw.underlying.DB
	// if opts.withWhereClause != "" {
	// 	db = db.Where(opts.withWhereClause, opts.withWhereClauseArgs...)
	// }
	// if opts.withDebug {
	// 	db = db.Debug()
	// }
	// db = db.Delete(i)
	// if db.Error != nil {
	// 	return NoRowsAffected, errors.Wrap(ctx, db.Error, op, errors.WithoutEvent())
	// }
	// rowsDeleted := int(db.RowsAffected)
	// if rowsDeleted > 0 && (withOplog || opts.newOplogMsg != nil) {
	// 	if withOplog {
	// 		if err := rw.addOplog(ctx, DeleteOp, opts, ticket, i); err != nil {
	// 			return rowsDeleted, errors.Wrap(ctx, err, op, errors.WithMsg("add oplog failed"))
	// 		}
	// 	}
	// 	if opts.newOplogMsg != nil {
	// 		msg, err := rw.newOplogMessage(ctx, DeleteOp, i)
	// 		if err != nil {
	// 			return rowsDeleted, errors.Wrap(ctx, err, op, errors.WithMsg("returning oplog failed"))
	// 		}
	// 		*opts.newOplogMsg = *msg
	// 	}
	// }
	// return rowsDeleted, nil
	panic("todo")
}

// DeleteItems will delete multiple items of the same type. Supported options:
// WithOplog and WithOplogMsgs.  WithOplog and WithOplogMsgs may not be used
// together.
func (rw *Db) DeleteItems(ctx context.Context, deleteItems []interface{}, opt ...Option) (int, error) {
	// const op = "db.DeleteItems"
	// if rw.underlying == nil {
	// 	return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	// }
	// if len(deleteItems) == 0 {
	// 	return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "no interfaces to delete")
	// }
	// opts := GetOpts(opt...)
	// if opts.newOplogMsg != nil {
	// 	return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "new oplog msg (singular) is not a supported option")
	// }
	// if opts.withOplog && opts.newOplogMsgs != nil {
	// 	return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, "both WithOplog and NewOplogMsgs options have been specified")
	// }
	// // verify that createItems are all the same type.
	// var foundType reflect.Type
	// for i, v := range deleteItems {
	// 	if i == 0 {
	// 		foundType = reflect.TypeOf(v)
	// 	}
	// 	currentType := reflect.TypeOf(v)
	// 	if foundType != currentType {
	// 		return NoRowsAffected, errors.New(ctx, errors.InvalidParameter, op, fmt.Sprintf("items contain disparate types.  item %d is not a %s", i, foundType.Name()))
	// 	}
	// }

	// var ticket *store.Ticket
	// if opts.withOplog {
	// 	_, err := validateOplogArgs(ctx, deleteItems[0], opts)
	// 	if err != nil {
	// 		return NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("oplog validation failed"))
	// 	}
	// 	ticket, err = rw.GetTicket(deleteItems[0])
	// 	if err != nil {
	// 		return NoRowsAffected, errors.Wrap(ctx, err, op, errors.WithMsg("unable to get ticket"))
	// 	}
	// }
	// rowsDeleted := 0
	// for _, item := range deleteItems {
	// 	// calling delete directly on the underlying db, since the writer.Delete
	// 	// doesn't provide capabilities needed here (which is different from the
	// 	// relationship between Create and CreateItems).
	// 	underlying := rw.underlying.Delete(item)
	// 	if underlying.Error != nil {
	// 		return rowsDeleted, errors.Wrap(ctx, underlying.Error, op, errors.WithoutEvent())
	// 	}
	// 	rowsDeleted += int(underlying.RowsAffected)
	// }
	// if rowsDeleted > 0 && (opts.withOplog || opts.newOplogMsgs != nil) {
	// 	if opts.withOplog {
	// 		if err := rw.addOplogForItems(ctx, DeleteOp, opts, ticket, deleteItems); err != nil {
	// 			return rowsDeleted, errors.Wrap(ctx, err, op, errors.WithMsg("unable to add oplog"))
	// 		}
	// 	}
	// 	if opts.newOplogMsgs != nil {
	// 		msgs, err := rw.oplogMsgsForItems(ctx, DeleteOp, opts, deleteItems)
	// 		if err != nil {
	// 			return rowsDeleted, errors.Wrap(ctx, err, op, errors.WithMsg("returning oplog msgs failed"))
	// 		}
	// 		*opts.newOplogMsgs = append(*opts.newOplogMsgs, msgs...)
	// 	}
	// }
	// return rowsDeleted, nil
	panic("todo")
}

// DoTx will wrap the Handler func passed within a transaction with retries
// you should ensure that any objects written to the db in your TxHandler are retryable, which
// means that the object may be sent to the db several times (retried), so things like the primary key must
// be reset before retry
func (w *Db) DoTx(ctx context.Context, retries uint, backOff Backoff, Handler TxHandler) (RetryInfo, error) {
	// const op = "db.DoTx"
	// if w.underlying == nil {
	// 	return RetryInfo{}, errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	// }
	// info := RetryInfo{}
	// for attempts := uint(1); ; attempts++ {
	// 	if attempts > retries+1 {
	// 		return info, errors.New(ctx, errors.MaxRetries, op, fmt.Sprintf("Too many retries: %d of %d", attempts-1, retries+1), errors.WithoutEvent())
	// 	}

	// 	// step one of this, start a transaction...
	// 	newTx := w.underlying.WithContext(ctx)
	// 	newTx = newTx.Begin()

	// 	rw := &Db{underlying: &DB{newTx}}
	// 	if err := Handler(rw, rw); err != nil {
	// 		if err := newTx.Rollback().Error; err != nil {
	// 			return info, errors.Wrap(ctx, err, op, errors.WithoutEvent())
	// 		}
	// 		if errors.Match(errors.T(errors.TicketAlreadyRedeemed), err) {
	// 			d := backOff.Duration(attempts)
	// 			info.Retries++
	// 			info.Backoff = info.Backoff + d
	// 			time.Sleep(d)
	// 			continue
	// 		}
	// 		return info, errors.Wrap(ctx, err, op, errors.WithoutEvent())
	// 	}

	// 	if err := newTx.Commit().Error; err != nil {
	// 		if err := newTx.Rollback().Error; err != nil {
	// 			return info, errors.Wrap(ctx, err, op)
	// 		}
	// 		return info, errors.Wrap(ctx, err, op)
	// 	}
	// 	return info, nil // it all worked!!!
	// }
	panic("todo")
}

// LookupByPublicId will lookup resource by its public_id or private_id, which
// must be unique. Options are ignored.
func (rw *Db) LookupById(ctx context.Context, resourceWithIder interface{}, _ ...Option) error {
	const op = "db.LookupById"
	if rw.underlying == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	}
	if err := dbw.New(rw.underlying.wrapped).LookupBy(ctx, resourceWithIder); err != nil {
		return wrapError(ctx, err, op)
	}
	return nil
}

// LookupByPublicId will lookup resource by its public_id, which must be unique.
// Options are ignored.
func (rw *Db) LookupByPublicId(ctx context.Context, resource ResourcePublicIder, opt ...Option) error {
	return rw.LookupById(ctx, resource, opt...)
}

// LookupWhere will lookup the first resource using a where clause with parameters (it only returns the first one)
func (rw *Db) LookupWhere(ctx context.Context, resource interface{}, where string, args ...interface{}) error {
	const op = "db.LookupWhere"
	if rw.underlying == nil {
		return errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	}
	if err := dbw.New(rw.underlying.wrapped).LookupWhere(ctx, resource, where, args...); err != nil {
		return wrapError(ctx, err, op)
	}
	return nil
}

// SearchWhere will search for all the resources it can find using a where
// clause with parameters. An error will be returned if args are provided without a
// where clause.
//
// Supports the WithLimit option.  If WithLimit < 0, then unlimited results are returned.
// If WithLimit == 0, then default limits are used for results.
// Supports the WithOrder and WithDebug options.
func (rw *Db) SearchWhere(ctx context.Context, resources interface{}, where string, args []interface{}, opt ...Option) error {
	panic("todo")
	// const op = "db.SearchWhere"
	// opts := GetOpts(opt...)
	// if rw.underlying == nil {
	// 	return errors.New(ctx, errors.InvalidParameter, op, "missing underlying db")
	// }
	// if where == "" && len(args) > 0 {
	// 	return errors.New(ctx, errors.InvalidParameter, op, "args provided with empty where")
	// }
	// if reflect.ValueOf(resources).Kind() != reflect.Ptr {
	// 	return errors.New(ctx, errors.InvalidParameter, op, "interface parameter must to be a pointer")
	// }
	// var err error
	// db := rw.underlying.WithContext(ctx)
	// if opts.withOrder != "" {
	// 	db = db.Order(opts.withOrder)
	// }
	// if opts.withDebug {
	// 	db = db.Debug()
	// }
	// // Perform limiting
	// switch {
	// case opts.WithLimit < 0: // any negative number signals unlimited results
	// case opts.WithLimit == 0: // zero signals the default value and default limits
	// 	db = db.Limit(DefaultLimit)
	// default:
	// 	db = db.Limit(opts.WithLimit)
	// }

	// if where != "" {
	// 	db = db.Where(where, args...)
	// }

	// // Perform the query
	// err = db.Find(resources).Error
	// if err != nil {
	// 	// searching with a slice parameter does not return a gorm.ErrRecordNotFound
	// 	return errors.Wrap(ctx, err, op, errors.WithoutEvent())
	// }
	// return nil
}

// func (rw *Db) primaryFieldsAreZero(ctx context.Context, i interface{}) ([]string, bool, error) {
// 	const op = "db.primaryFieldsAreZero"
// 	var fieldNames []string
// 	tx := rw.underlying.Model(i)
// 	if err := tx.Statement.Parse(i); err != nil {
// 		return nil, false, errors.Wrap(ctx, err, op, errors.WithoutEvent())
// 	}
// 	for _, f := range tx.Statement.Schema.PrimaryFields {
// 		if f.PrimaryKey {
// 			if _, isZero := f.ValueOf(reflect.ValueOf(i)); isZero {
// 				fieldNames = append(fieldNames, f.Name)
// 			}
// 		}
// 	}
// 	return fieldNames, len(fieldNames) > 0, nil
// }

// // filterPaths will filter out non-updatable fields
// func filterPaths(paths []string) []string {
// 	if len(paths) == 0 {
// 		return nil
// 	}
// 	var filtered []string
// 	for _, p := range paths {
// 		switch {
// 		case strings.EqualFold(p, "CreateTime"):
// 			continue
// 		case strings.EqualFold(p, "UpdateTime"):
// 			continue
// 		case strings.EqualFold(p, "PublicId"):
// 			continue
// 		default:
// 			filtered = append(filtered, p)
// 		}
// 	}
// 	return filtered
// }

// func setFieldsToNil(i interface{}, fieldNames []string) {
// 	// Note: error cases are not handled
// 	_ = Clear(i, fieldNames, 2)
// }

func isNil(i interface{}) bool {
	if i == nil {
		return true
	}
	switch reflect.TypeOf(i).Kind() {
	case reflect.Ptr, reflect.Map, reflect.Array, reflect.Chan, reflect.Slice:
		return reflect.ValueOf(i).IsNil()
	}
	return false
}

// func contains(ss []string, t string) bool {
// 	for _, s := range ss {
// 		if strings.EqualFold(s, t) {
// 			return true
// 		}
// 	}
// 	return false
// }

// // Clear sets fields in the value pointed to by i to their zero value.
// // Clear descends i to depth clearing fields at each level. i must be a
// // pointer to a struct. Cycles in i are not detected.
// //
// // A depth of 2 will change i and i's children. A depth of 1 will change i
// // but no children of i. A depth of 0 will return with no changes to i.
// func Clear(i interface{}, fields []string, depth int) error {
// 	const op = "db.Clear"
// 	if len(fields) == 0 || depth == 0 {
// 		return nil
// 	}
// 	fm := make(map[string]bool)
// 	for _, f := range fields {
// 		fm[f] = true
// 	}

// 	v := reflect.ValueOf(i)

// 	switch v.Kind() {
// 	case reflect.Ptr:
// 		if v.IsNil() || v.Elem().Kind() != reflect.Struct {
// 			return errors.EDeprecated(errors.WithCode(errors.InvalidParameter), errors.WithOp(op), errors.WithoutEvent())
// 		}
// 		clear(v, fm, depth)
// 	default:
// 		return errors.EDeprecated(errors.WithCode(errors.InvalidParameter), errors.WithOp(op), errors.WithoutEvent())
// 	}
// 	return nil
// }

// func clear(v reflect.Value, fields map[string]bool, depth int) {
// 	if depth == 0 {
// 		return
// 	}
// 	depth--

// 	switch v.Kind() {
// 	case reflect.Ptr:
// 		clear(v.Elem(), fields, depth+1)
// 	case reflect.Struct:
// 		typeOfT := v.Type()
// 		for i := 0; i < v.NumField(); i++ {
// 			f := v.Field(i)
// 			if ok := fields[typeOfT.Field(i).Name]; ok {
// 				if f.IsValid() && f.CanSet() {
// 					f.Set(reflect.Zero(f.Type()))
// 				}
// 				continue
// 			}
// 			clear(f, fields, depth)
// 		}
// 	}
// }

func wrapError(ctx context.Context, err error, op string, errOpts ...errors.Option) error {
	// See github.com/hashicorp/go-dbw/error.go for appropriate errors to test
	// for and wrap
	switch {
	case stderrors.Is(err, dbw.ErrUnknown):
		return errors.Wrap(ctx, err, errors.Op(op), errors.WithCode(errors.Unknown))
	case stderrors.Is(err, dbw.ErrInvalidParameter):
		return errors.Wrap(ctx, err, errors.Op(op), errors.WithCode(errors.InvalidParameter))
	case stderrors.Is(err, dbw.ErrInternal):
		return errors.Wrap(ctx, err, errors.Op(op), errors.WithCode(errors.Internal))
	case stderrors.Is(err, dbw.ErrRecordNotFound):
		return errors.Wrap(ctx, err, errors.Op(op), errors.WithCode(errors.RecordNotFound))
	case stderrors.Is(err, dbw.ErrMaxRetries):
		return errors.Wrap(ctx, err, errors.Op(op), errors.WithCode(errors.MaxRetries))
	case stderrors.Is(err, dbw.ErrInvalidFieldMask):
		return errors.Wrap(ctx, err, errors.Op(op), errors.WithCode(errors.InvalidFieldMask))
	default:
		return errors.Wrap(ctx, err, errors.Op(op), errOpts...)
	}
}
