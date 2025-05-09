package storage

import (
	"context"
	"encoding/json"
	"fmt"

	distribution "github.com/2DFS/2dfs-registry/v3"
	"github.com/2DFS/2dfs-registry/v3/internal/dcontext"
	"github.com/2DFS/2dfs-registry/v3/manifest/manifestlist"
	"github.com/2DFS/2dfs-registry/v3/manifest/ocischema"
	"github.com/2DFS/2dfs-registry/v3/manifest/schema2"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// A ManifestHandler gets and puts manifests of a particular type.
type ManifestHandler interface {
	// Unmarshal unmarshals the manifest from a byte slice.
	Unmarshal(ctx context.Context, dgst digest.Digest, content []byte) (distribution.Manifest, error)

	// Put creates or updates the given manifest returning the manifest digest.
	Put(ctx context.Context, manifest distribution.Manifest, skipDependencyVerification bool) (digest.Digest, error)
}

// SkipLayerVerification allows a manifest to be Put before its
// layers are on the filesystem
func SkipLayerVerification() distribution.ManifestServiceOption {
	return skipLayerOption{}
}

type skipLayerOption struct{}

func (o skipLayerOption) Apply(m distribution.ManifestService) error {
	if ms, ok := m.(*manifestStore); ok {
		ms.skipDependencyVerification = true
		return nil
	}
	return fmt.Errorf("skip layer verification only valid for manifestStore")
}

type manifestStore struct {
	repository *repository
	blobStore  *linkedBlobStore
	ctx        context.Context

	skipDependencyVerification bool

	schema2Handler        ManifestHandler
	manifestListHandler   ManifestHandler
	ocischemaHandler      ManifestHandler
	ocischemaIndexHandler ManifestHandler
}

var _ distribution.ManifestService = &manifestStore{}

func (ms *manifestStore) Exists(ctx context.Context, dgst digest.Digest) (bool, error) {
	dcontext.GetLogger(ms.ctx).Debug("(*manifestStore).Exists")

	_, err := ms.blobStore.Stat(ms.ctx, dgst)
	if err != nil {
		if err == distribution.ErrBlobUnknown {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (ms *manifestStore) Get(ctx context.Context, dgst digest.Digest, options ...distribution.ManifestServiceOption) (distribution.Manifest, error) {
	dcontext.GetLogger(ms.ctx).Debug("(*manifestStore).Get")

	// TODO(stevvooe): Need to check descriptor from above to ensure that the
	// mediatype is as we expect for the manifest store.

	content, err := ms.blobStore.Get(ctx, dgst)
	if err != nil {
		if err == distribution.ErrBlobUnknown {
			return nil, distribution.ErrManifestUnknownRevision{
				Name:     ms.repository.Named().Name(),
				Revision: dgst,
			}
		}

		return nil, err
	}

	// versioned is a minimal representation of a manifest with version and mediatype.
	var versioned struct {
		specs.Versioned

		// MediaType is the media type of this schema.
		MediaType string `json:"mediaType,omitempty"`
	}
	if err = json.Unmarshal(content, &versioned); err != nil {
		return nil, err
	}

	switch versioned.SchemaVersion {
	case 2:
		// This can be an image manifest or a manifest list
		switch versioned.MediaType {
		case schema2.MediaTypeManifest:
			return ms.schema2Handler.Unmarshal(ctx, dgst, content)
		case v1.MediaTypeImageManifest:
			return ms.ocischemaHandler.Unmarshal(ctx, dgst, content)
		case manifestlist.MediaTypeManifestList:
			return ms.manifestListHandler.Unmarshal(ctx, dgst, content)
		case v1.MediaTypeImageIndex:
			return ms.ocischemaIndexHandler.Unmarshal(ctx, dgst, content)
		case "":
			// OCI image or image index - no media type in the content

			// First see if it looks like an image index
			res, err := ms.ocischemaIndexHandler.Unmarshal(ctx, dgst, content)
			resIndex := res.(*ocischema.DeserializedImageIndex)
			if err == nil && resIndex.Manifests != nil {
				return resIndex, nil
			}

			// Otherwise, assume it must be an image manifest
			return ms.ocischemaHandler.Unmarshal(ctx, dgst, content)
		default:
			return nil, distribution.ErrManifestVerification{fmt.Errorf("unrecognized manifest content type %s", versioned.MediaType)}
		}
	}

	return nil, fmt.Errorf("unrecognized manifest schema version %d", versioned.SchemaVersion)
}

func (ms *manifestStore) Put(ctx context.Context, manifest distribution.Manifest, options ...distribution.ManifestServiceOption) (digest.Digest, error) {
	dcontext.GetLogger(ms.ctx).Debug("(*manifestStore).Put")

	switch manifest.(type) {
	case *schema2.DeserializedManifest:
		return ms.schema2Handler.Put(ctx, manifest, ms.skipDependencyVerification)
	case *ocischema.DeserializedManifest:
		return ms.ocischemaHandler.Put(ctx, manifest, ms.skipDependencyVerification)
	case *manifestlist.DeserializedManifestList:
		return ms.manifestListHandler.Put(ctx, manifest, ms.skipDependencyVerification)
	case *ocischema.DeserializedImageIndex:
		return ms.ocischemaIndexHandler.Put(ctx, manifest, ms.skipDependencyVerification)
	}

	return "", fmt.Errorf("unrecognized manifest type %T", manifest)
}

// Delete removes the revision of the specified manifest.
func (ms *manifestStore) Delete(ctx context.Context, dgst digest.Digest) error {
	dcontext.GetLogger(ms.ctx).Debug("(*manifestStore).Delete")
	return ms.blobStore.Delete(ctx, dgst)
}

func (ms *manifestStore) Enumerate(ctx context.Context, ingester func(digest.Digest) error) error {
	err := ms.blobStore.Enumerate(ctx, func(dgst digest.Digest) error {
		err := ingester(dgst)
		if err != nil {
			return err
		}
		return nil
	})
	return err
}
