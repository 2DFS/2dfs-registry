package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	distribution "github.com/2DFS/2dfs-registry/v3"
	"github.com/2DFS/2dfs-registry/v3/internal/dcontext"
	"github.com/2DFS/2dfs-registry/v3/manifest/schema2"
	"github.com/opencontainers/go-digest"
)

var (
	errMissingURL = errors.New("missing URL on layer")
	errInvalidURL = errors.New("invalid URL on layer")
)

// schema2ManifestHandler is a ManifestHandler that covers schema2 manifests.
type schema2ManifestHandler struct {
	repository   distribution.Repository
	blobStore    distribution.BlobStore
	ctx          context.Context
	manifestURLs manifestURLs
}

var _ ManifestHandler = &schema2ManifestHandler{}

func (ms *schema2ManifestHandler) Unmarshal(ctx context.Context, dgst digest.Digest, content []byte) (distribution.Manifest, error) {
	dcontext.GetLogger(ms.ctx).Debug("(*schema2ManifestHandler).Unmarshal")

	m := &schema2.DeserializedManifest{}
	if err := m.UnmarshalJSON(content); err != nil {
		return nil, err
	}

	return m, nil
}

func (ms *schema2ManifestHandler) Put(ctx context.Context, manifest distribution.Manifest, skipDependencyVerification bool) (digest.Digest, error) {
	dcontext.GetLogger(ms.ctx).Debug("(*schema2ManifestHandler).Put")

	m, ok := manifest.(*schema2.DeserializedManifest)
	if !ok {
		return "", fmt.Errorf("non-schema2 manifest put to schema2ManifestHandler: %T", manifest)
	}

	if err := ms.verifyManifest(ms.ctx, *m, skipDependencyVerification); err != nil {
		return "", err
	}

	mt, payload, err := m.Payload()
	if err != nil {
		return "", err
	}

	revision, err := ms.blobStore.Put(ctx, mt, payload)
	if err != nil {
		dcontext.GetLogger(ctx).Errorf("error putting payload into blobstore: %v", err)
		return "", err
	}

	return revision.Digest, nil
}

// verifyManifest ensures that the manifest content is valid from the
// perspective of the registry. As a policy, the registry only tries to store
// valid content, leaving trust policies of that content up to consumers.
func (ms *schema2ManifestHandler) verifyManifest(ctx context.Context, mnfst schema2.DeserializedManifest, skipDependencyVerification bool) error {
	var errs distribution.ErrManifestVerification

	if mnfst.Manifest.SchemaVersion != 2 {
		return fmt.Errorf("unrecognized manifest schema version %d", mnfst.Manifest.SchemaVersion)
	}

	if skipDependencyVerification {
		return nil
	}

	manifestService, err := ms.repository.Manifests(ctx)
	if err != nil {
		return err
	}

	blobsService := ms.repository.Blobs(ctx)

	for _, descriptor := range mnfst.References() {
		err := descriptor.Digest.Validate()
		if err != nil {
			errs = append(errs, err, distribution.ErrManifestBlobUnknown{Digest: descriptor.Digest})
			continue
		}

		switch descriptor.MediaType {
		case schema2.MediaTypeForeignLayer:
			// Clients download this layer from an external URL, so do not check for
			// its presence.
			if len(descriptor.URLs) == 0 {
				err = errMissingURL
			}
			allow := ms.manifestURLs.allow
			deny := ms.manifestURLs.deny
			for _, u := range descriptor.URLs {
				var pu *url.URL
				pu, err = url.Parse(u)
				if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Fragment != "" || (allow != nil && !allow.MatchString(u)) || (deny != nil && deny.MatchString(u)) {
					err = errInvalidURL
					break
				}
			}
		case schema2.MediaTypeManifest:
			var exists bool
			exists, err = manifestService.Exists(ctx, descriptor.Digest)
			if err != nil || !exists {
				err = distribution.ErrBlobUnknown // just coerce to unknown.
			}

			if err != nil {
				dcontext.GetLogger(ms.ctx).WithError(err).Debugf("failed to ensure exists of %v in manifest service", descriptor.Digest)
			}
			fallthrough // double check the blob store.
		default:
			// check its presence
			_, err = blobsService.Stat(ctx, descriptor.Digest)
		}

		if err != nil {
			if err != distribution.ErrBlobUnknown {
				errs = append(errs, err)
			}

			// On error here, we always append unknown blob errors.
			errs = append(errs, distribution.ErrManifestBlobUnknown{Digest: descriptor.Digest})
		}
	}

	if len(errs) != 0 {
		return errs
	}

	return nil
}
