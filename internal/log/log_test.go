package log

import (
	"github.com/stretchr/testify/require"
	api "github.com/wintercoder1/golog/api/v1"
	"google.golang.org/protobuf/proto"
	"io"
	"os"
	"testing"
)

func TestLog(t *testing.T) {
	for scenario, fn := range map[string]func(
		t *testing.T,
		log *Log,
	){
		"append and read a record succeeds": testAppendRead,
		"offset out of range error":         testOutOfRangeErr,
		"init with existing segments":       testInitExisting,
		"reader":                            testReader,
		"truncate":                          testTruncate,
	} {
		t.Run(scenario, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "store-test")
			require.NoError(t, err)
			defer os.RemoveAll(dir)

			c := Config{}
			c.Segment.MaxStoreBytes = 32

			log, err := NewLog(dir, c)
			require.NoError(t, err)

			fn(t, log)
		})
	}
}

func testAppendRead(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hello world"),
	}

	off, err := log.Append(append)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	read, err := log.Read(off)
	require.NoError(t, err)
	require.Equal(t, append.Value, read.Value)
}

func testOutOfRangeErr(t *testing.T, log *Log) {
	read, err := log.Read(1)
	require.Nil(t, read)
	apiErr := err.(api.ErrOffsetOutOfRange)
	require.Equal(t, uint64(1), apiErr.Offset)
}

func testInitExisting(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hello world"),
	}

	for i := 0; i < 3; i++ {
		off, err := log.Append(append)
		require.NoError(t, err)
		require.Equal(t, uint64(i), off)
	}

	c := Config{}
	c.Segment.MaxStoreBytes = 32

	log, err := NewLog(log.Dir, c)
	require.NoError(t, err)

	off, err := log.Append(append)
	require.NoError(t, err)
	require.Equal(t, uint64(3), off)
}

func testReader(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hello world"),
	}

	off, err := log.Append(append)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	reader := log.Reader()
	b, err := io.ReadAll(reader)
	require.NoError(t, err)

	read := api.Record{}
	err = proto.Unmarshal(b[lenWidth:], &read)
	require.NoError(t, err)
	require.Equal(t, append.Value, read.Value)
}

//func testTruncate(t *testing.T, log *Log) {
//	append := &api.Record{
//		Value: []byte("hello world"),
//	}
//
//	for i := 0; i < 3; i++ {
//		off, err := log.Append(append)
//		require.NoError(t, err)
//		require.Equal(t, uint64(i), off)
//	}
//
//	off, err := log.LowestOffset()
//	require.NoError(t, err)
//	require.Equal(t, uint64(0), off)
//
//	off, err = log.HighestOffset()
//	require.NoError(t, err)
//	require.Equal(t, uint64(2), off)
//
//	err = log.Truncate(1)
//	require.NoError(t, err)
//
//	_, err = log.Read(0)
//	require.Error(t, err)
//
//	off, err = log.LowestOffset()
//	require.NoError(t, err)
//	require.Equal(t, uint64(1), off)
//
//	off, err = log.HighestOffset()
//	require.NoError(t, err)
//	require.Equal(t, uint64(2), off)
//}

func testTruncate(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hello world"),
	}

	for i := 0; i < 3; i++ {
		off, err := log.Append(append)
		require.NoError(t, err)
		require.Equal(t, uint64(i), off)
	}

	// With MaxStoreBytes = 32, the records pack like this:
	//   offset 0 -> 21 bytes (8-byte len prefix + 13 marshaled); segment 0 = 21 < 32, not full
	//   offset 1 -> 23 bytes; segment 0 = 44 >= 32, full -> a new segment opens at base 2
	//   offset 2 -> 23 bytes; goes into segment 1
	// So segment 0 holds offsets {0, 1} and segment 1 holds {2}.
	off, err := log.LowestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	off, err = log.HighestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)

	// Truncate is per-segment: it removes any segment whose highest offset is
	// <= the argument. Segment 0's highest offset is 1, so the whole segment is
	// removed -- offset 1 goes with it. Segment 1 (base offset 2) survives.
	err = log.Truncate(1)
	require.NoError(t, err)

	// Offset 0 lived in the removed segment, so reading it now errors.
	_, err = log.Read(0)
	require.Error(t, err)

	// The surviving segment starts at base offset 2, so the lowest remaining
	// offset is 2 -- NOT 1. Offset 1 was removed along with segment 0.
	off, err = log.LowestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)

	off, err = log.HighestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)

	// The kept record should still be readable and intact.
	read, err := log.Read(2)
	require.NoError(t, err)
	require.Equal(t, append.Value, read.Value)
}
