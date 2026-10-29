package amqp

import (
	"encoding/binary"
	"errors"
	"io"
)

const frameLimit = 131072 // Includes the seven-byte header and frame-end octet.
const bodyLimit = 8 << 20

var errWire = errors.New("invalid AMQP frame")

type frame struct {
	kind    byte
	channel uint16
	payload []byte
}

func readFrame(r io.Reader, limit uint32) (frame, error) {
	var h [7]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return frame{}, err
	}
	n := binary.BigEndian.Uint32(h[3:])
	if limit < 8 || n > limit-8 {
		return frame{}, errWire
	}
	p := make([]byte, int(n)+1)
	if _, err := io.ReadFull(r, p); err != nil {
		return frame{}, err
	}
	if p[n] != 0xce {
		return frame{}, errWire
	}
	f := frame{h[0], binary.BigEndian.Uint16(h[1:]), p[:n]}
	if f.kind != 1 && f.kind != 2 && f.kind != 3 && f.kind != 8 {
		return frame{}, errWire
	}
	if f.kind == 8 && (f.channel != 0 || len(f.payload) != 0) {
		return frame{}, errWire
	}
	return f, nil
}
func writeFrame(w io.Writer, f frame) error {
	var h [7]byte
	h[0] = f.kind
	binary.BigEndian.PutUint16(h[1:], f.channel)
	binary.BigEndian.PutUint32(h[3:], uint32(len(f.payload)))
	for _, b := range [][]byte{h[:], f.payload, {0xce}} {
		for len(b) > 0 {
			n, err := w.Write(b)
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			b = b[n:]
		}
	}
	return nil
}

type encoder []byte

func (e *encoder) u8(v byte)      { *e = append(*e, v) }
func (e *encoder) u16(v uint16)   { *e = binary.BigEndian.AppendUint16(*e, v) }
func (e *encoder) u32(v uint32)   { *e = binary.BigEndian.AppendUint32(*e, v) }
func (e *encoder) u64(v uint64)   { *e = binary.BigEndian.AppendUint64(*e, v) }
func (e *encoder) short(v string) { e.u8(byte(len(v))); *e = append(*e, v...) }
func (e *encoder) long(v []byte)  { e.u32(uint32(len(v))); *e = append(*e, v...) }

type decoder struct {
	p   []byte
	bad bool
}

func (d *decoder) take(n int) []byte {
	if n < 0 || n > len(d.p) {
		d.bad = true
		return nil
	}
	p := d.p[:n]
	d.p = d.p[n:]
	return p
}
func (d *decoder) u8() byte {
	p := d.take(1)
	if p == nil {
		return 0
	}
	return p[0]
}
func (d *decoder) u16() uint16 {
	p := d.take(2)
	if p == nil {
		return 0
	}
	return binary.BigEndian.Uint16(p)
}
func (d *decoder) u32() uint32 {
	p := d.take(4)
	if p == nil {
		return 0
	}
	return binary.BigEndian.Uint32(p)
}
func (d *decoder) u64() uint64 {
	p := d.take(8)
	if p == nil {
		return 0
	}
	return binary.BigEndian.Uint64(p)
}
func (d *decoder) short() string { return string(d.take(int(d.u8()))) }
func (d *decoder) long() []byte {
	n := d.u32()
	if uint64(n) > uint64(len(d.p)) {
		d.bad = true
		return nil
	}
	return d.take(int(n))
}
func (d *decoder) done() bool { return !d.bad && len(d.p) == 0 }

// Basic properties use one flag word; preserve their exact bytes after bounds validation.
func validProperties(p []byte) bool {
	d := decoder{p: p}
	flags := d.u16()
	if flags&3 != 0 {
		return false
	}
	for bit := uint16(0x8000); bit >= 4; bit >>= 1 {
		if flags&bit == 0 {
			continue
		}
		switch bit {
		case 0x2000:
			d.long()
		case 0x1000, 0x0800:
			d.u8()
		case 0x0040:
			d.u64()
		default:
			d.short()
		}
	}
	return d.done()
}
