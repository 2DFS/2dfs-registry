// Package base provides a base implementation of the storage driver that can
// be used to implement common checks. The goal is to increase the amount of
// code sharing.
//
// The canonical approach to use this class is to embed in the exported driver
// struct such that calls are proxied through this implementation. First,
// declare the internal driver, as follows:
//
//	type driver struct { ... internal ...}
//
// The resulting type should implement StorageDriver such that it can be the
// target of a Base struct. The exported type can then be declared as follows:
//
//	type Driver struct {
//		Base
//	}
//
// Because Driver embeds Base, it effectively implements Base. If the driver
// needs to intercept a call, before going to base, Driver should implement
// that method. Effectively, Driver can intercept calls before coming in and
// driver implements the actual logic.
//
// To further shield the embed from other packages, it is recommended to
// employ a private embed struct:
//
//	type baseEmbed struct {
//		base.Base
//	}
//
// Then, declare driver to embed baseEmbed, rather than Base directly:
//
//	type Driver struct {
//		baseEmbed
//	}
//
// The type now implements StorageDriver, proxying through Base, without
// exporting an unnecessary field.
package base

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/2DFS/2dfs-registry/v3/internal/dcontext"
	prometheus "github.com/2DFS/2dfs-registry/v3/metrics"
	storagedriver "github.com/2DFS/2dfs-registry/v3/registry/storage/driver"
	"github.com/2DFS/2dfs-registry/v3/tracing"
	"github.com/docker/go-metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// storageAction is the metrics of blob related operations
var storageAction = prometheus.StorageNamespace.NewLabeledTimer("action", "The number of seconds that the storage action takes", "driver", "action")

// tracer is the OpenTelemetry tracer utilized for tracing operations within
// this package's code.
var tracer = otel.Tracer("github.com/2DFS/2dfs-registry/v3/registry/storage/driver/base")

func init() {
	metrics.Register(prometheus.StorageNamespace)
}

// Base provides a wrapper around a storagedriver implementation that provides
// common path and bounds checking.
type Base struct {
	storagedriver.StorageDriver
}

// Format errors received from the storage driver
func (base *Base) setDriverName(e error) error {
	switch actual := e.(type) {
	case nil:
		return nil
	case storagedriver.ErrUnsupportedMethod:
		actual.DriverName = base.StorageDriver.Name()
		return actual
	case storagedriver.PathNotFoundError:
		actual.DriverName = base.StorageDriver.Name()
		return actual
	case storagedriver.InvalidPathError:
		actual.DriverName = base.StorageDriver.Name()
		return actual
	case storagedriver.InvalidOffsetError:
		actual.DriverName = base.StorageDriver.Name()
		return actual
	default:
		return storagedriver.Error{
			DriverName: base.StorageDriver.Name(),
			Detail:     e,
		}
	}
}

// GetContent wraps GetContent of underlying storage driver.
func (base *Base) GetContent(ctx context.Context, path string) ([]byte, error) {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
	}
	ctx, span := tracer.Start(
		ctx,
		"GetContent",
		trace.WithAttributes(attrs...))

	defer span.End()

	if !storagedriver.PathRegexp.MatchString(path) {
		return nil, storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	start := time.Now()
	b, e := base.StorageDriver.GetContent(ctx, path)
	storageAction.WithValues(base.Name(), "GetContent").UpdateSince(start)
	return b, base.setDriverName(e)
}

// PutContent wraps PutContent of underlying storage driver.
func (base *Base) PutContent(ctx context.Context, path string, content []byte) error {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
		attribute.Int(tracing.AttributePrefix+"storage.content.length", len(content)),
	}
	ctx, span := tracer.Start(
		ctx,
		"PutContent",
		trace.WithAttributes(attrs...))

	defer span.End()

	if !storagedriver.PathRegexp.MatchString(path) {
		return storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	start := time.Now()
	err := base.setDriverName(base.StorageDriver.PutContent(ctx, path, content))
	storageAction.WithValues(base.Name(), "PutContent").UpdateSince(start)
	return err
}

// Reader wraps Reader of underlying storage driver.
func (base *Base) Reader(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
		attribute.Int64(tracing.AttributePrefix+"storage.offset", offset),
	}
	ctx, span := tracer.Start(
		ctx,
		"Reader",
		trace.WithAttributes(attrs...))

	defer span.End()

	if offset < 0 {
		return nil, storagedriver.InvalidOffsetError{Path: path, Offset: offset, DriverName: base.StorageDriver.Name()}
	}

	if !storagedriver.PathRegexp.MatchString(path) {
		return nil, storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	rc, e := base.StorageDriver.Reader(ctx, path, offset)
	return rc, base.setDriverName(e)
}

// Writer wraps Writer of underlying storage driver.
func (base *Base) Writer(ctx context.Context, path string, append bool) (storagedriver.FileWriter, error) {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
		attribute.Bool(tracing.AttributePrefix+"storage.append", append),
	}
	ctx, span := tracer.Start(
		ctx,
		"Writer",
		trace.WithAttributes(attrs...))

	defer span.End()

	if !storagedriver.PathRegexp.MatchString(path) {
		return nil, storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	writer, e := base.StorageDriver.Writer(ctx, path, append)
	return writer, base.setDriverName(e)
}

// Stat wraps Stat of underlying storage driver.
func (base *Base) Stat(ctx context.Context, path string) (storagedriver.FileInfo, error) {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
	}
	ctx, span := tracer.Start(
		ctx,
		"Stat",
		trace.WithAttributes(attrs...))

	defer span.End()

	if !storagedriver.PathRegexp.MatchString(path) && path != "/" {
		return nil, storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	start := time.Now()
	fi, e := base.StorageDriver.Stat(ctx, path)
	storageAction.WithValues(base.Name(), "Stat").UpdateSince(start)
	return fi, base.setDriverName(e)
}

// List wraps List of underlying storage driver.
func (base *Base) List(ctx context.Context, path string) ([]string, error) {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
	}
	ctx, span := tracer.Start(
		ctx,
		"List",
		trace.WithAttributes(attrs...))

	defer span.End()

	if !storagedriver.PathRegexp.MatchString(path) && path != "/" {
		return nil, storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	start := time.Now()
	str, e := base.StorageDriver.List(ctx, path)
	storageAction.WithValues(base.Name(), "List").UpdateSince(start)
	return str, base.setDriverName(e)
}

// Move wraps Move of underlying storage driver.
func (base *Base) Move(ctx context.Context, sourcePath string, destPath string) error {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.source.path", sourcePath),
		attribute.String(tracing.AttributePrefix+"storage.dest.path", destPath),
	}
	ctx, span := tracer.Start(
		ctx,
		"Move",
		trace.WithAttributes(attrs...))

	defer span.End()

	ctx, done := dcontext.WithTrace(ctx)
	defer done("%s.Move(%q, %q", base.Name(), sourcePath, destPath)

	if !storagedriver.PathRegexp.MatchString(sourcePath) {
		return storagedriver.InvalidPathError{Path: sourcePath, DriverName: base.StorageDriver.Name()}
	} else if !storagedriver.PathRegexp.MatchString(destPath) {
		return storagedriver.InvalidPathError{Path: destPath, DriverName: base.StorageDriver.Name()}
	}

	start := time.Now()
	err := base.setDriverName(base.StorageDriver.Move(ctx, sourcePath, destPath))
	storageAction.WithValues(base.Name(), "Move").UpdateSince(start)
	return err
}

// Delete wraps Delete of underlying storage driver.
func (base *Base) Delete(ctx context.Context, path string) error {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
	}
	ctx, span := tracer.Start(
		ctx,
		"Delete",
		trace.WithAttributes(attrs...))

	defer span.End()

	if !storagedriver.PathRegexp.MatchString(path) {
		return storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	start := time.Now()
	err := base.setDriverName(base.StorageDriver.Delete(ctx, path))
	storageAction.WithValues(base.Name(), "Delete").UpdateSince(start)
	return err
}

// RedirectURL wraps RedirectURL of the underlying storage driver.
func (base *Base) RedirectURL(r *http.Request, path string) (string, error) {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
	}
	ctx, span := tracer.Start(
		r.Context(),
		"RedirectURL",
		trace.WithAttributes(attrs...))

	defer span.End()

	if !storagedriver.PathRegexp.MatchString(path) {
		return "", storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	start := time.Now()
	str, e := base.StorageDriver.RedirectURL(r.WithContext(ctx), path)
	storageAction.WithValues(base.Name(), "RedirectURL").UpdateSince(start)
	return str, base.setDriverName(e)
}

// Walk wraps Walk of underlying storage driver.
func (base *Base) Walk(ctx context.Context, path string, f storagedriver.WalkFn, options ...func(*storagedriver.WalkOptions)) error {
	attrs := []attribute.KeyValue{
		attribute.String(tracing.AttributePrefix+"storage.driver.name", base.Name()),
		attribute.String(tracing.AttributePrefix+"storage.path", path),
	}
	ctx, span := tracer.Start(
		ctx,
		"Walk",
		trace.WithAttributes(attrs...))

	defer span.End()

	if !storagedriver.PathRegexp.MatchString(path) && path != "/" {
		return storagedriver.InvalidPathError{Path: path, DriverName: base.StorageDriver.Name()}
	}

	return base.setDriverName(base.StorageDriver.Walk(ctx, path, f, options...))
}
