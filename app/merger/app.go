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
	"time"

	"github.com/streamingfast/bstream"
	"github.com/streamingfast/dgrpc"
	"github.com/streamingfast/dmetrics"
	"github.com/streamingfast/dstore"
	"github.com/streamingfast/merger"
	"github.com/streamingfast/merger/bundle"
	"github.com/streamingfast/merger/metrics"
	"github.com/streamingfast/shutter"
	"go.uber.org/zap"
	pbhealth "google.golang.org/grpc/health/grpc_health_v1"
)

type Config struct {
	StorageOneBlockFilesPath     string
	StorageMergedBlocksFilesPath string
	GRPCListenAddr               string

	// perf tweak
	WritersLeewayDuration          time.Duration
	TimeBetweenStoreLookups        time.Duration
	OneBlockDeletionThreads        int
	MaxOneBlockOperationsBatchSize int
}

type App struct {
	*shutter.Shutter
	config         *Config
	readinessProbe pbhealth.HealthClient
}

func New(config *Config) *App {
	return &App{
		Shutter: shutter.New(),
		config:  config,
	}
}

func (a *App) Run() error {
	zlog.Info("running merger", zap.Reflect("config", a.config))

	if a.config.OneBlockDeletionThreads < 1 {
		return fmt.Errorf("need at least 1 OneBlockDeletionThread")
	}
	if a.config.MaxOneBlockOperationsBatchSize < 250 {
		return fmt.Errorf("minimum MaxOneBlockOperationsBatchSize is 250")
	}

	dmetrics.Register(metrics.MetricSet)

	oneBlockStoreStore, err := dstore.NewDBinStore(a.config.StorageOneBlockFilesPath)
	if err != nil {
		return fmt.Errorf("failed to init source archive store: %w", err)
	}

	mergedBlocksStore, err := dstore.NewDBinStore(a.config.StorageMergedBlocksFilesPath)
	if err != nil {
		return fmt.Errorf("failed to init destination archive store: %w", err)
	}

	bundleSize := uint64(100)

	io := merger.NewDStoreIO(oneBlockStoreStore, mergedBlocksStore, 5, 500*time.Millisecond, bstream.GetProtocolFirstStreamableBlock, bundleSize)
	filesDeleter := merger.NewOneBlockFilesDeleter(oneBlockStoreStore)

	nextBundle, err := io.FindStartBlock(context.Background())
	if err != nil {
		return err
	}

	bundler := bundle.NewBundler(nextBundle, bstream.GetProtocolFirstStreamableBlock, bundleSize)
	err = bundler.Bootstrap(func(lowBlockNum uint64) (oneBlockFiles []*bundle.OneBlockFile, err error) {
		oneBlockFiles, fetchErr := io.FetchMergedOneBlockFiles(lowBlockNum)
		if fetchErr != nil {
			return nil, fmt.Errorf("fetching one block files from merged file with low block num %d: %w", lowBlockNum, fetchErr)
		}
		return oneBlockFiles, err
	})
	if err != nil {
		return fmt.Errorf("bundle bootstrap: %w", err)
	}

	m := merger.NewMerger(
		bundler,
		a.config.TimeBetweenStoreLookups,
		a.config.MaxOneBlockOperationsBatchSize,
		a.config.GRPCListenAddr,
		io,
		filesDeleter.Delete,
	)
	zlog.Info("merger initiated")

	gs, err := dgrpc.NewInternalClient(a.config.GRPCListenAddr)
	if err != nil {
		return fmt.Errorf("cannot create readiness probe")
	}
	a.readinessProbe = pbhealth.NewHealthClient(gs)

	a.OnTerminating(m.Shutdown)
	m.OnTerminated(a.Shutdown)

	filesDeleter.Start(a.config.OneBlockDeletionThreads, 100000)
	go m.Launch()

	zlog.Info("merger running")
	return nil
}

func (a *App) IsReady() bool {
	if a.readinessProbe == nil {
		return false
	}

	resp, err := a.readinessProbe.Check(context.Background(), &pbhealth.HealthCheckRequest{})
	if err != nil {
		zlog.Info("merger readiness probe error", zap.Error(err))
		return false
	}

	if resp.Status == pbhealth.HealthCheckResponse_SERVING {
		return true
	}

	return false
}
