package tray

import (
	"bytes"
	"encoding/binary"
)

func iconData() []byte {
	const (
		size      = 16
		rowPixels = size * size
		maskBytes = 64
	)

	imageSize := 40 + rowPixels*4 + maskBytes
	buf := bytes.NewBuffer(make([]byte, 0, 22+imageSize))

	_ = binary.Write(buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))

	buf.WriteByte(size)
	buf.WriteByte(size)
	buf.WriteByte(0)
	buf.WriteByte(0)
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(32))
	_ = binary.Write(buf, binary.LittleEndian, uint32(imageSize))
	_ = binary.Write(buf, binary.LittleEndian, uint32(22))

	_ = binary.Write(buf, binary.LittleEndian, uint32(40))
	_ = binary.Write(buf, binary.LittleEndian, int32(size))
	_ = binary.Write(buf, binary.LittleEndian, int32(size*2))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(32))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(rowPixels*4))
	_ = binary.Write(buf, binary.LittleEndian, int32(0))
	_ = binary.Write(buf, binary.LittleEndian, int32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(buf, binary.LittleEndian, uint32(0))

	for i := 0; i < rowPixels; i++ {
		buf.Write([]byte{0x37, 0x8B, 0xFF, 0xFF})
	}
	buf.Write(make([]byte, maskBytes))
	return buf.Bytes()
}
