package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNeedsUpdate(t *testing.T) {
	currentVer := "1.3.0"

	otherVer := []struct {
		version string
		update  bool
	}{
		{
			version: "0.4.0",
			update:  false,
		},
		{
			version: "1.2.9",
			update:  false,
		},
		{
			version: "1.3.0",
			update:  false,
		},
		{
			version: "1.3.1",
			update:  true,
		},
		{
			version: "1.3.1-rc.0",
			update:  true,
		},
		{
			version: "2.0.0-rc.1",
			update:  true,
		},
		{
			version: "2.0.0",
			update:  true,
		},
	}

	for _, val := range otherVer {
		update, err := needsUpdate(currentVer, val.version)
		require.NoError(t, err)
		require.Equal(t, val.update, update)
	}
}
