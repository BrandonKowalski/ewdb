package capture

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
)

type Image struct {
	W, H int
	Pix  []byte
}

func FourCC(a, b, c, d byte) uint32 {
	return uint32(a) | uint32(b)<<8 | uint32(c)<<16 | uint32(d)<<24
}

func FormatName(f uint32) string {
	return string([]byte{byte(f), byte(f >> 8), byte(f >> 16), byte(f >> 24)})
}

type layout struct {
	r, g, b, a, bpp int
}

func layoutOf(format uint32) (layout, bool) {
	switch format {
	case FourCC('X', 'R', '2', '4'):
		return layout{2, 1, 0, -1, 4}, true
	case FourCC('A', 'R', '2', '4'):
		return layout{2, 1, 0, 3, 4}, true
	case FourCC('X', 'B', '2', '4'):
		return layout{0, 1, 2, -1, 4}, true
	case FourCC('A', 'B', '2', '4'):
		return layout{0, 1, 2, 3, 4}, true
	case FourCC('R', 'G', '2', '4'):
		return layout{2, 1, 0, -1, 3}, true
	case FourCC('B', 'G', '2', '4'):
		return layout{0, 1, 2, -1, 3}, true
	}
	return layout{}, false
}

func ToRGBA(format uint32, src []byte, pitch, w, h int) ([]byte, error) {
	rg16 := format == FourCC('R', 'G', '1', '6')
	bg16 := format == FourCC('B', 'G', '1', '6')
	l, ok := layoutOf(format)
	bpp := l.bpp
	if rg16 || bg16 {
		bpp = 2
	} else if !ok {
		return nil, fmt.Errorf("unsupported format %s", FormatName(format))
	}
	if w < 0 || h < 0 || pitch < 0 {
		return nil, fmt.Errorf("bad geometry %dx%d pitch %d", w, h, pitch)
	}
	if w > 0 && h > 0 && len(src) < (h-1)*pitch+w*bpp {
		return nil, fmt.Errorf("buffer of %d bytes is smaller than %dx%d with pitch %d", len(src), w, h, pitch)
	}
	dst := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		row := src[y*pitch:]
		out := dst[y*w*4:]
		for x := 0; x < w; x++ {
			d := out[x*4 : x*4+4]
			if rg16 || bg16 {
				v := uint16(row[x*2]) | uint16(row[x*2+1])<<8
				hi := byte(v>>11) & 0x1f
				mid := byte(v>>5) & 0x3f
				lo := byte(v) & 0x1f
				first := hi<<3 | hi>>2
				last := lo<<3 | lo>>2
				if rg16 {
					d[0], d[2] = first, last
				} else {
					d[0], d[2] = last, first
				}
				d[1] = mid<<2 | mid>>4
				d[3] = 0xff
				continue
			}
			s := row[x*bpp : x*bpp+bpp]
			d[0] = s[l.r]
			d[1] = s[l.g]
			d[2] = s[l.b]
			if l.a < 0 {
				d[3] = 0xff
			} else {
				d[3] = s[l.a]
			}
		}
	}
	return dst, nil
}

func WriteRaw(w io.Writer, img Image) error {
	var head [12]byte
	binary.LittleEndian.PutUint32(head[0:], uint32(img.W))
	binary.LittleEndian.PutUint32(head[4:], uint32(img.H))
	binary.LittleEndian.PutUint32(head[8:], 1)
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err := w.Write(img.Pix)
	return err
}

func WritePNG(w io.Writer, img Image) error {
	pix := img.Pix
	for i := 3; i < len(pix); i += 4 {
		if pix[i] != 0xff {
			pix = append([]byte(nil), pix...)
			for j := 3; j < len(pix); j += 4 {
				pix[j] = 0xff
			}
			break
		}
	}
	rgba := &image.RGBA{Pix: pix, Stride: img.W * 4, Rect: image.Rect(0, 0, img.W, img.H)}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(w, rgba)
}

func Stack(imgs []Image) Image {
	w, h := 0, 0
	for _, im := range imgs {
		if im.W > w {
			w = im.W
		}
		h += im.H
	}
	pix := make([]byte, w*h*4)
	for i := 3; i < len(pix); i += 4 {
		pix[i] = 0xff
	}
	top := 0
	for _, im := range imgs {
		for y := 0; y < im.H; y++ {
			copy(pix[((top+y)*w)*4:], im.Pix[y*im.W*4:(y+1)*im.W*4])
		}
		top += im.H
	}
	return Image{W: w, H: h, Pix: pix}
}
