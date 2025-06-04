package modelmanager

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a GGUF string for test data
func ggufTestString(s string) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint64(len(s)))
	buf.WriteString(s)
	return buf.Bytes()
}

// Helper to create a GGUF KV pair for test data
func ggufTestKV(key string, valueType GGUFMetadataValueType, valueData []byte) []byte {
	var buf bytes.Buffer
	buf.Write(ggufTestString(key))
	binary.Write(&buf, binary.LittleEndian, valueType)
	buf.Write(valueData)
	return buf.Bytes()
}

func TestParseGGUFMetaData(t *testing.T) {
	t.Run("ValidGGUFV3File", func(t *testing.T) {
		var data bytes.Buffer
		// Header
		binary.Write(&data, binary.LittleEndian, ggufMagicLittleEndian) // Magic
		binary.Write(&data, binary.LittleEndian, ggufVersionV3)         // Version
		binary.Write(&data, binary.LittleEndian, uint64(0))             // TensorCount
		binary.Write(&data, binary.LittleEndian, uint64(7))             // MetadataKVCount

		// KV Pairs
		// 1. String
		data.Write(ggufTestKV("arch", GGUFMetadataValueTypeString, ggufTestString("llama")))
		// 2. Uint32
		var uint32Bytes bytes.Buffer
		binary.Write(&uint32Bytes, binary.LittleEndian, uint32(32000))
		data.Write(ggufTestKV("vocab.size", GGUFMetadataValueTypeUint32, uint32Bytes.Bytes()))
		// 3. Bool true
		data.Write(ggufTestKV("general.bool_true", GGUFMetadataValueTypeBool, []byte{1}))
		// 4. Bool false
		data.Write(ggufTestKV("general.bool_false", GGUFMetadataValueTypeBool, []byte{0}))
		// 5. Float32
		var float32Bytes bytes.Buffer
		binary.Write(&float32Bytes, binary.LittleEndian, float32(3.14))
		data.Write(ggufTestKV("general.float_val", GGUFMetadataValueTypeFloat32, float32Bytes.Bytes()))
		// 6. Array of Uint8
		var arrayBytes bytes.Buffer
		binary.Write(&arrayBytes, binary.LittleEndian, GGUFMetadataValueTypeUint8) // Element type
		binary.Write(&arrayBytes, binary.LittleEndian, uint64(3))                  // Array length
		arrayBytes.Write([]byte{10, 20, 30})                                       // Array data
		data.Write(ggufTestKV("general.array_u8", GGUFMetadataValueTypeArray, arrayBytes.Bytes()))
		// 7. Int16
		var int16Bytes bytes.Buffer
		binary.Write(&int16Bytes, binary.LittleEndian, int16(-1000))
		data.Write(ggufTestKV("general.int16_val", GGUFMetadataValueTypeInt16, int16Bytes.Bytes()))

		parsedInfo, err := ParseGGUFMetaData(data.Bytes())
		require.NoError(t, err)
		require.NotNil(t, parsedInfo)

		assert.Equal(t, ggufMagicLittleEndian, parsedInfo.Header.Magic)
		assert.Equal(t, ggufVersionV3, parsedInfo.Header.Version)
		assert.Equal(t, uint64(0), parsedInfo.Header.TensorCount)
		assert.Equal(t, uint64(7), parsedInfo.Header.MetadataKVCount)

		require.Len(t, parsedInfo.Metadata, 7)
		assert.Equal(t, "llama", parsedInfo.Metadata["arch"])
		assert.Equal(t, uint32(32000), parsedInfo.Metadata["vocab.size"])
		assert.Equal(t, true, parsedInfo.Metadata["general.bool_true"])
		assert.Equal(t, false, parsedInfo.Metadata["general.bool_false"])
		assert.InDelta(t, float32(3.14), parsedInfo.Metadata["general.float_val"], 0.001)
		expectedArray := []interface{}{uint8(10), uint8(20), uint8(30)}
		assert.Equal(t, expectedArray, parsedInfo.Metadata["general.array_u8"])
		assert.Equal(t, int16(-1000), parsedInfo.Metadata["general.int16_val"])
	})

	t.Run("ValidGGUFV1File", func(t *testing.T) {
		var data bytes.Buffer
		// Header
		binary.Write(&data, binary.LittleEndian, ggufMagicLittleEndian) // Magic
		binary.Write(&data, binary.LittleEndian, ggufVersionV1)         // Version
		binary.Write(&data, binary.LittleEndian, uint64(1))             // TensorCount
		binary.Write(&data, binary.LittleEndian, uint64(1))             // MetadataKVCount
		// KV Pair
		data.Write(ggufTestKV("version", GGUFMetadataValueTypeUint32, []byte{1, 0, 0, 0})) // Value 1

		parsedInfo, err := ParseGGUFMetaData(data.Bytes())
		require.NoError(t, err)
		require.NotNil(t, parsedInfo)
		assert.Equal(t, ggufVersionV1, parsedInfo.Header.Version)
		assert.Equal(t, uint32(1), parsedInfo.Metadata["version"])
	})

	t.Run("NestedArray", func(t *testing.T) {
		var data bytes.Buffer
		// Header
		binary.Write(&data, binary.LittleEndian, ggufMagicLittleEndian)
		binary.Write(&data, binary.LittleEndian, ggufVersionV3)
		binary.Write(&data, binary.LittleEndian, uint64(0))
		binary.Write(&data, binary.LittleEndian, uint64(1)) // One KV pair

		// KV Pair: key = "nested_array"
		// Value: Array of (Array of Uint8)
		var nestedArrayValue bytes.Buffer
		binary.Write(&nestedArrayValue, binary.LittleEndian, GGUFMetadataValueTypeArray) // Outer array element type: Array
		binary.Write(&nestedArrayValue, binary.LittleEndian, uint64(2))                  // Outer array length: 2

		// Inner array 1: [1, 2] (type Uint8)
		binary.Write(&nestedArrayValue, binary.LittleEndian, GGUFMetadataValueTypeUint8) // Inner array 1 element type
		binary.Write(&nestedArrayValue, binary.LittleEndian, uint64(2))                  // Inner array 1 length
		nestedArrayValue.Write([]byte{1, 2})                                             // Inner array 1 data

		// Inner array 2: [3, 4, 5] (type Uint8)
		binary.Write(&nestedArrayValue, binary.LittleEndian, GGUFMetadataValueTypeUint8) // Inner array 2 element type
		binary.Write(&nestedArrayValue, binary.LittleEndian, uint64(3))                  // Inner array 2 length
		nestedArrayValue.Write([]byte{3, 4, 5})                                          // Inner array 2 data

		data.Write(ggufTestKV("nested_array", GGUFMetadataValueTypeArray, nestedArrayValue.Bytes()))

		parsedInfo, err := ParseGGUFMetaData(data.Bytes())
		require.NoError(t, err)
		require.NotNil(t, parsedInfo)
		require.Len(t, parsedInfo.Metadata, 1)

		expectedNested := []interface{}{
			[]interface{}{uint8(1), uint8(2)},
			[]interface{}{uint8(3), uint8(4), uint8(5)},
		}
		assert.Equal(t, expectedNested, parsedInfo.Metadata["nested_array"])
	})

	t.Run("ErrorCases", func(t *testing.T) {
		testCases := []struct {
			name        string
			data        []byte
			expectedErr error
			errContains string
		}{
			{
				name:        "InvalidMagic",
				data:        []byte{'G', 'G', 'U', 'X', 1, 0, 0, 0}, // GXUF
				expectedErr: ErrInvalidGGUFMagic,
			},
			{
				name: "UnsupportedVersion",
				data: func() []byte {
					var b bytes.Buffer
					binary.Write(&b, binary.LittleEndian, ggufMagicLittleEndian)
					binary.Write(&b, binary.LittleEndian, uint32(99)) // Unsupported version
					return b.Bytes()
				}(),
				expectedErr: ErrUnsupportedGGUFVersion,
			},
			{
				name:        "InsufficientDataForHeader",
				data:        []byte{'G', 'G', 'U', 'F'}, // Only magic
				expectedErr: ErrInsufficientData,
				errContains: "failed to read version",
			},
			{
				name: "InsufficientDataForKVKey",
				data: func() []byte {
					var b bytes.Buffer
					binary.Write(&b, binary.LittleEndian, ggufMagicLittleEndian)
					binary.Write(&b, binary.LittleEndian, ggufVersionV3)
					binary.Write(&b, binary.LittleEndian, uint64(0))
					binary.Write(&b, binary.LittleEndian, uint64(1)) // Expect 1 KV
					// Missing KV data
					return b.Bytes()
				}(),
				expectedErr: ErrInsufficientData,
				errContains: "failed to read metadata key 0",
			},
			{
				name: "InsufficientDataForKVValueType",
				data: func() []byte {
					var b bytes.Buffer
					binary.Write(&b, binary.LittleEndian, ggufMagicLittleEndian)
					binary.Write(&b, binary.LittleEndian, ggufVersionV3)
					binary.Write(&b, binary.LittleEndian, uint64(0))
					binary.Write(&b, binary.LittleEndian, uint64(1))
					b.Write(ggufTestString("some.key")) // Key is present
					// Missing value type and value
					return b.Bytes()
				}(),
				expectedErr: ErrInsufficientData,
				errContains: "failed to read value type for key 'some.key'",
			},
			{
				name: "InsufficientDataForKVValue",
				data: func() []byte {
					var b bytes.Buffer
					binary.Write(&b, binary.LittleEndian, ggufMagicLittleEndian)
					binary.Write(&b, binary.LittleEndian, ggufVersionV3)
					binary.Write(&b, binary.LittleEndian, uint64(0))
					binary.Write(&b, binary.LittleEndian, uint64(1))
					b.Write(ggufTestString("some.key"))
					binary.Write(&b, binary.LittleEndian, GGUFMetadataValueTypeUint32) // Expect Uint32
					// Missing Uint32 value (needs 4 bytes)
					return b.Bytes()
				}(),
				expectedErr: ErrInsufficientData, // Wrapped by binary.Read
			},
			{
				name: "InvalidBoolValue",
				data: func() []byte {
					var b bytes.Buffer
					binary.Write(&b, binary.LittleEndian, ggufMagicLittleEndian)
					binary.Write(&b, binary.LittleEndian, ggufVersionV3)
					binary.Write(&b, binary.LittleEndian, uint64(0))
					binary.Write(&b, binary.LittleEndian, uint64(1))
					b.Write(ggufTestKV("bad.bool", GGUFMetadataValueTypeBool, []byte{2})) // Invalid bool
					return b.Bytes()
				}(),
				expectedErr: ErrInvalidGGUFMetadataValue,
				errContains: "invalid boolean value 2",
			},
			{
				name: "UnknownValueType",
				data: func() []byte {
					var b bytes.Buffer
					binary.Write(&b, binary.LittleEndian, ggufMagicLittleEndian)
					binary.Write(&b, binary.LittleEndian, ggufVersionV3)
					binary.Write(&b, binary.LittleEndian, uint64(0))
					binary.Write(&b, binary.LittleEndian, uint64(1))
					b.Write(ggufTestString("unknown.type.key"))
					binary.Write(&b, binary.LittleEndian, GGUFMetadataValueType(99)) // Unknown type
					return b.Bytes()
				}(),
				expectedErr: ErrInvalidGGUFMetadataValue,
				errContains: "unknown type 99",
			},
			{
				name: "StringLengthExceedsData",
				data: func() []byte {
					var b bytes.Buffer
					binary.Write(&b, binary.LittleEndian, ggufMagicLittleEndian)
					binary.Write(&b, binary.LittleEndian, ggufVersionV3)
					binary.Write(&b, binary.LittleEndian, uint64(0))
					binary.Write(&b, binary.LittleEndian, uint64(1)) // 1 KV pair
					// Key with string length too long
					binary.Write(&b, binary.LittleEndian, uint64(100)) // String length 100
					// Only provide a few bytes for the string itself
					b.WriteString("short")
					// No value type or value data needed as it should fail on string read
					return b.Bytes()
				}(),
				expectedErr: ErrInsufficientData,
				errContains: "string length 100 exceeds available data",
			},
			{
				name: "ArrayLengthExceedsData",
				data: func() []byte {
					var b bytes.Buffer
					binary.Write(&b, binary.LittleEndian, ggufMagicLittleEndian)
					binary.Write(&b, binary.LittleEndian, ggufVersionV3)
					binary.Write(&b, binary.LittleEndian, uint64(0))
					binary.Write(&b, binary.LittleEndian, uint64(1)) // 1 KV pair
					b.Write(ggufTestString("bad.array"))
					binary.Write(&b, binary.LittleEndian, GGUFMetadataValueTypeArray)
					binary.Write(&b, binary.LittleEndian, GGUFMetadataValueTypeUint8) // Element type
					binary.Write(&b, binary.LittleEndian, uint64(1000))               // Array length 1000
					// Only provide a few bytes for array content
					b.Write([]byte{1, 2, 3})
					return b.Bytes()
				}(),
				expectedErr: ErrInsufficientData,
				errContains: "array length 1000 seems too large for available data",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := ParseGGUFMetaData(tc.data)
				require.Error(t, err)
				if tc.expectedErr != nil {
					assert.ErrorIs(t, err, tc.expectedErr)
				}
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains)
				}
			})
		}
	})

	t.Run("AllNumericTypes", func(t *testing.T) {
		var data bytes.Buffer
		// Header
		binary.Write(&data, binary.LittleEndian, ggufMagicLittleEndian)
		binary.Write(&data, binary.LittleEndian, ggufVersionV3)
		binary.Write(&data, binary.LittleEndian, uint64(0))
		binary.Write(&data, binary.LittleEndian, uint64(10)) // KVCount

		// KV Pairs
		var valBytes bytes.Buffer

		// Uint8
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, uint8(255))
		data.Write(ggufTestKV("type.u8", GGUFMetadataValueTypeUint8, valBytes.Bytes()))
		// Int8
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, int8(-128))
		data.Write(ggufTestKV("type.i8", GGUFMetadataValueTypeInt8, valBytes.Bytes()))
		// Uint16
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, uint16(65535))
		data.Write(ggufTestKV("type.u16", GGUFMetadataValueTypeUint16, valBytes.Bytes()))
		// Int16
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, int16(-32768))
		data.Write(ggufTestKV("type.i16", GGUFMetadataValueTypeInt16, valBytes.Bytes()))
		// Uint32
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, uint32(4294967295))
		data.Write(ggufTestKV("type.u32", GGUFMetadataValueTypeUint32, valBytes.Bytes()))
		// Int32
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, int32(-2147483648))
		data.Write(ggufTestKV("type.i32", GGUFMetadataValueTypeInt32, valBytes.Bytes()))
		// Float32
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, float32(123.456))
		data.Write(ggufTestKV("type.f32", GGUFMetadataValueTypeFloat32, valBytes.Bytes()))
		// Uint64
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, uint64(18446744073709551615))
		data.Write(ggufTestKV("type.u64", GGUFMetadataValueTypeUint64, valBytes.Bytes()))
		// Int64
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, int64(-9223372036854775808))
		data.Write(ggufTestKV("type.i64", GGUFMetadataValueTypeInt64, valBytes.Bytes()))
		// Float64
		valBytes.Reset()
		binary.Write(&valBytes, binary.LittleEndian, float64(789.0123456789))
		data.Write(ggufTestKV("type.f64", GGUFMetadataValueTypeFloat64, valBytes.Bytes()))

		parsedInfo, err := ParseGGUFMetaData(data.Bytes())
		require.NoError(t, err)
		require.NotNil(t, parsedInfo)
		require.Len(t, parsedInfo.Metadata, 10)

		assert.Equal(t, uint8(255), parsedInfo.Metadata["type.u8"])
		assert.Equal(t, int8(-128), parsedInfo.Metadata["type.i8"])
		assert.Equal(t, uint16(65535), parsedInfo.Metadata["type.u16"])
		assert.Equal(t, int16(-32768), parsedInfo.Metadata["type.i16"])
		assert.Equal(t, uint32(4294967295), parsedInfo.Metadata["type.u32"])
		assert.Equal(t, int32(-2147483648), parsedInfo.Metadata["type.i32"])
		assert.InDelta(t, float32(123.456), parsedInfo.Metadata["type.f32"], 0.0001)
		assert.Equal(t, uint64(18446744073709551615), parsedInfo.Metadata["type.u64"])
		assert.Equal(t, int64(-9223372036854775808), parsedInfo.Metadata["type.i64"])
		assert.InDelta(t, float64(789.0123456789), parsedInfo.Metadata["type.f64"], 0.0000000001)
	})
}
