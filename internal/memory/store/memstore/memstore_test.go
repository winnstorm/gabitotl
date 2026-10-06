package memstore

import (
	"testing"

	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.Store { return New() })
}
