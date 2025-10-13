package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNeedsUpdate(t *testing.T) {
	otherVer := []struct {
		currentVer string
		otherVer   string
		update     bool
	}{
		{
			currentVer: "1.3.0",
			otherVer:   "0.4.0",
			update:     false,
		},
		{
			currentVer: "1.3.0",
			otherVer:   "1.2.9",
			update:     false,
		},
		{
			currentVer: "1.3.0",
			otherVer:   "1.3.0-rc.0",
			update:     false,
		},
		{
			currentVer: "1.3.0",
			otherVer:   "1.3.0",
			update:     false,
		},
		{
			currentVer: "1.3.0",
			otherVer:   "1.3.1",
			update:     true,
		},
		{
			currentVer: "1.3.0",
			otherVer:   "2.0.0-rc.1",
			update:     false,
		},
		{
			currentVer: "1.3.0",
			otherVer:   "2.0.0-beta.1",
			update:     false,
		},
		{
			currentVer: "1.3.0-rc.1",
			otherVer:   "1.3.0-rc.2",
			update:     true,
		},
		{
			currentVer: "1.3.0-rc.3",
			otherVer:   "1.3.0-rc.1",
			update:     false,
		},
		{
			currentVer: "1.3.0-rc.3",
			otherVer:   "1.4.0-rc.1",
			update:     true,
		},
		{
			currentVer: "1.3.0",
			otherVer:   "2.0.0",
			update:     true,
		},
		{
			currentVer: "dev",
			otherVer:   "9.9.9",
			update:     false,
		},
	}

	for _, val := range otherVer {
		t.Run(fmt.Sprintf("from %s to %s", val.currentVer, val.otherVer), func(t *testing.T) {
			update, err := needsUpdate(val.currentVer, val.otherVer)
			require.NoError(t, err)
			require.Equal(t, val.update, update)
		})
	}
}
