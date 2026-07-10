package api

import (
	"bytes"
	"errors"
	"strconv"

	"github.com/makia9879/makia-notice/internal/store"
)

func newBytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func itoa(n int) string { return strconv.Itoa(n) }

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }
