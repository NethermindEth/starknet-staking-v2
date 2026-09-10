package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const v130 = "1.3.0"

func TestNeedsUpdate(t *testing.T) {
	otherVer := []struct {
		currentVer string
		otherVer   string
		update     bool
	}{
		{
			currentVer: v130,
			otherVer:   "0.4.0",
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   "1.2.9",
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   "1.3.0-rc.0",
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   v130,
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   "1.3.1",
			update:     true,
		},
		{
			currentVer: v130,
			otherVer:   "2.0.0-rc.1",
			update:     false,
		},
		{
			currentVer: v130,
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
			currentVer: v130,
			otherVer:   "2.0.0",
			update:     true,
		},
		{
			currentVer: "dev",
			otherVer:   "0.0.0",
			update:     true,
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
