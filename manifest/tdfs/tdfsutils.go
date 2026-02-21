package tdfs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"

	tdfsfilesystem "github.com/2DFS/2dfs-builder/filesystem"
	distribution "github.com/2DFS/2dfs-registry/v3"
	"github.com/2DFS/2dfs-registry/v3/manifest/ocischema"
)

type Partition struct {
	x1 int
	y1 int
	x2 int
	y2 int
}

// AllotmentWithPrefetch wraps an Allotment with prefetch metadata
type AllotmentWithPrefetch struct {
	tdfsfilesystem.Allotment
	ShouldPrefetch bool
}

// isInPartition checks if an allotment falls within any of the given partitions
func isInPartition(allotment tdfsfilesystem.Allotment, partitions []Partition) bool {
	for _, p := range partitions {
		if allotment.Row >= p.x1 && allotment.Row <= p.x2 &&
			allotment.Col >= p.y1 && allotment.Col <= p.y2 {
			return true
		}
	}
	return false
}

const (
	//semantic tag partition init char
	partitionInit = `--`
	//semantic tag partition split char
	partitionSplitChar = `.`
	//semantic partition regex patter
	semanticTagPattern = partitionInit + `\d+\` + partitionSplitChar + `\d+\` + partitionSplitChar + `\d+\` + partitionSplitChar + `\d+`
)

// CheckTagPartitions checks if the tag contains semantic partitions and returns the tag and the partitions
func CheckTagPartitions(tag string) (string, []Partition) {
	partitions := []Partition{}
	onlyTag := tag
	re := regexp.MustCompile(semanticTagPattern)
	matches := re.FindAllString(tag, -1)

	if len(matches) > 0 {
		onlyTag = strings.Split(tag, partitionInit)[0]
		//semantic tag with partition
		log.Default().Printf("Semantic tag with partition detected %s\n", tag)
		for _, match := range matches {
			part, err := parsePartition(strings.Replace(match, partitionInit, "", -1))
			if err != nil {
				log.Default().Printf("[WARNING] Invalid partition %s, skipping...\n", match)
				continue
			}
			partitions = append(partitions, part)
			log.Default().Printf("[PARTITIONING...] Added partition %+v \n", part)
		}
	}
	return onlyTag, partitions
}

func parsePartition(p string) (Partition, error) {
	parts := strings.Split(p, partitionSplitChar)
	result := Partition{}
	if len(parts) != 4 {
		return result, fmt.Errorf("invalid partition %s", p)
	}
	var err error
	result.x1, err = strconv.Atoi(parts[0])
	if err != nil {
		return result, err
	}
	result.y1, err = strconv.Atoi(parts[1])
	if err != nil {
		return result, err
	}
	result.x2, err = strconv.Atoi(parts[2])
	if err != nil {
		return result, err
	}
	result.y2, err = strconv.Atoi(parts[3])
	if err != nil {
		return result, err
	}
	return result, nil
}

func ConvertTdfsManifestToOciManifest(ctx context.Context, tdfsManifest *ocischema.DeserializedManifest, blobService distribution.BlobService, partitions []Partition) (distribution.Manifest, error) {

	log.Default().Printf("Converting TDFS manifest to OCI manifest\n")
	newLayers := []distribution.Descriptor{}
	allAllotments := []AllotmentWithPrefetch{}
	layerConfigBlob, err := blobService.Get(ctx, tdfsManifest.Config.Digest)
	if err != nil {
		log.Default().Printf("Error getting config %s\n", tdfsManifest.Config.Digest)
		return nil, err
	}
	var config v1.Image = v1.Image{}
	err = json.Unmarshal(layerConfigBlob, &config)
	if err != nil {
		log.Default().Printf("Error unmarshalling config %s\n", tdfsManifest.Config.Digest)
		return nil, err
	}

	//select partitions
	for _, layer := range tdfsManifest.Layers {
		if layer.MediaType == MediaTypeTdfsLayer {
			log.Default().Printf("Converting tdfs layer %s\n", layer.Digest)
			layerContent, err := blobService.Get(ctx, layer.Digest)
			if err != nil {
				log.Default().Printf("Error getting layer %s\n", layer.Digest)
				return nil, err
			}
			field, err := tdfsfilesystem.GetField().Unmarshal(string(layerContent))
			if err != nil {
				log.Default().Printf("Error unmarshalling layer %s\n", layer.Digest)
				return nil, err
			}
			// Only process if we haven't collected allotments yet
			if len(allAllotments) == 0 {
				log.Default().Printf("Adding field!!\n")
				if field != nil {
					// Use a map to track unique allotments by digest and their prefetch status
					seenDigests := make(map[string]*AllotmentWithPrefetch)

					for allotment := range field.IterateAllotments() {
						// Skip empty allotments
						if allotment.Digest == "" {
							continue
						}

						inPartition := isInPartition(allotment, partitions)

						// Non-stargz allotments have no lazy loading mechanism, so filter
						// them at the manifest level — only include if they match the partition.
						// Stargz allotments are always included; the runtime handles lazy loading.
						if allotment.TOCDigest == "" && !inPartition {
							log.Default().Printf("Skipping non-stargz allotment %s (not in partition)\n", allotment.Digest)
							continue
						}

						// Check if allotment is already seen (deduplication)
						if existing, ok := seenDigests[allotment.Digest]; ok {
							// If already seen, just update prefetch status if this one matches partition
							if !existing.ShouldPrefetch && inPartition {
								existing.ShouldPrefetch = true
								log.Default().Printf("Allotment %s marked for prefetch\n", allotment.Digest)
							}
							continue
						}

						seenDigests[allotment.Digest] = &AllotmentWithPrefetch{
							Allotment:      allotment,
							ShouldPrefetch: inPartition,
						}

						if inPartition {
							log.Default().Printf("Allotment %s at (%d,%d) matches partition, marked for prefetch\n",
								allotment.Digest, allotment.Row, allotment.Col)
						}
					}

					// Convert map to slice
					for _, awp := range seenDigests {
						allAllotments = append(allAllotments, *awp)
					}
				}
			}
		} else {
			log.Default().Printf("Appended layer %s\n", layer.Digest)
			newLayers = append(newLayers, layer)
		}
	}

	//create new layers
	if len(allAllotments) > 0 {
		//adding allotment layers
		for _, awp := range allAllotments {
			blob, err := blobService.Stat(ctx, digest.Digest(fmt.Sprintf("sha256:%s", awp.Digest)))
			if err != nil {
				log.Default().Printf("Unable to find allotment %s\n", awp.Digest)
				return nil, err
			}
			log.Default().Printf("Allotment %s [CREATING] (prefetch=%v)\n", awp.Digest, awp.ShouldPrefetch)

			// Build annotations
			annotations := map[string]string{}

			// Add stargz TOC digest annotation if available
			if awp.TOCDigest != "" {
				annotations["containerd.io/snapshot/stargz/toc.digest"] = awp.TOCDigest
			}

			// Add prefetch annotation for partition-matching allotments
			if awp.ShouldPrefetch {
				annotations["containerd.io/snapshot/remote/stargz.prefetch"] = fmt.Sprintf("%d", blob.Size)
				log.Default().Printf("Added prefetch annotation for allotment %s with size %d\n", awp.Digest, blob.Size)
			}

			// Only set annotations if we have any
			var layerAnnotations map[string]string
			if len(annotations) > 0 {
				layerAnnotations = annotations
			}

			newLayers = append(newLayers, distribution.Descriptor{
				MediaType:   "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:      digest.Digest(fmt.Sprintf("sha256:%s", awp.Digest)),
				Size:        blob.Size,
				Annotations: layerAnnotations,
			})
			config.RootFS.DiffIDs = append(config.RootFS.DiffIDs, digest.Digest(fmt.Sprintf("sha256:%s", awp.DiffID)))
		}
		log.Default().Printf("All allotments added! Total: %d\n", len(allAllotments))
	}

	newConfig, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	//create new manifest
	manifestBuilder := ocischema.NewManifestBuilder(blobService, newConfig, tdfsManifest.Annotations)
	err = manifestBuilder.SetMediaType(v1.MediaTypeImageManifest)
	if err != nil {
		log.Default().Printf("Error setting media type %s\n", v1.MediaTypeImageManifest)
		return nil, err
	}
	for _, layer := range newLayers {
		err := manifestBuilder.AppendReference(layer)
		if err != nil {
			log.Default().Printf("Error appending layer %s\n", layer.Digest)
			return nil, err
		}
	}
	return manifestBuilder.Build(ctx)
}

func ConvertPartitionedIndexToOciIndex(tdfsManifest *ocischema.DeserializedImageIndex) ([]byte, error) {
	log.Default().Printf("Converting partitioned index to OCI index\n")
	return tdfsManifest.MarshalJSON()
}
