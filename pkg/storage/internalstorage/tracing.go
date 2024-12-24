package internalstorage

/*
	Ref from https://github.com/go-gorm/opentelemetry/tree/master/tracing@0.1.11
*/

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

var (
	firstWordRegex   = regexp.MustCompile(`^\w+`)
	cCommentRegex    = regexp.MustCompile(`(?is)/\*.*?\*/`)
	lineCommentRegex = regexp.MustCompile(`(?im)(?:--|#).*?$`)
	sqlPrefixRegex   = regexp.MustCompile(`^[\s;]*`)

	dbRowsAffected = attribute.Key("db.rows_affected")
)

type tracePlugin struct {
	attrs            []attribute.KeyValue
	excludeQueryVars bool
}

func NewGormTrace(excludeQueryVars bool) gorm.Plugin {
	return &tracePlugin{
		excludeQueryVars: excludeQueryVars,
	}
}

func (p tracePlugin) Name() string {
	return "gorm:oteltracing"
}

type gormHookFunc func(tx *gorm.DB)

type gormRegister interface {
	Register(name string, fn func(*gorm.DB)) error
}

func (p tracePlugin) Initialize(db *gorm.DB) (err error) {
	cb := db.Callback()
	hooks := []struct {
		callback gormRegister
		hook     gormHookFunc
		name     string
	}{
		{cb.Query().Before("gorm:query"), p.before("gorm.Query"), "before:select"},
		{cb.Query().After("gorm:query"), p.after("gorm.Query"), "after:select"},

		/*
			{cb.Create().Before("gorm:create"), p.before("gorm.Create"), "before:create"},
			{cb.Create().After("gorm:create"), p.after("gorm.Create"), "after:create"},

			{cb.Delete().Before("gorm:delete"), p.before("gorm.Delete"), "before:delete"},
			{cb.Delete().After("gorm:delete"), p.after("gorm.Delete"), "after:delete"},

			{cb.Update().Before("gorm:update"), p.before("gorm.Update"), "before:update"},
			{cb.Update().After("gorm:update"), p.after("gorm.Update"), "after:update"},

			{cb.Row().Before("gorm:row"), p.before("gorm.Row"), "before:row"},
			{cb.Row().After("gorm:row"), p.after("gorm.Row"), "after:row"},

			{cb.Raw().Before("gorm:raw"), p.before("gorm.Raw"), "before:raw"},
			{cb.Raw().After("gorm:raw"), p.after("gorm.Raw"), "after:raw"},
		*/
	}

	var firstErr error
	for _, h := range hooks {
		if err := h.callback.Register("otel:"+h.name, h.hook); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("callback register %s failed: %w", h.name, err)
		}
	}

	return firstErr
}

func (p *tracePlugin) before(operate string) gormHookFunc {
	return func(tx *gorm.DB) {
		span := trace.SpanFromContext(tx.Statement.Context)
		if !span.IsRecording() {
			return
		}
		span.AddEvent("About " + operate)
	}
}

func (p *tracePlugin) after(operate string) gormHookFunc {
	return func(tx *gorm.DB) {
		span := trace.SpanFromContext(tx.Statement.Context)
		if !span.IsRecording() {
			return
		}

		attrs := make([]attribute.KeyValue, 0, len(p.attrs)+4)
		attrs = append(attrs, p.attrs...)

		if sys := dbSystem(tx); sys.Valid() {
			attrs = append(attrs, sys)
		}

		vars := tx.Statement.Vars

		var query string
		if p.excludeQueryVars {
			query = tx.Statement.SQL.String()
		} else {
			query = tx.Dialector.Explain(tx.Statement.SQL.String(), vars...)
		}

		formatQuery := p.formatQuery(query)
		attrs = append(attrs, semconv.DBStatementKey.String(formatQuery))
		attrs = append(attrs, semconv.DBOperationKey.String(dbOperation(formatQuery)))
		if tx.Statement.Table != "" {
			attrs = append(attrs, semconv.DBSQLTableKey.String(tx.Statement.Table))
		}
		if tx.Statement.RowsAffected != -1 {
			attrs = append(attrs, dbRowsAffected.Int64(tx.Statement.RowsAffected))
		}

		span.AddEvent(fmt.Sprintf("%s succeeded", operate), trace.WithAttributes(attrs...))
		switch tx.Error {
		case nil,
			gorm.ErrRecordNotFound,
			driver.ErrSkip,
			io.EOF, // end of rows iterator
			sql.ErrNoRows:
			// ignore
		default:
			span.RecordError(tx.Error)
		}
	}
}

func (p *tracePlugin) formatQuery(query string) string {
	return query
}

func dbSystem(tx *gorm.DB) attribute.KeyValue {
	switch tx.Dialector.Name() {
	case "mysql":
		return semconv.DBSystemMySQL
	case "mssql":
		return semconv.DBSystemMSSQL
	case "postgres", "postgresql":
		return semconv.DBSystemPostgreSQL
	case "sqlite":
		return semconv.DBSystemSqlite
	case "sqlserver":
		return semconv.DBSystemKey.String("sqlserver")
	case "clickhouse":
		return semconv.DBSystemKey.String("clickhouse")
	case "spanner":
		return semconv.DBSystemKey.String("spanner")
	default:
		return attribute.KeyValue{}
	}
}

func dbOperation(query string) string {
	s := cCommentRegex.ReplaceAllString(query, "")
	s = lineCommentRegex.ReplaceAllString(s, "")
	s = sqlPrefixRegex.ReplaceAllString(s, "")
	return strings.ToLower(firstWordRegex.FindString(s))
}
