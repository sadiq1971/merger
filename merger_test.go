// Copyright 2019 dfuse Platform Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package merger

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/streamingfast/merger/bundle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBundler(nextBundle, lowestPossibleBundle, bundleSize uint64) *bundle.Bundler {
	return bundle.NewBundler(testLogger, nextBundle, lowestPossibleBundle, bundleSize)
}

type TestMergerIO struct {
	MergeAndSaveFunc             func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error)
	FetchMergedOneBlockFilesFunc func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error)
	DownloadOneBlockFileFunc     func(ctx context.Context, oneBlockFile *bundle.OneBlockFile) (data []byte, err error)
	WalkOneBlockFilesFunc        func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error
}

func (io *TestMergerIO) MergeAndStore(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
	if io.MergeAndSaveFunc != nil {
		return io.MergeAndSaveFunc(inclusiveLowerBlock, oneBlockFiles)
	}

	return nil
}

func (io *TestMergerIO) FetchMergedOneBlockFiles(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
	if io.FetchMergedOneBlockFilesFunc != nil {
		return io.FetchMergedOneBlockFilesFunc(lowBlockNum)
	}

	return nil, nil
}

func (io *TestMergerIO) DownloadOneBlockFile(ctx context.Context, oneBlockFile *bundle.OneBlockFile) (data []byte, err error) {
	if io.DownloadOneBlockFileFunc != nil {
		return io.DownloadOneBlockFileFunc(ctx, oneBlockFile)
	}

	return nil, nil
}

func (io *TestMergerIO) WalkOneBlockFiles(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
	if io.WalkOneBlockFilesFunc != nil {
		return io.WalkOneBlockFilesFunc(ctx, callback)
	}
	return nil

}

func TestNewMerger_SunnyPath(t *testing.T) {
	bundler := newBundler(0, 0, 5)

	mergerIO := &TestMergerIO{}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		return nil, fmt.Errorf("nada")
	}

	srcOneBlockFiles := []*bundle.OneBlockFile{
		bundle.MustNewOneBlockFile("0000000001-20210728T105016.01-00000001a-00000000a-0-suffix"),
		bundle.MustNewOneBlockFile("0000000002-20210728T105016.02-00000002a-00000001a-0-suffix"),
		bundle.MustNewOneBlockFile("0000000003-20210728T105016.03-00000003a-00000002a-0-suffix"),
		bundle.MustNewOneBlockFile("0000000004-20210728T105016.06-00000004a-00000003a-2-suffix"),
		bundle.MustNewOneBlockFile("0000000006-20210728T105016.08-00000006a-00000004a-2-suffix"),
	}
	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		for _, o := range srcOneBlockFiles {
			if err := callback(o); err != nil {
				return err
			}
		}
		return nil
	}

	var mergedFiles []*bundle.OneBlockFile
	mergerIO.MergeAndSaveFunc = func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
		defer merger.Shutdown(nil)
		mergedFiles = oneBlockFiles
		return nil
	}

	var deletedFiles []*bundle.OneBlockFile
	merger.deleteFilesFunc = func(oneBlockFiles []*bundle.OneBlockFile) {
		deletedFiles = append(deletedFiles, oneBlockFiles...)
	}

	go func() {
		select {
		case <-time.After(time.Second):
			panic("too long")
		case <-merger.Terminated():
		}
	}()

	err := merger.launch()
	require.NoError(t, err)

	assert.Len(t, deletedFiles, 4)
	assert.Equal(t, bundle.ToSortedIDs(srcOneBlockFiles[0:4]), bundle.ToSortedIDs(deletedFiles))
	assert.Len(t, mergedFiles, 4)
	assert.Equal(t, srcOneBlockFiles[0:4], mergedFiles)

}

func TestNewMerger_Unlinkable_File(t *testing.T) {
	bundler := newBundler(0, 0, 5)

	mergerIO := &TestMergerIO{}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		return nil, fmt.Errorf("nada")
	}

	srcOneBlockFiles := []*bundle.OneBlockFile{
		bundle.MustNewOneBlockFile("0000000001-20210728T105016.01-00000001a-00000000a-0-suffix"),
		bundle.MustNewOneBlockFile("0000000002-20210728T105016.02-00000002a-00000001a-0-suffix"),
		bundle.MustNewOneBlockFile("0000000003-20210728T105016.03-00000003a-00000002a-0-suffix"),
		bundle.MustNewOneBlockFile("0000000004-20210728T105016.06-00000004a-00000003a-3-suffix"),
		bundle.MustNewOneBlockFile("0000000006-20210728T105016.08-00000006a-00000004a-4-suffix"),
		bundle.MustNewOneBlockFile("0000000002-20210728T105016.09-00000002b-00000001b-0-suffix"), //un linkable file
	}

	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		for _, o := range srcOneBlockFiles {
			if err := callback(o); err != nil {
				return err
			}
		}
		return nil
	}

	var deletedFiles []*bundle.OneBlockFile
	merger.deleteFilesFunc = func(oneBlockFiles []*bundle.OneBlockFile) {
		deletedFiles = append(deletedFiles, oneBlockFiles...)
	}

	var mergedFiles []*bundle.OneBlockFile
	mergerIO.MergeAndSaveFunc = func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
		defer merger.Shutdown(nil)
		mergedFiles = oneBlockFiles
		return nil
	}

	go func() {
		select {
		case <-time.After(time.Second):
			panic("too long")
		case <-merger.Terminated():
		}
	}()

	err := merger.launch()
	require.NoError(t, err)

	expectedDeleted := append(clone(srcOneBlockFiles[0:4]), srcOneBlockFiles[5])
	require.Equal(t, bundle.ToSortedIDs(expectedDeleted), bundle.ToSortedIDs(deletedFiles))

	expectedMerged := append(clone(srcOneBlockFiles[0:4]), srcOneBlockFiles[5])
	require.Equal(t, bundle.ToIDs(expectedMerged), bundle.ToIDs(mergedFiles))
}

func TestNewMerger_File_Too_Old(t *testing.T) {
	bundler := newBundler(0, 0, 5)

	mergerIO := &TestMergerIO{}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		return nil, fmt.Errorf("nada")
	}

	srcOneBlockFiles := [][]*bundle.OneBlockFile{
		{
			bundle.MustNewOneBlockFile("0000000001-20210728T105016.01-00000001a-00000000a-0-suffix"),
			bundle.MustNewOneBlockFile("0000000002-20210728T105016.02-00000002a-00000001a-0-suffix"),
			bundle.MustNewOneBlockFile("0000000003-20210728T105016.03-00000003a-00000002a-0-suffix"),
			bundle.MustNewOneBlockFile("0000000004-20210728T105016.06-00000004a-00000003a-3-suffix"),
			bundle.MustNewOneBlockFile("0000000006-20210728T105016.08-00000006a-00000004a-4-suffix"),
		},
		{
			bundle.MustNewOneBlockFile("0000000002-20210728T105016.09-00000002b-00000001b-0-suffix"), //too old
		},
	}
	walkOneBlockFilesCallCount := 0

	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		for _, o := range srcOneBlockFiles[walkOneBlockFilesCallCount] {
			if err := callback(o); err != nil {
				return err
			}
		}
		walkOneBlockFilesCallCount++
		if walkOneBlockFilesCallCount == 2 {
			defer merger.Shutdown(nil)
		}
		return nil
	}

	var deletedFiles []*bundle.OneBlockFile
	merger.deleteFilesFunc = func(oneBlockFiles []*bundle.OneBlockFile) {
		deletedFiles = append(deletedFiles, oneBlockFiles...)
	}

	var mergedFiles []*bundle.OneBlockFile
	mergerIO.MergeAndSaveFunc = func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
		mergedFiles = oneBlockFiles
		return nil
	}

	go func() {
		select {
		case <-time.After(time.Second):
			panic("too long")
		case <-merger.Terminated():
		}
	}()

	err := merger.launch()
	require.NoError(t, err)

	require.Equal(t, 2, walkOneBlockFilesCallCount)

	expectedDeleted := append(clone(srcOneBlockFiles[0][0:4]), srcOneBlockFiles[1][0]) //normal purge and too old file
	require.Equal(t, bundle.ToSortedIDs(expectedDeleted), bundle.ToSortedIDs(deletedFiles))

	expectedMerged := clone(srcOneBlockFiles[0][0:4])
	require.Equal(t, bundle.ToIDs(expectedMerged), bundle.ToIDs(mergedFiles))
}

func clone(in []*bundle.OneBlockFile) (out []*bundle.OneBlockFile) {
	out = make([]*bundle.OneBlockFile, len(in))
	copy(out, in)
	return
}

//func TestNewMerger_Wait_For_Files(t *testing.T) {
//	bundler := newBundler(0, 0, 5)
//
//	mergerIO := &TestMergerIO{}
//	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, nil)
//
//	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
//		return nil, fmt.Errorf("nada")
//	}
//
//	srcOneBlockFiles := [][]*bundle.OneBlockFile{
//		{},
//		{
//			bundle.MustNewOneBlockFile("0000000001-20210728T105016.01-00000001a-00000000a-0-suffix"),
//			bundle.MustNewOneBlockFile("0000000002-20210728T105016.02-00000002a-00000001a-0-suffix"),
//			bundle.MustNewOneBlockFile("0000000003-20210728T105016.03-00000003a-00000002a-0-suffix"),
//			bundle.MustNewOneBlockFile("0000000004-20210728T105016.06-00000004a-00000003a-2-suffix"),
//		},
//		{
//			bundle.MustNewOneBlockFile("0000000006-20210728T105016.08-00000006a-00000004a-2-suffix"),
//		},
//	}
//
//	fetchOneBlockFilesCallCount := 0
//
//	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
//		fetchOneBlockFilesCallCount++
//		for _, o := range srcOneBlockFiles[fetchOneBlockFilesCallCount] {
//			if err := callback(o); err != nil {
//				return err
//			}
//		}
//		return nil
//	}
//
//	var deletedFiles []*bundle.OneBlockFile
//	merger.deleteFilesFunc = func(oneBlockFiles []*bundle.OneBlockFile) {
//		deletedFiles = append(deletedFiles, oneBlockFiles...)
//	}
//
//	var mergedFiles []*bundle.OneBlockFile
//	mergerIO.MergeAndSaveFunc = func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
//		defer merger.Shutdown(nil)
//		mergedFiles = oneBlockFiles
//		return nil
//	}
//
//	go func() {
//		select {
//		case <-time.After(3 * time.Second):
//			panic("too long")
//		case <-merger.Terminated():
//		}
//	}()
//
//	err := merger.launch()
//	require.NoError(t, err)
//
//	assert.Equal(t, bundle.ToSortedIDs(mergedFiles), bundle.ToSortedIDs(deletedFiles))
//	assert.Equal(t, srcOneBlockFiles[1], mergedFiles)
//}

func TestNewMerger_Multiple_Merge(t *testing.T) {
	bundler := newBundler(0, 0, 5)

	mergerIO := &TestMergerIO{}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		return nil, fmt.Errorf("nada")
	}

	srcOneBlockFiles := []*bundle.OneBlockFile{
		bundle.MustNewOneBlockFile("0000000001-20210728T105016.01-00000001a-00000000a-0-suffix"),
		bundle.MustNewOneBlockFile("0000000002-20210728T105016.02-00000002a-00000001a-1-suffix"),
		bundle.MustNewOneBlockFile("0000000003-20210728T105016.03-00000003a-00000002a-1-suffix"),
		bundle.MustNewOneBlockFile("0000000004-20210728T105016.06-00000004a-00000003a-1-suffix"),

		bundle.MustNewOneBlockFile("0000000006-20210728T105016.08-00000006a-00000004a-1-suffix"),
		bundle.MustNewOneBlockFile("0000000007-20210728T105016.09-00000007a-00000006a-1-suffix"),
		bundle.MustNewOneBlockFile("0000000008-20210728T105016.10-00000008a-00000007a-1-suffix"),
		bundle.MustNewOneBlockFile("0000000009-20210728T105016.11-00000009a-00000008a-1-suffix"),

		bundle.MustNewOneBlockFile("0000000010-20210728T105016.12-00000010a-00000009a-1-suffix"),
	}

	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		for _, o := range srcOneBlockFiles {
			if err := callback(o); err != nil {
				return err
			}
		}
		return nil
	}

	var deletedFiles []*bundle.OneBlockFile
	merger.deleteFilesFunc = func(oneBlockFiles []*bundle.OneBlockFile) {
		deletedFiles = append(deletedFiles, oneBlockFiles...)
	}

	var mergedFiles []*bundle.OneBlockFile
	mergeUploadFuncCallCount := 0
	mergerIO.MergeAndSaveFunc = func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
		mergeUploadFuncCallCount++

		if mergeUploadFuncCallCount == 2 {
			defer merger.Shutdown(nil)
		}

		mergedFiles = append(mergedFiles, oneBlockFiles...)
		return nil
	}

	go func() {
		select {
		case <-time.After(time.Second):
			panic("too long")
		case <-merger.Terminated():
		}
	}()

	err := merger.launch()
	require.NoError(t, err)

	expectedDeleted := mergedFiles
	require.Equal(t, bundle.ToSortedIDs(expectedDeleted), bundle.ToSortedIDs(deletedFiles))

	require.Equal(t, 2, mergeUploadFuncCallCount)
	require.Equal(t, bundle.ToIDs(srcOneBlockFiles[0:8]), bundle.ToIDs(mergedFiles))
}

func TestNewMerger_SunnyPath_With_MergeFile_Already_Exist(t *testing.T) {
	bundler := newBundler(100, 0, 5)

	mergerIO := &TestMergerIO{}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	mergeFiles := map[uint64][]*bundle.OneBlockFile{
		100: {
			bundle.MustNewOneBlockFile("0000000100-20210728T105016.08-00000100a-00000099a-99-suffix"),
			bundle.MustNewOneBlockFile("0000000101-20210728T105016.09-00000101a-00000100a-99-suffix"),
			bundle.MustNewOneBlockFile("0000000102-20210728T105016.10-00000102a-00000101a-99-suffix"),
			bundle.MustNewOneBlockFile("0000000103-20210728T105016.11-00000103a-00000102a-99-suffix"),
			bundle.MustNewOneBlockFile("0000000104-20210728T105016.12-00000104a-00000103a-99-suffix"),
		},
		105: {
			bundle.MustNewOneBlockFile("0000000105-20210728T105016.13-00000105a-00000104a-100-suffix"),
			bundle.MustNewOneBlockFile("0000000106-20210728T105016.14-00000106a-00000105a-101-suffix"),
			bundle.MustNewOneBlockFile("0000000107-20210728T105016.15-00000107a-00000106a-102-suffix"),
			bundle.MustNewOneBlockFile("0000000108-20210728T105016.16-00000108a-00000107a-103-suffix"),
			bundle.MustNewOneBlockFile("0000000109-20210728T105016.17-00000109a-00000108a-104-suffix"),
		},
		110: {
			bundle.MustNewOneBlockFile("0000000110-20210728T105016.18-00000110a-00000109a-105-suffix"),
			bundle.MustNewOneBlockFile("0000000111-20210728T105016.19-00000111a-00000110a-106-suffix"),
			bundle.MustNewOneBlockFile("0000000112-20210728T105016.20-00000112a-00000111a-107-suffix"),
			bundle.MustNewOneBlockFile("0000000113-20210728T105016.21-00000113a-00000112a-108-suffix"),
			bundle.MustNewOneBlockFile("0000000114-20210728T105016.21-00000114a-00000113a-109-suffix"),
		},
	}

	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		oneBlockFile, found := mergeFiles[lowBlockNum]
		if !found {
			return nil, fmt.Errorf("nada")
		}
		if lowBlockNum == 110 {
			defer merger.Shutdown(nil)
		}
		return oneBlockFile, nil
	}

	cycle := 0

	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		// not actually fetching oneBlockFiles, but it is a good place to add assertions in this loop
		switch cycle {
		case 0:
			num, err := merger.bundler.LongestChainFirstBlockNum()
			require.NoError(t, err)
			require.Equal(t, merger.bundler.BundleInclusiveLowerBlock(), uint64(105))
			require.Equal(t, merger.bundler.ExclusiveHighestBlockLimit(), uint64(110))
			require.Equal(t, num, uint64(100))
		case 1:
			num, err := merger.bundler.LongestChainFirstBlockNum()
			require.NoError(t, err)
			require.Equal(t, merger.bundler.BundleInclusiveLowerBlock(), uint64(110))
			require.Equal(t, merger.bundler.ExclusiveHighestBlockLimit(), uint64(115))
			require.Equal(t, num, uint64(104))
		case 2:
			num, err := merger.bundler.LongestChainFirstBlockNum()
			require.NoError(t, err)
			require.Equal(t, merger.bundler.BundleInclusiveLowerBlock(), uint64(115))
			require.Equal(t, merger.bundler.ExclusiveHighestBlockLimit(), uint64(120))
			require.Equal(t, num, uint64(109))
		default:
			t.Fatalf("Should not happen")
		}
		cycle += 1

		return nil
	}

	merger.deleteFilesFunc = func(oneBlockFiles []*bundle.OneBlockFile) {
		t.Fatalf("Should not happen. Only forkdb should be truncated")
	}

	mergerIO.MergeAndSaveFunc = func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
		t.Fatalf("should not have been called")
		return nil
	}

	go func() {
		select {
		case <-time.After(time.Second):
			panic("too long")
		case <-merger.Terminated():
		}
	}()

	err := merger.launch()
	require.NoError(t, err)
}

func TestNewMerger_SunnyPath_With_Bootstrap(t *testing.T) {
	bundler := newBundler(5, 0, 5)

	mergerIO := &TestMergerIO{}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	mergeFiles := map[uint64][]*bundle.OneBlockFile{
		0: {
			bundle.MustNewMergedOneBlockFile("0000000001-20210728T105016.01-00000001a-00000000a-0-suffix"),
			bundle.MustNewMergedOneBlockFile("0000000002-20210728T105016.02-00000002a-00000001a-0-suffix"),
			bundle.MustNewMergedOneBlockFile("0000000003-20210728T105016.03-00000003a-00000002a-0-suffix"),
			bundle.MustNewMergedOneBlockFile("0000000004-20210728T105016.06-00000004a-00000003a-2-suffix"),
		},
	}

	var mergeFilesFetched []uint64
	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		mergeFilesFetched = append(mergeFilesFetched, lowBlockNum)
		oneBlockFile, found := mergeFiles[lowBlockNum]
		if !found {
			return nil, fmt.Errorf("nada")
		}
		return oneBlockFile, nil
	}

	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		defer merger.Shutdown(nil)
		return nil
	}

	merger.deleteFilesFunc = func(oneBlockFiles []*bundle.OneBlockFile) {
		t.Fatalf("should not have been call")
	}

	mergerIO.MergeAndSaveFunc = func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
		t.Fatalf("should not have been call")
		return nil
	}

	err := bundler.Bootstrap(mergerIO.FetchMergedOneBlockFilesFunc)
	require.NoError(t, err)

	err = merger.launch()
	require.NoError(t, err)

	require.Equal(t, []uint64{0, 0, 5}, mergeFilesFetched) //one time from the bootstrap and 2 time from main loop
}

func TestMerger_Launch_FailWalkOneBlockFiles(t *testing.T) {
	bundler := newBundler(0, 0, 5)

	mergerIO := &TestMergerIO{}
	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) { return []*bundle.OneBlockFile{}, nil }

	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		return fmt.Errorf("couldn't fetch one block files")
	}

	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	merger.Launch()
}

func TestMerger_Launch_Drift(t *testing.T) {
	c := struct {
		name                      string
		files                     []*bundle.OneBlockFile
		blockLimit                uint64
		expectedHighestBlockLimit uint64
		expectedLastMergeBlockID  string
	}{
		name: "should set metrics blocktime",
		files: []*bundle.OneBlockFile{
			bundle.MustNewOneBlockFile("0000000114-20210728T105016.0-00000114a-00000113a-90-suffix"),
			bundle.MustNewOneBlockFile("0000000115-20210728T105116.0-00000115a-00000114a-90-suffix"),
			bundle.MustNewOneBlockFile("0000000116-20210728T105216.0-00000116a-00000115a-90-suffix"),
			bundle.MustNewOneBlockFile("0000000117-20210728T105316.0-00000117a-00000116a-90-suffix"),
			bundle.MustNewOneBlockFile("0000000121-20210728T105416.0-00000121a-00000117b-90-suffix"),
		},
		blockLimit:                110,
		expectedHighestBlockLimit: 117,
		expectedLastMergeBlockID:  "00000117a",
	}

	bundler := newBundler(c.blockLimit, 0, 10)
	for _, f := range c.files {
		bundler.AddOneBlockFile(f)
	}

	bundler.Commit(c.blockLimit)

	var areWeDoneYet uint64
	done := make(chan struct{})
	fetchMergedFiles := func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		if areWeDoneYet == 1 {
			close(done)
		}
		areWeDoneYet += 1
		return []*bundle.OneBlockFile{}, nil
	}

	walkOneBlockFiles := func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		for _, f := range c.files {
			if err := callback(f); err != nil {
				return err
			}
		}
		return nil
	}

	mergerIO := &TestMergerIO{
		WalkOneBlockFilesFunc:        walkOneBlockFiles,
		FetchMergedOneBlockFilesFunc: fetchMergedFiles,
	}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	go merger.Launch()
	select {
	case <-time.After(3 * time.Second):
		t.Fail()
	case <-done:
		merger.Shutdown(nil)
	}
}

func TestMerger_PreMergedBlocks_Purge(t *testing.T) {
	//todo: froch this test is failing when run with...
	//go test -count 100000 -run TestMerger_PreMergedBlocks_Purge ./...

	c := struct {
		name          string
		mergedFiles   map[uint64][]*bundle.OneBlockFile
		oneBlockFiles []*bundle.OneBlockFile
	}{
		name: "",
		mergedFiles: map[uint64][]*bundle.OneBlockFile{
			uint64(113): {
				bundle.MustNewOneBlockFile("0000000113-20210728T105016.0-00000113a-00000112a-90-suffix"),
				bundle.MustNewOneBlockFile("0000000114-20210728T105016.0-00000114a-00000113a-113-suffix"),
				bundle.MustNewOneBlockFile("0000000115-20210728T105116.0-00000115a-00000114a-113-suffix"),
				bundle.MustNewOneBlockFile("0000000116-20210728T105216.0-00000116a-00000115a-113-suffix"),
				bundle.MustNewOneBlockFile("0000000117-20210728T105316.0-00000117a-00000116a-113-suffix"),
			},
			uint64(118): {
				bundle.MustNewOneBlockFile("0000000118-20210728T105016.0-00000118a-00000117a-117-suffix"),
				bundle.MustNewOneBlockFile("0000000119-20210728T105116.0-00000119a-00000118a-117-suffix"),
				bundle.MustNewOneBlockFile("0000000120-20210728T105216.0-00000120a-00000119a-117-suffix"),
				bundle.MustNewOneBlockFile("0000000121-20210728T105316.0-00000121a-00000120a-117-suffix"),
				bundle.MustNewOneBlockFile("0000000122-20210728T105016.0-00000122a-00000121a-117-suffix"),
			},
		},
		oneBlockFiles: []*bundle.OneBlockFile{
			bundle.MustNewOneBlockFile("0000000123-20210728T105116.0-00000123a-00000122a-122-suffix"),
			bundle.MustNewOneBlockFile("0000000124-20210728T105216.0-00000124a-00000123a-122-suffix"),
			bundle.MustNewOneBlockFile("0000000125-20210728T105316.0-00000125a-00000124a-122-suffix"),
			bundle.MustNewOneBlockFile("0000000126-20210728T105416.0-00000126a-00000125a-122-suffix"),
			bundle.MustNewOneBlockFile("0000000127-20210728T105416.0-00000127a-00000126a-122-suffix"),
			bundle.MustNewOneBlockFile("0000000128-20210728T105416.0-00000128a-00000127a-122-suffix"),
		},
	}

	bundler := newBundler(113, 0, 5)

	mergerIO := &TestMergerIO{}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	mergerIO.FetchMergedOneBlockFilesFunc = func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		return c.mergedFiles[lowBlockNum], nil
	}

	cycleCount := 0

	mergerIO.WalkOneBlockFilesFunc = func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		cycleCount += 1
		if cycleCount == 2 {
			for _, o := range c.oneBlockFiles {
				if err := callback(o); err != nil {
					return err
				}
			}
		}
		return nil
	}

	mergerIO.MergeAndSaveFunc = func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
		if cycleCount == 2 {
			merger.Shutdown(io.EOF)
		}
		return nil
	}

	merger.deleteFilesFunc = func(oneBlockFiles []*bundle.OneBlockFile) {
		return
	}

	go merger.Launch()
	select {
	case <-time.After(30 * time.Second):
		t.Error("too long")
	case <-merger.Terminated():
		require.Equal(t, 133, int(merger.bundler.ExclusiveHighestBlockLimit()))
		merger.Shutdown(nil)
	}
}

func TestMerger_Launch_MergeUploadError(t *testing.T) {
	c := struct {
		name                      string
		files                     []*bundle.OneBlockFile
		blockLimit                uint64
		expectedHighestBlockLimit uint64
		expectedLastMergeBlockID  string
	}{
		name: "sunny path",
		files: []*bundle.OneBlockFile{
			bundle.MustNewOneBlockFile("0000000114-20210728T105016.0-00000114a-00000113a-90-suffix"),
			bundle.MustNewOneBlockFile("0000000115-20210728T105116.0-00000115a-00000114a-114-suffix"),
			bundle.MustNewOneBlockFile("0000000116-20210728T105216.0-00000116a-00000115a-114-suffix"),
			bundle.MustNewOneBlockFile("0000000117-20210728T105316.0-00000117a-00000116a-114-suffix"),
			bundle.MustNewOneBlockFile("0000000118-20210728T105316.0-00000118a-00000117a-114-suffix"),
		},
		blockLimit:                113,
		expectedHighestBlockLimit: 118,
		expectedLastMergeBlockID:  "00000119a",
	}

	bundler := newBundler(c.blockLimit, 0, 5)
	for _, f := range c.files {
		bundler.AddOneBlockFile(f)
	}

	fetchMergedFile := func(lowBlockNum uint64) ([]*bundle.OneBlockFile, error) {
		return []*bundle.OneBlockFile{}, nil
	}

	walkOneBlockFiles := func(ctx context.Context, callback func(*bundle.OneBlockFile) error) error {
		for _, f := range c.files {
			if err := callback(f); err != nil {
				return err
			}
		}
		return nil
	}

	mergeUpload := func(inclusiveLowerBlock uint64, oneBlockFiles []*bundle.OneBlockFile) (err error) {
		return fmt.Errorf("yo")
	}

	mergerIO := &TestMergerIO{
		FetchMergedOneBlockFilesFunc: fetchMergedFile,
		WalkOneBlockFilesFunc:        walkOneBlockFiles,
		MergeAndSaveFunc:             mergeUpload,
	}
	merger := NewMerger(testLogger, bundler, time.Second, 10, "", mergerIO, time.Second, nil)

	err := merger.launch()
	require.Error(t, err)
	require.Errorf(t, err, "yo")
}
