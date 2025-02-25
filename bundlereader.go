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

	"github.com/streamingfast/bstream"
	"github.com/streamingfast/logging"
	"go.uber.org/zap"
)

type BundleReader struct {
	ctx              context.Context
	readBuffer       []byte
	readBufferOffset int
	headerPassed     bool
	oneBlockDataChan chan []byte
	errChan          chan error

	logger *zap.Logger
}

func NewBundleReader(ctx context.Context, logger *zap.Logger, tracer logging.Tracer, oneBlockFiles []*bstream.OneBlockFile, oneBlockDownloader bstream.OneBlockDownloaderFunc) *BundleReader {
	r := &BundleReader{
		ctx:              ctx,
		logger:           logger,
		oneBlockDataChan: make(chan []byte, 1),
		errChan:          make(chan error, 1),
	}
	go r.downloadAll(oneBlockFiles, oneBlockDownloader)
	return r
}

// downloadAll does not work in parallel: for performance, the oneBlockFiles' data should already have been memoized by calling Data() on them.
func (r *BundleReader) downloadAll(oneBlockFiles []*bstream.OneBlockFile, oneBlockDownloader bstream.OneBlockDownloaderFunc) {
	defer close(r.oneBlockDataChan)
	for _, oneBlockFile := range oneBlockFiles {
		data, err := oneBlockFile.Data(r.ctx, oneBlockDownloader)
		if err != nil {
			r.errChan <- err
			return
		}
		r.oneBlockDataChan <- data
	}
}

func (r *BundleReader) Read(p []byte) (bytesRead int, err error) {

	if r.readBuffer == nil {

		var data []byte
		select {
		case d, ok := <-r.oneBlockDataChan:
			if !ok {
				return 0, io.EOF
			}
			data = d
		case err := <-r.errChan:
			return 0, err
		case <-r.ctx.Done():
			return 0, nil
		}

		if len(data) == 0 {
			r.readBuffer = nil
			return 0, fmt.Errorf("one-block-file corrupt: empty data")
		}

		if r.headerPassed {
			if len(data) < bstream.GetBlockWriterHeaderLen {
				return 0, fmt.Errorf("one-block-file corrupt: expected header size of %d, but file size is only %d bytes", bstream.GetBlockWriterHeaderLen, len(data))
			}
			data = data[bstream.GetBlockWriterHeaderLen:]
		} else {
			r.headerPassed = true
		}
		r.readBuffer = data
		r.readBufferOffset = 0
	}
	// there are still bytes to be read
	bytesRead = copy(p, r.readBuffer[r.readBufferOffset:])
	r.readBufferOffset += bytesRead
	if r.readBufferOffset >= len(r.readBuffer) {
		r.readBuffer = nil
	}

	return bytesRead, nil
}
