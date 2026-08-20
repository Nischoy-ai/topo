package store_test

import (
	"testing"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/internal/store/storetest"
)

func TestMemoryConformsToRepository(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Repository { return store.NewMemory() })
}
