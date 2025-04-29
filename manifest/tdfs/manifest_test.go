package tdfs

import (
	"strings"
	"testing"

	tdfs "github.com/2DFS/2dfs-builder/filesystem"
)

const expectedManifestSerialization = `{
  "rows": [
    {
      "allotments": [
        {
          "row": 0,
          "col": 0,
          "digest": "4125b344c065ea823f46ad3ea56b468398d6a71cee2c853f38594741aca8d6d2",
          "diffid": "76d9d151de596d0b6031e9263da644d8ded6fbdd201d68840d2bdf08d2f187dd"
        },
        {
          "row": 0,
          "col": 1,
          "digest": "dd631a30bc67c8edd567016dfe36af2719de13c9f8af9abe77d9da49245f7d30",
          "diffid": "3b3f37d82931d236475d88a7c91696b378f360bc870bb776eacb584fd8f95655"
        }
      ],
      "allotments_size": 2
    },
    {
      "allotments": [
        {
          "row": 1,
          "col": 0,
          "digest": "ba23244b835211f0cc7876851770b60cd183fb5b1c874c83d5108e5ba9c73999",
          "diffid": "dd377c1de373dd32503f845b14c0ef7c6243f8169e01bf95277641ec62607e6d"
        },
        {
          "row": 1,
          "col": 1,
          "digest": "70284438af4d85e203e7deedceb17556135b7b91ed358f4c2fc33a6f2816fed9",
          "diffid": "1bd699e76a3f5cdf914ba6f7d5619a5354ec76a6efa728fb46418d4cbf9c663e"
        }
      ],
      "allotments_size": 2
    }
  ],
  "rows_size": 2,
  "owner": ""
}`

func makeTestManifest(mediaType string) TdfsManifest {
	field, err := tdfs.GetField().Unmarshal(string(expectedManifestSerialization))
	if err != nil {
		panic(err)
	}
	return TdfsManifest{
		MediaType: mediaType,
		Field:     field,
	}
}

func TestManifest(t *testing.T) {
	mfst := makeTestManifest(MediaTypeTdfsLayer)

	marshalled := mfst.Field.Marshal()
	expectedSerialized := strings.ReplaceAll(expectedManifestSerialization, "\n", "")
	expectedSerialized = strings.ReplaceAll(expectedSerialized, " ", "")
	if marshalled != expectedSerialized {
		t.Fatal("unexpected marshalled manifest, GOT: ", marshalled, "EXPECTED: ", expectedSerialized)
	}

	//check rows
	totAllotments := 0
	for allotment := range mfst.Field.IterateAllotments() {
		totAllotments++
		if allotment.Row == 0 && allotment.Col == 0 {
			if allotment.Digest != "4125b344c065ea823f46ad3ea56b468398d6a71cee2c853f38594741aca8d6d2" {
				t.Fatalf("unexpected digest for allotment (0,0): %s", allotment.Digest)
			}
			if allotment.DiffID != "76d9d151de596d0b6031e9263da644d8ded6fbdd201d68840d2bdf08d2f187dd" {
				t.Fatalf("unexpected diffid for allotment (0,0): %s", allotment.DiffID)
			}
		} else if allotment.Row == 0 && allotment.Col == 1 {
			if allotment.Digest != "dd631a30bc67c8edd567016dfe36af2719de13c9f8af9abe77d9da49245f7d30" {
				t.Fatalf("unexpected digest for allotment (0,1): %s", allotment.Digest)
			}
			if allotment.DiffID != "3b3f37d82931d236475d88a7c91696b378f360bc870bb776eacb584fd8f95655" {
				t.Fatalf("unexpected diffid for allotment (0,1): %s", allotment.DiffID)
			}
		} else if allotment.Row == 1 && allotment.Col == 0 {
			if allotment.Digest != "ba23244b835211f0cc7876851770b60cd183fb5b1c874c83d5108e5ba9c73999" {
				t.Fatalf("unexpected digest for allotment (1,0): %s", allotment.Digest)
			}
			if allotment.DiffID != "dd377c1de373dd32503f845b14c0ef7c6243f8169e01bf95277641ec62607e6d" {
				t.Fatalf("unexpected diffid for allotment (1,0): %s", allotment.DiffID)
			}
		} else if allotment.Row == 1 && allotment.Col == 1 {
			if allotment.Digest != "70284438af4d85e203e7deedceb17556135b7b91ed358f4c2fc33a6f2816fed9" {
				t.Fatalf("unexpected digest for allotment (1,1): %s", allotment.Digest)
			}
			if allotment.DiffID != "1bd699e76a3f5cdf914ba6f7d5619a5354ec76a6efa728fb46418d4cbf9c663e" {
				t.Fatalf("unexpected diffid for allotment (1,1): %s", allotment.DiffID)
			}
		} else {
			t.Fatalf("unexpected row/col: (%d,%d)", allotment.Row, allotment.Col)
		}
	}
	if totAllotments != 4 {
		t.Fatalf("unexpected number of allotments: %d", totAllotments)
	}
}
