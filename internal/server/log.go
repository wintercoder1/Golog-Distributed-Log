package server

import "errors"

// import (
//
//	"errors"
//	"sync"
//
// )
//
//	type Log struct {
//		mu      sync.Mutex
//		records []Record
//	}
//
//	func NewLog() *Log {
//		return &Log{}
//	}
//
//	func (c *Log) Append(record Record) (uint64, error) {
//		// We lock this section so that concurrent modifications to the shared state won't casue race conditions.
//		// Begin Critical section
//		c.mu.Lock()
//		defer c.mu.Unlock()                    // End Critical section. Lol Golang defer
//		record.Offset = uint64(len(c.records)) // First update offset
//		c.records = append(c.records, record)  // Add new record
//		return record.Offset, nil
//	}
//
//	func (c *Log) Read(offset uint64) (Record, error) {
//		// In theory this critical section could not be needed if we don't have that many writes but many reads. (Ie RWLock)
//		// Begin Critical section
//		c.mu.Lock()
//		defer c.mu.Unlock() // End Critical section. Lol Golang defer
//		// Error case. Offset is outside the length
//		if offset >= uint64(len(c.records)) || offset < 0 {
//			return Record{}, ErrOffsetNotFound // Err case
//		}
//		return c.records[offset], nil // Correct case
//	}
//
//	type Record struct {
//		Value  []byte `json:"value"`
//		Offset uint64 `json:"offset"`
//	}
var (
	ErrOffsetNotFound = errors.New("offset not found")
)
