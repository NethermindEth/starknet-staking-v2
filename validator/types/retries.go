package types

import (
	"errors"
	"fmt"
	"strconv"
)

type Retries struct {
	infinite bool
	value    uint64
}

// Return a new Retries type set to infinite
func NewRetries() Retries {
	return Retries{
		infinite: true,
		value:    0,
	}
}

func RetriesFromString(s string) (Retries, error) {
	r := NewRetries()
	if s == "infinite" {
		return r, nil
	}
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return Retries{},
			fmt.Errorf(
				"cannot create retries from string:"+
					" `%s` is neither a positive number nor the `infinite` key word",
				s,
			)
	}
	if val == 0 {
		return Retries{}, errors.New("retries value should be greater or equal than one")
	}
	r.Set(val)

	return r, nil
}

func (r *Retries) Set(val uint64) {
	r.infinite = false
	r.value = val
}

func (r *Retries) Sub() {
	if r.infinite {
		return
	}
	if r.value == 0 {
		panic("underflow error, Retries is already zero")
	}
	r.value--
}

func (r *Retries) IsZero() bool {
	return !r.infinite && r.value == 0
}

func (r *Retries) String() string {
	if r.infinite {
		return "infinite"
	}

	return strconv.FormatUint(r.value, 10)
}
