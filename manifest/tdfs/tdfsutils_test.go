package tdfs

import (
	"log"
	"testing"
)

type PartitionResult struct {
	Tag        string
	Partitions []Partition
}

var tagsAndPartitions map[string]PartitionResult = map[string]PartitionResult{
	"v1--1.0.0.0": {
		Tag: "v1",
		Partitions: []Partition{
			{
				x1: 1,
				y1: 0,
				x2: 0,
				y2: 0,
			},
		},
	},
	"test--0.0.0.0--1.1.1.1": {
		Tag: "test",
		Partitions: []Partition{
			{
				x1: 0,
				y1: 0,
				x2: 0,
				y2: 0,
			},
			{
				x1: 1,
				y1: 1,
				x2: 1,
				y2: 1,
			},
		},
	},
	"v2": {
		Tag:        "v2",
		Partitions: []Partition{},
	},
}

func TestCheckTagPartitions(t *testing.T) {
	for tag, result := range tagsAndPartitions {
		parsedTag, paresdPartitions := CheckTagPartitions(tag)
		if parsedTag != result.Tag {
			t.Errorf("Expected tag %s, got %s", result.Tag, parsedTag)
		}
		if len(paresdPartitions) != len(result.Partitions) {
			t.Errorf("Expected %d partitions, got %d", len(result.Partitions), len(paresdPartitions))
		}
		for i, partition := range paresdPartitions {
			if partition.x1 != result.Partitions[i].x1 || partition.y1 != result.Partitions[i].y1 ||
				partition.x2 != result.Partitions[i].x2 || partition.y2 != result.Partitions[i].y2 {
				t.Errorf("Expected partition %v, got %v", result.Partitions[i], partition)
			}
		}
		log.Default().Printf("Tag %s with partitions %v parsed successfully\n", parsedTag, paresdPartitions)
	}
	return
}
