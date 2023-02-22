package merger

import (
	//	"context"
	//"fmt"

	"context"
	"testing"
	//	"time"

	//	"github.com/streamingfast/bstream"
	//"github.com/streamingfast/sadia1971/bundle"
	"github.com/streamingfast/bstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var block99 = bstream.MustNewOneBlockFile("0000000099-0000000000000099a-0000000000000098a-97-suffix")
var block100 = bstream.MustNewOneBlockFile("0000000100-0000000000000100a-0000000000000099a-98-suffix")
var block101 = bstream.MustNewOneBlockFile("0000000101-0000000000000101a-0000000000000100a-99-suffix")
var block102Final100 = bstream.MustNewOneBlockFile("0000000102-0000000000000102a-0000000000000101a-100-suffix")
var block103Final101 = bstream.MustNewOneBlockFile("0000000103-0000000000000103a-0000000000000102a-101-suffix")
var block104Final102 = bstream.MustNewOneBlockFile("0000000104-0000000000000104a-0000000000000103a-102-suffix")
var block105Final103 = bstream.MustNewOneBlockFile("0000000105-0000000000000105a-0000000000000104a-103-suffix")
var block106Final104 = bstream.MustNewOneBlockFile("0000000106-0000000000000106a-0000000000000105a-104-suffix")

func init() {
	bstream.GetBlockReaderFactory = bstream.TestBlockReaderFactory
}

func TestNewBundler(t *testing.T) {
	b := NewBundler(100, 200, 2, 100, nil)
	require.NotNil(t, b)
	assert.EqualValues(t, 100, b.bundleSize)
	assert.EqualValues(t, 200, b.stopBlock)
	assert.NotNil(t, b.bundleError)
	assert.NotNil(t, b.seenBlockFiles)
}

func TestBundlerReset(t *testing.T) {
	b := NewBundler(100, 200, 2, 2, nil) // merge every 2 blocks

	b.irreversibleBlocks = []*bstream.OneBlockFile{block100, block101}
	b.Reset(102, block100.ToBstreamBlock().AsRef())
	assert.Nil(t, b.irreversibleBlocks)
	assert.EqualValues(t, 102, b.baseBlockNum)

}

func TestBundlerMergeKeepOne(t *testing.T) {

	tests := []struct {
		name            string
		inBlocks        []*bstream.OneBlockFile
		expectRemaining []*bstream.OneBlockFile
		expectBase      uint64
		expectMerged    []uint64
	}{
		{
			name: "vanilla",
			inBlocks: []*bstream.OneBlockFile{
				block100,
				block101,
				block102Final100,
				block103Final101,
				block104Final102,
			},
			expectRemaining: []*bstream.OneBlockFile{
				block101,
				block102Final100,
			},
			expectBase:   102,
			expectMerged: []uint64{100},
		},
		{
			name: "vanilla_plus_one",
			inBlocks: []*bstream.OneBlockFile{
				block100,
				block101,
				block102Final100,
				block103Final101,
				block104Final102,
				block105Final103,
			},
			expectRemaining: []*bstream.OneBlockFile{
				block101,
				block102Final100,
				block103Final101,
			},
			expectBase:   102,
			expectMerged: []uint64{100},
		},
		{
			name: "twoMerges",
			inBlocks: []*bstream.OneBlockFile{
				block100,
				block101,
				block102Final100,
				block103Final101,
				block104Final102,
				block105Final103,
				block106Final104,
			},
			expectRemaining: []*bstream.OneBlockFile{
				block103Final101,
				block104Final102,
			},
			expectBase:   104,
			expectMerged: []uint64{102},
		},
	}

	for _, c := range tests {

		t.Run(c.name, func(t *testing.T) {
			var merged []uint64
			b := NewBundler(100, 200, 2, 2, &TestMergerIO{
				MergeAndStoreFunc: func(_ context.Context, inclusiveLowerBlock uint64, _ []*bstream.OneBlockFile) (err error) {
					merged = append(merged, inclusiveLowerBlock)
					return nil
				},
			}) // merge every 2 blocks
			b.irreversibleBlocks = []*bstream.OneBlockFile{block100, block101}

			for _, blk := range c.inBlocks {
				require.NoError(t, b.HandleBlockFile(blk))
			}

			// wait for MergeAndStore
			b.inProcess.Lock()
			b.inProcess.Unlock()

			assert.Equal(t, c.expectRemaining, b.irreversibleBlocks)
			assert.Equal(t, c.expectBase, b.baseBlockNum)
		})
	}
}
