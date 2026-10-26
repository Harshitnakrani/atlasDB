package record

import (
	"encoding/binary"
	"errors"
	"math"

	"atlasDB/internal/common"
)

var (
	ErrCorruptRecord = errors.New("atlas record: corrupted record payload")
	ErrColOutOfRange = errors.New("atlas record: column index out of range")
)

const (
	tagNull  = 0
	tagFalse = 1
	tagTrue  = 2
	tagInt   = 3
	tagFloat = 4
	tagText  = 5
	tagBlob  = 6
)

// Record represents a logical database row consisting of typed values.
type Record struct {
	Values []common.Value
}

func NewRecord(values []common.Value) *Record {
	return &Record{Values: values}
}

// Encode serializes the record into an explicit, architecture-independent binary representation.
func (r *Record) Encode() ([]byte, error) {
	return EncodeValues(r.Values)
}

// EncodeValues serializes a slice of common.Value into bytes.
func EncodeValues(values []common.Value) ([]byte, error) {
	numCols := len(values)
	var headerBuf []byte
	var bodyBuf []byte

	// 1. Column count
	var uvarBuf [10]byte
	n := binary.PutUvarint(uvarBuf[:], uint64(numCols))
	headerBuf = append(headerBuf, uvarBuf[:n]...)

	// 2. Type tags & payloads
	for _, val := range values {
		switch val.Type {
		case common.TypeNull:
			headerBuf = append(headerBuf, tagNull)

		case common.TypeBool:
			if val.Bool {
				headerBuf = append(headerBuf, tagTrue)
			} else {
				headerBuf = append(headerBuf, tagFalse)
			}

		case common.TypeInt:
			headerBuf = append(headerBuf, tagInt)
			var viBuf [10]byte
			vn := binary.PutVarint(viBuf[:], val.Int)
			bodyBuf = append(bodyBuf, viBuf[:vn]...)

		case common.TypeFloat:
			headerBuf = append(headerBuf, tagFloat)
			var flBuf [8]byte
			binary.BigEndian.PutUint64(flBuf[:], math.Float64bits(val.Float))
			bodyBuf = append(bodyBuf, flBuf[:]...)

		case common.TypeText:
			headerBuf = append(headerBuf, tagText)
			textBytes := []byte(val.Text)
			n := binary.PutUvarint(uvarBuf[:], uint64(len(textBytes)))
			bodyBuf = append(bodyBuf, uvarBuf[:n]...)
			bodyBuf = append(bodyBuf, textBytes...)

		case common.TypeBlob:
			headerBuf = append(headerBuf, tagBlob)
			n := binary.PutUvarint(uvarBuf[:], uint64(len(val.Blob)))
			bodyBuf = append(bodyBuf, uvarBuf[:n]...)
			bodyBuf = append(bodyBuf, val.Blob...)

		default:
			return nil, common.ErrTypeMismatch
		}
	}

	result := make([]byte, 0, len(headerBuf)+len(bodyBuf))
	result = append(result, headerBuf...)
	result = append(result, bodyBuf...)
	return result, nil
}

// Decode deserializes bytes back into a slice of common.Value.
func Decode(data []byte) ([]common.Value, error) {
	if len(data) == 0 {
		return []common.Value{}, nil
	}

	numColsU64, headerOffset := binary.Uvarint(data)
	if headerOffset <= 0 {
		return nil, ErrCorruptRecord
	}
	numCols := int(numColsU64)

	if len(data) < headerOffset+numCols {
		return nil, ErrCorruptRecord
	}

	types := data[headerOffset : headerOffset+numCols]
	bodyOffset := headerOffset + numCols

	values := make([]common.Value, numCols)
	currBody := bodyOffset

	for i := 0; i < numCols; i++ {
		tag := types[i]
		switch tag {
		case tagNull:
			values[i] = common.NullValue()

		case tagFalse:
			values[i] = common.BoolValue(false)

		case tagTrue:
			values[i] = common.BoolValue(true)

		case tagInt:
			val, n := binary.Varint(data[currBody:])
			if n <= 0 {
				return nil, ErrCorruptRecord
			}
			currBody += n
			values[i] = common.IntValue(val)

		case tagFloat:
			if currBody+8 > len(data) {
				return nil, ErrCorruptRecord
			}
			bits := binary.BigEndian.Uint64(data[currBody : currBody+8])
			currBody += 8
			values[i] = common.FloatValue(math.Float64frombits(bits))

		case tagText:
			lengthU64, n := binary.Uvarint(data[currBody:])
			if n <= 0 {
				return nil, ErrCorruptRecord
			}
			currBody += n
			length := int(lengthU64)
			if currBody+length > len(data) {
				return nil, ErrCorruptRecord
			}
			textStr := string(data[currBody : currBody+length])
			currBody += length
			values[i] = common.TextValue(textStr)

		case tagBlob:
			lengthU64, n := binary.Uvarint(data[currBody:])
			if n <= 0 {
				return nil, ErrCorruptRecord
			}
			currBody += n
			length := int(lengthU64)
			if currBody+length > len(data) {
				return nil, ErrCorruptRecord
			}
			blobBytes := make([]byte, length)
			copy(blobBytes, data[currBody:currBody+length])
			currBody += length
			values[i] = common.BlobValue(blobBytes)

		default:
			return nil, ErrCorruptRecord
		}
	}

	return values, nil
}
