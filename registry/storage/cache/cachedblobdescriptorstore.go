package cache

import (
	"context"

	distribution "github.com/2DFS/2dfs-registry/v3"
	"github.com/2DFS/2dfs-registry/v3/internal/dcontext"
	prometheus "github.com/2DFS/2dfs-registry/v3/metrics"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type cachedBlobStatter struct {
	cache   distribution.BlobDescriptorService
	backend distribution.BlobDescriptorService
}

var (
	// cacheRequestCount is the number of total cache requests received.
	cacheRequestCount = prometheus.StorageNamespace.NewCounter("cache_requests", "The number of cache request received")
	// cacheRequestCount is the number of total cache requests received.
	cacheHitCount = prometheus.StorageNamespace.NewCounter("cache_hits", "The number of cache request received")
	// cacheErrorCount is the number of cache request errors.
	cacheErrorCount = prometheus.StorageNamespace.NewCounter("cache_errors", "The number of cache request errors")
)

// NewCachedBlobStatter creates a new statter which prefers a cache and
// falls back to a backend.
func NewCachedBlobStatter(cache distribution.BlobDescriptorService, backend distribution.BlobDescriptorService) distribution.BlobDescriptorService {
	return &cachedBlobStatter{
		cache:   cache,
		backend: backend,
	}
}

func (cbds *cachedBlobStatter) Stat(ctx context.Context, dgst digest.Digest) (v1.Descriptor, error) {
	cacheRequestCount.Inc(1)

	// try getting from cache
	desc, cacheErr := cbds.cache.Stat(ctx, dgst)
	if cacheErr == nil {
		cacheHitCount.Inc(1)
		return desc, nil
	}

	// couldn't get from cache; get from backend
	desc, err := cbds.backend.Stat(ctx, dgst)
	if err != nil {
		return desc, err
	}

	if cacheErr == distribution.ErrBlobUnknown {
		if err := cbds.cache.SetDescriptor(ctx, dgst, desc); err != nil {
			dcontext.GetLoggerWithField(ctx, "blob", dgst).WithError(err).Error("error from cache setting desc")
		}
		// we don't need to return cache error upstream if any. continue returning value from backend
	} else {
		// unknown error from cache. just log and error. do not store cache as it may be trigger many set calls
		dcontext.GetLoggerWithField(ctx, "blob", dgst).WithError(cacheErr).Error("error from cache stat(ing) blob")
		cacheErrorCount.Inc(1)
	}

	return desc, nil
}

func (cbds *cachedBlobStatter) Clear(ctx context.Context, dgst digest.Digest) error {
	err := cbds.cache.Clear(ctx, dgst)
	if err != nil {
		return err
	}

	err = cbds.backend.Clear(ctx, dgst)
	if err != nil {
		return err
	}
	return nil
}

func (cbds *cachedBlobStatter) SetDescriptor(ctx context.Context, dgst digest.Digest, desc v1.Descriptor) error {
	if err := cbds.cache.SetDescriptor(ctx, dgst, desc); err != nil {
		dcontext.GetLoggerWithField(ctx, "blob", dgst).WithError(err).Error("error from cache setting desc")
	}
	return nil
}
