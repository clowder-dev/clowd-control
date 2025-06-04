package modelmanager

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	// ggufMagicLittleEndian is "GGUF" as a little-endian uint32.
	ggufMagicLittleEndian uint32 = 0x46554747
	// GGUF versions supported by this parser. Version 3 is the latest spec provided.
	// Versions 1 and 2 are also common.
	ggufVersionV1 uint32 = 1
	ggufVersionV2 uint32 = 2
	ggufVersionV3 uint32 = 3
)

// GGUFMetadataValueType mirrors the C enum gguf_metadata_value_type.
type GGUFMetadataValueType uint32

// Constants for GGUF metadata value types.
const (
	GGUFMetadataValueTypeUint8   GGUFMetadataValueType = 0
	GGUFMetadataValueTypeInt8    GGUFMetadataValueType = 1
	GGUFMetadataValueTypeUint16  GGUFMetadataValueType = 2
	GGUFMetadataValueTypeInt16   GGUFMetadataValueType = 3
	GGUFMetadataValueTypeUint32  GGUFMetadataValueType = 4
	GGUFMetadataValueTypeInt32   GGUFMetadataValueType = 5
	GGUFMetadataValueTypeFloat32 GGUFMetadataValueType = 6
	GGUFMetadataValueTypeBool    GGUFMetadataValueType = 7
	GGUFMetadataValueTypeString  GGUFMetadataValueType = 8
	GGUFMetadataValueTypeArray   GGUFMetadataValueType = 9
	GGUFMetadataValueTypeUint64  GGUFMetadataValueType = 10
	GGUFMetadataValueTypeInt64   GGUFMetadataValueType = 11
	GGUFMetadataValueTypeFloat64 GGUFMetadataValueType = 12
)

// GGUFHeader represents the parsed GGUF file header.
type GGUFHeader struct {
	Magic           uint32 // Should be ggufMagicLittleEndian
	Version         uint32
	TensorCount     uint64
	MetadataKVCount uint64
}

// ParsedGGUFInfo holds the extracted header and metadata key-value pairs.
type ParsedGGUFInfo struct {
	Header   GGUFHeader
	Metadata map[string]interface{}
}

var (
	ErrInvalidGGUFMagic         = errors.New("invalid GGUF magic number")
	ErrUnsupportedGGUFVersion   = errors.New("unsupported GGUF version")
	ErrInsufficientData         = errors.New("insufficient data for GGUF parsing")
	ErrInvalidGGUFMetadataValue = errors.New("invalid GGUF metadata value")
)

// readData wraps binary.Read for simple data types, enhancing error reporting for insufficient data.
func readData(reader io.Reader, data interface{}, typeName string) error {
	err := binary.Read(reader, binary.LittleEndian, data)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("%w: reading %s: %v", ErrInsufficientData, typeName, err)
		}
		return fmt.Errorf("error reading %s: %v", typeName, err) // General error for other binary.Read issues
	}
	return nil
}

// ParseGGUFMetaData extracts metadata from a GGUF file prefix.
// The input `data` is expected to be the initial bytes of a GGUF file.
func ParseGGUFMetaData(data []byte) (*ParsedGGUFInfo, error) {
	reader := bytes.NewReader(data)

	header := GGUFHeader{}
	var err error

	// Read Magic
	if err = binary.Read(reader, binary.LittleEndian, &header.Magic); err != nil {
		return nil, fmt.Errorf("%w: failed to read magic number: %v", ErrInsufficientData, err)
	}
	if header.Magic != ggufMagicLittleEndian {
		return nil, fmt.Errorf("%w: expected %X, got %X", ErrInvalidGGUFMagic, ggufMagicLittleEndian, header.Magic)
	}

	// Read Version
	if err = binary.Read(reader, binary.LittleEndian, &header.Version); err != nil {
		return nil, fmt.Errorf("%w: failed to read version: %v", ErrInsufficientData, err)
	}
	// Supporting V1, V2, V3. V3 is the reference from spec.
	if header.Version != ggufVersionV1 && header.Version != ggufVersionV2 && header.Version != ggufVersionV3 {
		return nil, fmt.Errorf("%w: parser supports v1, v2, v3, got v%d", ErrUnsupportedGGUFVersion, header.Version)
	}

	// Read TensorCount
	if err = binary.Read(reader, binary.LittleEndian, &header.TensorCount); err != nil {
		return nil, fmt.Errorf("%w: failed to read tensor count: %v", ErrInsufficientData, err)
	}

	// Read MetadataKVCount
	if err = binary.Read(reader, binary.LittleEndian, &header.MetadataKVCount); err != nil {
		return nil, fmt.Errorf("%w: failed to read metadata KV count: %v", ErrInsufficientData, err)
	}

	metadata := make(map[string]interface{})
	for i := uint64(0); i < header.MetadataKVCount; i++ {
		key, err := readGGUFString(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read metadata key %d: %w", i, err)
		}

		var valueType GGUFMetadataValueType
		if err = binary.Read(reader, binary.LittleEndian, &valueType); err != nil {
			return nil, fmt.Errorf("%w: failed to read value type for key '%s': %v", ErrInsufficientData, key, err)
		}

		value, err := readGGUFValue(reader, valueType)
		if err != nil {
			return nil, fmt.Errorf("failed to read metadata value for key '%s' (type %d): %w", key, valueType, err)
		}
		metadata[key] = value
	}

	return &ParsedGGUFInfo{
		Header:   header,
		Metadata: metadata,
	}, nil
}

func readGGUFString(reader io.Reader) (string, error) {
	var length uint64
	if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
		return "", fmt.Errorf("%w: failed to read string length: %v", ErrInsufficientData, err)
	}

	// Protect against extremely large string lengths if the prefix is too short
	// This check is more relevant if the reader wasn't bounded by an initial slice,
	// but good for robustness. Max sensible length can be tuned.
	if r, ok := reader.(*bytes.Reader); ok {
		if length > uint64(r.Len()) {
			return "", fmt.Errorf("%w: string length %d exceeds available data %d", ErrInsufficientData, length, r.Len())
		}
	}


	strBytes := make([]byte, length)
	if _, err := io.ReadFull(reader, strBytes); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return "", fmt.Errorf("%w: reading string content (length %d): %v", ErrInsufficientData, length, err)
		}
		return "", fmt.Errorf("error reading string content (length %d): %v", length, err)
	}
	return string(strBytes), nil
}

func readGGUFValue(reader io.Reader, valueType GGUFMetadataValueType) (interface{}, error) {
	switch valueType {
	case GGUFMetadataValueTypeUint8:
		var val uint8
		if err := readData(reader, &val, "uint8"); err != nil { return nil, err }
		return val, nil
	case GGUFMetadataValueTypeInt8:
		var val int8
		if err := readData(reader, &val, "int8"); err != nil { return nil, err }
		return val, nil
	case GGUFMetadataValueTypeUint16:
		var val uint16
		if err := readData(reader, &val, "uint16"); err != nil { return nil, err }
		return val, nil
	case GGUFMetadataValueTypeInt16:
		var val int16
		if err := readData(reader, &val, "int16"); err != nil { return nil, err }
		return val, nil
	case GGUFMetadataValueTypeUint32:
		var val uint32
		if err := readData(reader, &val, "uint32"); err != nil { return nil, err }
		return val, nil
	case GGUFMetadataValueTypeInt32:
		var val int32
		if err := readData(reader, &val, "int32"); err != nil { return nil, err }
		return val, nil
	case GGUFMetadataValueTypeFloat32:
		var bits uint32
		if err := readData(reader, &bits, "float32 bits"); err != nil { return nil, err }
		return math.Float32frombits(bits), nil
	case GGUFMetadataValueTypeBool:
		var val uint8
		if err := readData(reader, &val, "bool"); err != nil { return nil, err }
		if val == 0 {
			return false, nil
		}
		if val == 1 {
			return true, nil
		}
		return nil, fmt.Errorf("%w: invalid boolean value %d", ErrInvalidGGUFMetadataValue, val)
	case GGUFMetadataValueTypeString:
		return readGGUFString(reader) // readGGUFString handles its own specific ErrInsufficientData wrapping
	case GGUFMetadataValueTypeArray:
		var elementType GGUFMetadataValueType
		if err := binary.Read(reader, binary.LittleEndian, &elementType); err != nil {
			return nil, fmt.Errorf("%w: failed to read array element type: %v", ErrInsufficientData, err)
		}
		var length uint64
		if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
			return nil, fmt.Errorf("%w: failed to read array length: %v", ErrInsufficientData, err)
		}
		
		// Protect against extremely large array lengths if the prefix is too short
		// This is a basic sanity check. A more sophisticated check might consider element size.
		if r, ok := reader.(*bytes.Reader); ok {
			// Estimate minimum size for an element (e.g., 1 byte)
			if length > 0 && length > uint64(r.Len()) {
                 return nil, fmt.Errorf("%w: array length %d seems too large for available data %d", ErrInsufficientData, length, r.Len())
			}
		}


		arr := make([]interface{}, length)
		for i := uint64(0); i < length; i++ {
			elem, err := readGGUFValue(reader, elementType)
			if err != nil {
				return nil, fmt.Errorf("failed to read array element %d: %w", i, err)
			}
			arr[i] = elem
		}
		return arr, nil
	case GGUFMetadataValueTypeUint64:
		var val uint64
		if err := readData(reader, &val, "uint64"); err != nil { return nil, err }
		return val, nil
	case GGUFMetadataValueTypeInt64:
		var val int64
		if err := readData(reader, &val, "int64"); err != nil { return nil, err }
		return val, nil
	case GGUFMetadataValueTypeFloat64:
		var bits uint64
		if err := readData(reader, &bits, "float64 bits"); err != nil { return nil, err }
		return math.Float64frombits(bits), nil
	default:
		return nil, fmt.Errorf("%w: unknown type %d", ErrInvalidGGUFMetadataValue, valueType)
	}
}
