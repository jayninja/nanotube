// Package rec includes everything related to datapoint record.
package rec

import (
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// RecBytes represents a single piece of data (a datapoint) that can be sent.
type RecBytes struct { // nolint:revive
	Path    []byte
	Val     float64
	RawVal  []byte // this is to avoid discrepancies in precision and formatting
	Time    uint32
	RawTime []byte // to avoid differences when encoding, and save time
	//	Raw  string // to avoid wasting time for serialization
	Received time.Time
}

// ParseRecBytes parses a single datapoint record from a string. Makes sure it's valid.
// Performs normalizations.
func ParseRecBytes(s []byte, normalize bool, shouldLog bool, nowF func() time.Time, lg *zap.Logger) (*RecBytes, error) {
	// strings.Fields does normalization by working with any number and any kind of whitespace
	pathStart, pathEnd, valStart, valEnd, timeStart, timeEnd, err := recFields(s)
	if err != nil {
		return nil, errors.Wrap(err, "failed to break record into fields")
	}

	var path []byte
	if normalize {
		path, _, err = normalizePathBytes(s[pathStart:pathEnd])
		if err != nil {
			return nil, errors.Wrap(err, "failed to normalize path")
		}
	} else {
		path = s[pathStart:pathEnd]
	}

	return &RecBytes{
		Path:     path,
		RawVal:   s[valStart:valEnd],
		RawTime:  s[timeStart:timeEnd],
		Received: nowF(),
	}, nil
}

func recFields(s []byte) (pathStart, pathEnd, valStart, valEnd, timeStart, timeEnd int, retErr error) {
	pathStart, pathEnd, err := getField(s, 0)
	if err != nil {
		retErr = errors.Wrap(err, "failed to find path in record")
		return
	}

	valStart, valEnd, err = getField(s, pathEnd)
	if err != nil {
		retErr = errors.Wrap(err, "failed to find value in record")
		return
	}

	timeStart, timeEnd, err = getField(s, valEnd)
	if err != nil {
		retErr = errors.Wrap(err, "failed to find time in record")
		return
	}

	return
}

func getField(s []byte, st int) (int, int, error) {
	if st == len(s) {
		return st, st, errors.New("start point beyond the string end")
	}

	start := st
	for ; start < len(s) && isWhitespace(s[start]); start++ {
	}
	if start == len(s) {
		return st, st, errors.New("string contains only whitespace")
	}

	end := start + 1
	for ; end < len(s) && !isWhitespace(s[end]); end++ {
	}

	return start, end, nil
}

func isWhitespace(c byte) bool {
	return c == byte(' ') || c == byte('\t')
}

// Serialize makes record into a string ready to be sent via TCP w/ line protocol.
func (r *RecBytes) Serialize() []byte {
	// TODO (grzkv): Copy can be avoided if string was not changed
	res := make([]byte, 0, len(r.Path)+len(r.RawTime)+len(r.RawVal)+3)
	res = append(res, r.Path...)
	res = append(res, ' ')
	res = append(res, r.RawVal...)
	res = append(res, ' ')
	res = append(res, r.RawTime...)
	res = append(res, '\n')

	return res
}

// normalizePath does path normalization as described in the docs
// returns: (updated path, was any normalization done)
func normalizePathBytes(s []byte) ([]byte, bool, error) {
	if len(s) == 0 {
		return []byte{}, false, nil
	}

	start := 0
	for ; start < len(s) && s[start] == '.'; start++ {
	}
	if start == len(s) {
		return []byte{}, true, errors.New("path contains only dots")
	}

	end := len(s) - 1 // points to the last char in path
	for ; end >= start && s[end] == '.'; end-- {
	}
	// check for string consisting only of points was done above

	needsNormalization := false
	for i := start; i <= end; i++ {
		if s[i] == '.' {
			if s[i+1] == '.' { // safe, a dot cannot be the last
				needsNormalization = true
				break
			}
		}

		if !validChar(s[i]) {
			needsNormalization = true
			break
		}
	}

	res := []byte{}
	if needsNormalization {
		for i := start; i <= end; i++ {
			if s[i] == '.' {
				// a dot cannot be the last char
				if s[i+1] == '.' {
					continue
				}
			}

			if validChar(s[i]) {
				res = append(res, s[i])
			} else {
				res = append(res, byte('_'))
			}
		}
	} else {
		if start == 0 && end == len(s)-1 {
			return s, false, nil
		}
		res = s[start : end+1]
		return res, true, nil
	}

	return res, true, nil
}

// Copy returns a deep copy of the record
func (r RecBytes) Copy() *RecBytes {
	cpy := &RecBytes{
		Val:      r.Val,
		Time:     r.Time,
		Received: r.Received,
	}
	copy(cpy.Path, r.Path)
	copy(cpy.RawVal, r.RawVal)
	copy(cpy.RawTime, r.RawTime)

	return cpy
}
