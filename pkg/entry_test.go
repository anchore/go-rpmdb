package rpmdb

import (
	"encoding/binary"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawHeader builds an on-disk header blob: il, dl, the index entries (tag, type, offset, count), then the data store.
func rawHeader(il, dl uint32, entries [][4]uint32, data []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, il)
	b = binary.BigEndian.AppendUint32(b, dl)
	for _, e := range entries {
		for _, v := range e {
			b = binary.BigEndian.AppendUint32(b, v)
		}
	}
	return append(b, data...)
}

// regionTrailer is the 16 byte trailer a region entry points at, with the negated offset encoding ril.
func regionTrailer(tag, ril int32) []byte {
	return rawHeader(uint32(tag), uint32(RPM_BIN_TYPE), [][4]uint32{{uint32(-ril * REGION_TAG_COUNT), uint32(REGION_TAG_COUNT)}}, nil)
}

func Test_headerImport(t *testing.T) {
	regionEntry := func(offset uint32) [4]uint32 {
		return [4]uint32{uint32(RPMTAG_HEADERIMMUTABLE), uint32(RPM_BIN_TYPE), offset, uint32(REGION_TAG_COUNT)}
	}

	tests := []struct {
		name        string
		data        []byte
		errContains string // empty means the header must import cleanly
	}{
		{
			// found by fuzzer
			name:        "negative il",
			data:        []byte{0xe3, 0x30, 0x30, 0x30, 0x30, 0x30, 0x30, 0x30},
			errContains: "tags",
		},
		{
			name: "minimal valid region",
			data: rawHeader(1, 16, [][4]uint32{regionEntry(0)}, regionTrailer(RPMTAG_HEADERIMMUTABLE, 1)),
		},
		{
			// a zeroed region trailer leaves ril at 0, which used to reach blob.peList[1:0]
			name:        "zeroed region trailer",
			data:        rawHeader(1, 32, [][4]uint32{regionEntry(16)}, make([]byte, 32)),
			errContains: "invalid region trailer",
		},
		{
			// the trailer's tag/type/count were never checked, so garbage here was accepted
			name:        "region trailer with the wrong tag",
			data:        rawHeader(1, 16, [][4]uint32{regionEntry(0)}, regionTrailer(1000, 1)),
			errContains: "invalid region trailer",
		},
		{
			// a trailer that passes validation but encodes ril = 0 is caught at import instead
			name:        "region trailer with a zero region index length",
			data:        rawHeader(1, 32, [][4]uint32{regionEntry(16)}, append(make([]byte, 16), regionTrailer(RPMTAG_HEADERIMMUTABLE, 0)...)),
			errContains: "invalid region index length",
		},
		{
			// il*16 overflowed int32, leaving dataStart negative and slicing data with it
			name:        "il overflows the index size",
			data:        rawHeader(0x08000000, 32, [][4]uint32{regionEntry(0)}, make([]byte, 64)),
			errContains: "out of range",
		},
		{
			name:        "dl beyond the max header data size",
			data:        rawHeader(1, 0x7fffffff, [][4]uint32{regionEntry(0)}, regionTrailer(RPMTAG_HEADERIMMUTABLE, 1)),
			errContains: "out of range",
		},
		{
			// a string array claiming more strings than there are NUL terminators was accepted (and with
			// a large count, rescanned the same tail up to 4 billion times)
			name:        "string array count past the last terminator",
			data:        rawHeader(1, 4, [][4]uint32{{1000, uint32(RPM_STRING_ARRAY_TYPE), 0, 2}}, []byte("abc\x00")),
			errContains: "invalid data length",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() {
				_, err = headerImport(tt.data)
			})
			if tt.errContains == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.errContains)
		})
	}
}

func Test_headerImport_boundsAllocation(t *testing.T) {
	// 8 bytes claiming 2^31-1 index entries used to allocate ~32GB before any size check
	data := rawHeader(0x7fffffff, 0, nil, nil)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := headerImport(data)
	runtime.ReadMemStats(&after)

	require.Error(t, err)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(64<<20))
}

func Test_strtaglen(t *testing.T) {
	data := []byte("ab\x00cd\x00ef")
	end := int32(len(data))

	assert.Equal(t, 3, strtaglen(data, 1, 0, end))
	assert.Equal(t, 6, strtaglen(data, 2, 0, end))
	// a third string has no terminator before the end of the data
	assert.Equal(t, -1, strtaglen(data, 3, 0, end))
}
