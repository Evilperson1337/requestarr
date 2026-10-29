package snapshottest

import "testing"

func TestMemoryAdapterHoldsTheContract(t *testing.T) {
	Run(t, func(*testing.T) Backend { return NewMemory() })
}
